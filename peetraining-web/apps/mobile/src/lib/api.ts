import { ApiError, createApiClient, unwrap } from '@training/api-client';
import * as Crypto from 'expo-crypto';
import { appConfig } from './config';
import { deviceId } from './device';
import { useSession } from './session';

let refreshing: Promise<string | undefined> | undefined;

/**
 * 用刷新令牌换一对新令牌，返回新的访问令牌；同一时间只发一次请求。
 * 只有服务端明确拒绝（401：刷新令牌过期、已作废、账号停用）才清除登录态，路由守卫回到 0.2 登录页。
 * 断网、超时、服务端 5xx 时保留登录态，返回 undefined：手机锁屏后刚解锁时网络往往还没恢复，
 * 这时清掉登录态会让用户莫名其妙被退出登录。
 */
export function refreshSession(): Promise<string | undefined> {
  refreshing ??= doRefresh().finally(() => {
    refreshing = undefined;
  });
  return refreshing;
}

async function doRefresh(): Promise<string | undefined> {
  const s = useSession.getState().session;
  if (!s) return undefined;
  try {
    const tokens = await unwrap(
      createApiClient({ baseUrl: appConfig.apiBaseUrl, getAccessToken: () => undefined }).POST('/auth/refresh', {
        body: { refresh_token: s.refreshToken, device_id: deviceId() },
      }),
    );
    useSession.getState().setSession({
      accessToken: tokens.access_token,
      accessExpiresAt: tokens.access_expires_at,
      refreshToken: tokens.refresh_token,
      refreshExpiresAt: tokens.refresh_expires_at,
    });
    return tokens.access_token;
  } catch (e) {
    // 刷新期间如果用户已重新登录（令牌变了），不要清掉新的登录态。
    if (e instanceof ApiError && e.status === 401 && useSession.getState().session?.refreshToken === s.refreshToken) {
      useSession.getState().clear();
    }
    return undefined;
  }
}

/**
 * 全局接口客户端。令牌过期时自动刷新一次（见 refreshSession）。
 * 写请求自动带 Idempotency-Key（CLAUDE.md 必须遵守第 7 条）。
 */
export const api = createApiClient({
  baseUrl: appConfig.apiBaseUrl,
  getAccessToken: () => useSession.getState().session?.accessToken,
  newIdempotencyKey: () => Crypto.randomUUID(),
  refresh: refreshSession,
});

export { unwrap };
