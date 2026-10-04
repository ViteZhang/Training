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
    set({ session: undefined });
  },
}));
