import { createApiClient, unwrap } from '@training/api-client';
import * as Crypto from 'expo-crypto';
import { appConfig } from './config';
import { deviceId } from './device';
import { useSession } from './session';

/**
 * 全局接口客户端。令牌过期时自动刷新一次；刷新失败清除登录态，路由守卫回到 0.2 登录页。
 * 写请求自动带 Idempotency-Key（CLAUDE.md 必须遵守第 7 条）。
 */
export const api = createApiClient({
  baseUrl: appConfig.apiBaseUrl,
  getAccessToken: () => useSession.getState().session?.accessToken,
  newIdempotencyKey: () => Crypto.randomUUID(),
  refresh: async () => {
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
    } catch {
      useSession.getState().clear();
      return undefined;
    }
  },
});

export { unwrap };
