import { createMMKV } from 'react-native-mmkv';

/**
 * 本地存储（MMKV，同步读写）。用途：令牌、作答草稿（每 5 秒保存）、功能开关缓存、最近搜索。
 * 不存手机号明文以外的敏感信息；退出登录时清除用户相关的键。
 */
export const storage = createMMKV({ id: 'training' });

export function getJSON<T>(key: string): T | undefined {
  const raw = storage.getString(key);
  if (raw === undefined) return undefined;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return undefined;
  }
}

export function setJSON(key: string, value: unknown) {
  storage.set(key, JSON.stringify(value));
}

export function remove(key: string) {
  storage.remove(key);
}
