import { useSyncExternalStore } from 'react';

/**
 * 后台会话令牌放 localStorage，同一浏览器的多个标签页共用一个登录态，可以多开标签页。
 * 任一标签页登录或退出，其他标签页通过 storage 事件同步；会话本身由后端 8 小时过期。
 */
const KEY = 'admin_token';
const listeners = new Set<() => void>();

function read(): string | undefined {
  try {
    return localStorage.getItem(KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

let token = read();

function notify() {
  listeners.forEach((l) => l());
}

// 别的标签页登录、退出或会话过期时，本页跟着更新。
globalThis.addEventListener?.('storage', (e: StorageEvent) => {
  if (e.key !== KEY && e.key !== null) return;
  const next = read();
  if (next === token) return;
  token = next;
  notify();
});

export function getToken() {
  return token;
}

export function setSession(t: string) {
  token = t;
  try {
    localStorage.setItem(KEY, t);
  } catch {
    // 隐私模式下存不了，只在内存里。
  }
  notify();
}

export function clearSession() {
  token = undefined;
  try {
    localStorage.removeItem(KEY);
  } catch {
    // 忽略
  }
  notify();
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
