import { refreshSession } from '../lib/api';
import { useSession } from '../lib/session';

jest.mock('expo-crypto', () => ({ randomUUID: () => 'uuid-1234-5678' }));

const session = { accessToken: 'a', accessExpiresAt: '2000-01-01T00:00:00Z', refreshToken: 'r', refreshExpiresAt: '2099-01-01T00:00:00Z' };
const realFetch = globalThis.fetch;

function respond(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

beforeEach(() => {
  useSession.getState().setSession(session);
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

describe('刷新令牌', () => {
  it('成功时换成新令牌', async () => {
    globalThis.fetch = jest.fn(async () =>
      respond(200, { access_token: 'a2', access_expires_at: '2099-01-01T00:00:00Z', refresh_token: 'r2', refresh_expires_at: '2099-01-01T00:00:00Z' }),
    ) as unknown as typeof fetch;
    await expect(refreshSession()).resolves.toBe('a2');
    expect(useSession.getState().session?.refreshToken).toBe('r2');
  });

  it('锁屏解锁后网络还没恢复：刷新失败但不退出登录', async () => {
    globalThis.fetch = jest.fn(async () => {
      throw new TypeError('Network request failed');
    }) as unknown as typeof fetch;
    await expect(refreshSession()).resolves.toBeUndefined();
    expect(useSession.getState().session?.refreshToken).toBe('r');
  });

  it('服务端 5xx 时不退出登录', async () => {
    globalThis.fetch = jest.fn(async () => respond(502, { code: 'INTERNAL', message: '服务异常' })) as unknown as typeof fetch;
    await expect(refreshSession()).resolves.toBeUndefined();
    expect(useSession.getState().session).toBeDefined();
  });

  it('服务端明确拒绝（401）时退出登录', async () => {
    globalThis.fetch = jest.fn(async () => respond(401, { code: 'UNAUTHORIZED', message: '登录已失效' })) as unknown as typeof fetch;
    await expect(refreshSession()).resolves.toBeUndefined();
    expect(useSession.getState().session).toBeUndefined();
  });

  it('并发只刷新一次', async () => {
    const fetchMock = jest.fn(async () =>
      respond(200, { access_token: 'a3', access_expires_at: '2099-01-01T00:00:00Z', refresh_token: 'r3', refresh_expires_at: '2099-01-01T00:00:00Z' }),
    );
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    await Promise.all([refreshSession(), refreshSession()]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
