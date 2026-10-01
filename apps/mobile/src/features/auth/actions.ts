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

/** 退出登录：作废本设备令牌（失败也继续），清除本地登录态与缓存。题库和记录都在服务端，不受影响。 */
export async function logout() {
  try {
    await api.POST('/auth/logout');
  } catch {
    // 离线时也允许退出，服务端令牌到期自然失效。
  }
  useSession.getState().clear();
  queryClient.clear();
}
