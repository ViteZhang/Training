import { ApiError, createApiClient, unwrap } from '@training/api-client';
import * as Crypto from 'expo-crypto';
import { AppState } from 'react-native';
import { appConfig } from './config';
import { deviceId } from './device';
import { accessTokenFresh, useSession } from './session';

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

/** 刷新因网络或 5xx 失败时的重试间隔：解锁后网络通常一两秒内恢复。 */
export const REFRESH_RETRY_MS = [1000, 2500];

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function doRefresh(): Promise<string | undefined> {
  const s = useSession.getState().session;
  if (!s) return undefined;
  const client = createApiClient({ baseUrl: appConfig.apiBaseUrl, getAccessToken: () => undefined });
  try {
    let tokens;
    for (let attempt = 0; ; attempt++) {
      try {
        tokens = await unwrap(client.POST('/auth/refresh', { body: { refresh_token: s.refreshToken, device_id: deviceId() } }));
        break;
      } catch (e) {
        // 服务端明确拒绝（4xx）不重试；网络错误与 5xx 等一会儿再试。
        // 重试用同一个刷新令牌是安全的：上一次如果服务端已轮换、只是响应没收到，服务端会按「响应丢失」重新发一对。
        const retryable = !(e instanceof ApiError && e.status >= 400 && e.status < 500);
        if (!retryable || attempt >= REFRESH_RETRY_MS.length || useSession.getState().session?.refreshToken !== s.refreshToken) throw e;
        await sleep(REFRESH_RETRY_MS[attempt] ?? 0);
      }
    }
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

/** 回到前台时访问令牌已过期（或快过期）就先刷新，免得页面的第一批请求都先吃一个 401。 */
export function onAppActive(s: string) {
  if (s === 'active' && useSession.getState().session && !accessTokenFresh()) void refreshSession();
}

if (typeof process === 'undefined' || !process.env?.JEST_WORKER_ID) AppState.addEventListener('change', onAppActive);

export { unwrap };
