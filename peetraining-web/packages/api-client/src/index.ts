/**
 * 接口客户端。类型由 openapi.yaml 生成在 src/generated（禁止手改，执行 pnpm gen:api 重新生成）；
 * 本文件只做统一的请求处理：令牌、令牌刷新、幂等键、错误转换。
 */
import createClient, { type Middleware } from 'openapi-fetch';
import type { components, paths } from './generated/schema';

export type { components, paths };
export type Schemas = components['schemas'];

/** 后端统一错误 { code, message, detail }。 */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail?: Record<string, unknown>;
  readonly originalCause?: unknown;

  constructor(status: number, body: Partial<Schemas['Error']> | undefined, originalCause?: unknown) {
    super(body?.message ?? '网络开小差了，请稍后再试');
    this.name = 'ApiError';
    this.status = status;
    this.code = body?.code ?? (status === 0 ? 'NETWORK' : 'UNKNOWN');
    this.detail = body?.detail;
    this.originalCause = originalCause;
  }

  get isQuotaExceeded() {
    return this.code === 'QUOTA_EXCEEDED';
  }
  get isUnauthorized() {
    return this.code === 'UNAUTHORIZED';
  }
  get isNotFound() {
    return this.code === 'NOT_FOUND';
  }
  get isNetwork() {
    return this.code === 'NETWORK';
  }
  get isTimeout() {
    return this.originalCause instanceof Error && this.originalCause.name === 'TimeoutError';
  }
}

export interface ApiClientOptions {
  baseUrl: string;
  /** 当前访问令牌；没有登录返回 undefined */
  getAccessToken: () => string | undefined;
  /**
   * 访问令牌过期时刷新；返回新的访问令牌，刷新失败返回 undefined。
   * 刷新失败后 getAccessToken 仍有令牌，说明登录态还在（多半是网络没恢复），按网络错误返回；
   * 登录态被清除才把原来的 401 交给调用方。
   */
  refresh?: () => Promise<string | undefined>;
  /** 生成幂等键，默认用 crypto.randomUUID */
  newIdempotencyKey?: () => string;
  fetch?: typeof fetch;
  /** Optional request timeout. Mutations are not replayed automatically after a timeout. */
  requestTimeoutMs?: number;
}

const WRITE_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

/** 创建带鉴权、幂等键、错误转换的客户端。 */
export function createApiClient(opts: ApiClientOptions) {
  const requestFetch: typeof fetch = (input, init) => {
    const fetcher = opts.fetch ?? globalThis.fetch;
    return opts.requestTimeoutMs && opts.requestTimeoutMs > 0
      ? withTimeout(fetcher, opts.requestTimeoutMs)(input, init)
      : fetcher(input, init);
  };
  const client = createClient<paths>({ baseUrl: opts.baseUrl, fetch: requestFetch });
  const newKey = opts.newIdempotencyKey ?? (() => globalThis.crypto.randomUUID());
  let refreshing: Promise<string | undefined> | undefined;

  const middleware: Middleware = {
    onRequest({ request }) {
      const token = opts.getAccessToken();
      if (token) request.headers.set('Authorization', `Bearer ${token}`);
      // 调用方可以自己传 Idempotency-Key（例如重试同一次提交时复用）；没有就生成一个。
      if (WRITE_METHODS.has(request.method) && !request.headers.has('Idempotency-Key')) {
        request.headers.set('Idempotency-Key', newKey());
      }
      return request;
    },
    // 不改响应时返回 undefined：openapi-fetch 要求返回值是全局 Response 的实例，而 Expo 的
    // expo/fetch（替换了全局 fetch）返回的响应不是，原样返回会被当成异常（App 里显示「网络开小差了」）。
    async onResponse({ request, response }) {
      if (response.status !== 401 || !opts.refresh || request.headers.get('X-Retried') === '1') {
        return undefined;
      }
      // 并发的多个 401 只刷新一次。
      refreshing ??= opts.refresh().finally(() => {
        refreshing = undefined;
      });
      const token = await refreshing;
      if (!token) {
        // 登录态还在（刷新因网络失败）：不要让页面显示「登录已失效」，按网络错误返回，查询会自动重试。
        if (opts.getAccessToken()) {
          return new Response(JSON.stringify({ code: 'NETWORK', message: '网络还没连上，请稍后再试' }), {
            status: 503,
            headers: { 'Content-Type': 'application/json' },
          });
        }
        return undefined;
      }
      const retry = new Request(request, { headers: new Headers(request.headers) });
      retry.headers.set('Authorization', `Bearer ${token}`);
      retry.headers.set('X-Retried', '1');
      return toResponse(await requestFetch(retry));
    },
  };
  client.use(middleware);
  return client;
}

class ApiRequestTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Request timed out after ${timeoutMs}ms`);
    this.name = 'TimeoutError';
  }
}

function withTimeout(fetcher: typeof fetch, timeoutMs: number): typeof fetch {
  return async (input, init) => {
    const controller = new AbortController();
    const inheritedSignal = init?.signal ?? (typeof Request !== 'undefined' && input instanceof Request ? input.signal : undefined);
    const abort = () => controller.abort();
    if (inheritedSignal?.aborted) abort();
    else inheritedSignal?.addEventListener('abort', abort, { once: true });

    let timer: ReturnType<typeof setTimeout> | undefined;
    const timedOut = new Promise<never>((_, reject) => {
      timer = setTimeout(() => {
        reject(new ApiRequestTimeoutError(timeoutMs));
        controller.abort();
      }, timeoutMs);
    });

    try {
      return await Promise.race([fetcher(input, { ...init, signal: controller.signal }), timedOut]);
    } finally {
      if (timer) clearTimeout(timer);
      inheritedSignal?.removeEventListener('abort', abort);
    }
  };
}

/** 把 expo/fetch 等返回的非标准响应转成全局 Response（中间件替换响应时 openapi-fetch 要求）。 */
async function toResponse(res: Response): Promise<Response> {
  if (res instanceof Response) return res;
  res = res as Response;
  const headers: Record<string, string> = {};
  res.headers.forEach((value, key) => {
    headers[key] = value;
  });
  const body = res.status === 204 || res.status === 304 ? null : await res.text();
  return new Response(body, { status: res.status, statusText: res.statusText, headers });
}

export type ApiClient = ReturnType<typeof createApiClient>;

/**
 * 把 openapi-fetch 的结果转成「成功返回数据、失败抛 ApiError」，方便配合 TanStack Query。
 * 网络错误（fetch 抛异常）统一为 code=NETWORK。
 */
export async function unwrap<T>(
  call: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  let res;
  try {
    res = await call;
  } catch (e) {
    throw new ApiError(0, undefined, e);
  }
  if (res.error !== undefined || !res.response.ok) {
    throw new ApiError(res.response.status, res.error as Partial<Schemas['Error']>);
  }
  return res.data as T;
}
