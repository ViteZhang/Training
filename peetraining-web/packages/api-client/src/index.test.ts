import { describe, expect, it, vi } from 'vitest';
import { ApiError, createApiClient, unwrap } from './index';

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

describe('createApiClient', () => {
  it('带上令牌，写请求自动加幂等键', async () => {
    const seen: Request[] = [];
    const client = createApiClient({
      baseUrl: 'https://x.test/api/v1',
      getAccessToken: () => 'tok',
      newIdempotencyKey: () => 'idem-1',
      fetch: async (input) => {
        seen.push(input as Request);
        return jsonResponse(201, { id: 1 });
      },
    });
    await client.POST('/subjects', { body: { name: '中国文学', full_score: 150 } });
    await client.GET('/subjects');
    expect(seen[0].headers.get('Authorization')).toBe('Bearer tok');
    expect(seen[0].headers.get('Idempotency-Key')).toBe('idem-1');
    expect(seen[1].headers.get('Idempotency-Key')).toBeNull();
  });

  it('401 时刷新令牌并重试一次；并发只刷新一次', async () => {
    let token = 'old';
    const refresh = vi.fn(async () => {
      token = 'new';
      return 'new';
    });
    const client = createApiClient({
      baseUrl: 'https://x.test/api/v1',
      getAccessToken: () => token,
      refresh,
      fetch: async (input) => {
        const req = input as Request;
        return req.headers.get('Authorization') === 'Bearer new'
          ? jsonResponse(200, { items: [], max_subjects: 3, can_add: true })
          : jsonResponse(401, { code: 'UNAUTHORIZED', message: '登录已失效' });
      },
    });
    const [a, b] = await Promise.all([client.GET('/subjects'), client.GET('/subjects')]);
    expect(a.response.status).toBe(200);
    expect(b.response.status).toBe(200);
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it('刷新失败时返回原来的 401', async () => {
    const client = createApiClient({
      baseUrl: 'https://x.test/api/v1',
      getAccessToken: () => 'old',
      refresh: async () => undefined,
      fetch: async () => jsonResponse(401, { code: 'UNAUTHORIZED', message: '登录已失效' }),
    });
    await expect(unwrap(client.GET('/me'))).rejects.toMatchObject({ code: 'UNAUTHORIZED', status: 401 });
  });
});

describe('unwrap', () => {
  it('成功返回数据', async () => {
    const data = await unwrap(Promise.resolve({ data: { ok: 1 }, response: new Response(null, { status: 200 }) }));
    expect(data).toEqual({ ok: 1 });
  });

  it('错误转成 ApiError', async () => {
    const err = await unwrap(
      Promise.resolve({
        error: { code: 'QUOTA_EXCEEDED', message: '今天的批改次数用完了', detail: { quota_type: 'grading' } },
        response: new Response(null, { status: 402 }),
      }),
    ).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).isQuotaExceeded).toBe(true);
    expect((err as ApiError).detail).toEqual({ quota_type: 'grading' });
  });

  it('网络错误为 NETWORK', async () => {
    const err = (await unwrap(Promise.reject(new TypeError('failed'))).catch((e: unknown) => e)) as ApiError;
    expect(err.isNetwork).toBe(true);
    expect(err.message).toContain('网络');
    expect(new ApiError(404, { code: 'NOT_FOUND', message: 'x' }).isNotFound).toBe(true);
    expect(new ApiError(401, { code: 'UNAUTHORIZED', message: 'x' }).isUnauthorized).toBe(true);
    expect(new ApiError(500, undefined).code).toBe('UNKNOWN');
  });
});
