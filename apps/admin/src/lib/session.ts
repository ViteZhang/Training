import { useSyncExternalStore } from 'react';

/** 后台会话令牌放 sessionStorage：关掉浏览器标签页即退出；不放 localStorage，降低被别的页面读到的风险。 */
const KEY = 'admin_token';
const listeners = new Set<() => void>();

function read(): string | undefined {
  try {
    return sessionStorage.getItem(KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

let token = read();

export function getToken() {
  return token;
}

export function setSession(t: string) {
  token = t;
  try {
    sessionStorage.setItem(KEY, t);
  } catch {
    // 隐私模式下存不了，只在内存里。
  }
  listeners.forEach((l) => l());
}

export function clearSession() {
  token = undefined;
  try {
    sessionStorage.removeItem(KEY);
  } catch {
    // 忽略
  }
  listeners.forEach((l) => l());
}

export function useToken() {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => token,
  );
}
