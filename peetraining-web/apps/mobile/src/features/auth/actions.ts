import type { Schemas } from '@training/api-client';
import { api } from '@/lib/api';
import { queryClient } from '@/lib/queryClient';
import { useSession } from '@/lib/session';

/** 保存登录结果。 */
export function saveTokens(t: Schemas['TokenPair']) {
  useSession.getState().setSession({
    accessToken: t.access_token,
    accessExpiresAt: t.access_expires_at,
    refreshToken: t.refresh_token,
    refreshExpiresAt: t.refresh_expires_at,
  });
}

/** 退出立即清本地状态；服务端撤销本设备令牌作为限时尽力请求。 */
export function logout(): Promise<void> {
  const revocation = api.POST('/auth/logout').catch(() => undefined);
  useSession.getState().clear();
  void queryClient.cancelQueries();
  queryClient.clear();
  void revocation;
  return Promise.resolve();
}
