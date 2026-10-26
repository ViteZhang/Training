import { create } from 'zustand';
import { getJSON, remove, setJSON } from './storage';

const KEY = 'session';

export interface Session {
  accessToken: string;
  accessExpiresAt: string;
  refreshToken: string;
  refreshExpiresAt: string;
}

interface SessionState {
  session?: Session;
  setSession: (s: Session) => void;
  clear: () => void;
}

/** 登录态。令牌存在 MMKV；客户端不放任何云服务密钥。 */
export const useSession = create<SessionState>((set) => ({
  session: getJSON<Session>(KEY),
  setSession: (session) => {
    setJSON(KEY, session);
    set({ session });
  },
  clear: () => {
    remove(KEY);
    remove('onboarding_step'); // 启动页记下的引导进度，换账号登录时不能沿用
    set({ session: undefined });
  },
}));

/** 访问令牌在 marginMs 之后仍有效（没登录返回 false）。 */
export function accessTokenFresh(marginMs = 60_000, now = Date.now()): boolean {
  const s = useSession.getState().session;
  return !!s && Date.parse(s.accessExpiresAt) - now > marginMs;
}
