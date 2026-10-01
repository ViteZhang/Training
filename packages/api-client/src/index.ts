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

  constructor(status: number, body: Partial<Schemas['Error']> | undefined) {
    super(body?.message ?? '网络开小差了，请稍后再试');
    this.name = 'ApiError';
    this.status = status;
    this.code = body?.code ?? (status === 0 ? 'NETWORK' : 'UNKNOWN');
    this.detail = body?.detail;
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
}

export interface ApiClientOptions {
  baseUrl: string;
  /** 当前访问令牌；没有登录返回 undefined */
  getAccessToken: () => string | undefined;
  /** 访问令牌过期时刷新；返回新的访问令牌，刷新失败返回 undefined（调用方随后退出登录） */
  refresh?: () => Promise<string | undefined>;
  /** 生成幂等键，默认用 crypto.randomUUID */
  newIdempotencyKey?: () => string;
  fetch?: typeof fetch;
}

const WRITE_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);

/** 创建带鉴权、幂等键、错误转换的客户端。 */
export function createApiClient(opts: ApiClientOptions) {
  const client = createClient<paths>({ baseUrl: opts.baseUrl, fetch: opts.fetch });
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
    async onResponse({ request, response }) {
      if (response.status !== 401 || !opts.refresh || request.headers.get('X-Retried') === '1') {
        return response;
      }
      // 并发的多个 401 只刷新一次。
      refreshing ??= opts.refresh().finally(() => {
        refreshing = undefined;
      });
      const token = await refreshing;
      if (!token) return response;
      const retry = new Request(request, { headers: new Headers(request.headers) });
      retry.headers.set('Authorization', `Bearer ${token}`);
      retry.headers.set('X-Retried', '1');
      return (opts.fetch ?? fetch)(retry);
    },
  };
  client.use(middleware);
  return client;
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
  } catch {
    throw new ApiError(0, undefined);
  }
  if (res.error !== undefined || !res.response.ok) {
    throw new ApiError(res.response.status, res.error as Partial<Schemas['Error']>);
  }
  return res.data as T;
}
