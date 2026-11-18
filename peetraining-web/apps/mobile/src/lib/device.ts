import * as Crypto from 'expo-crypto';
import { Platform } from 'react-native';
import { storage } from './storage';

const KEY = 'device_id';

/** 设备标识：首次启动生成并保存，用于按设备管理刷新令牌（6.11 登录设备）。 */
export function deviceId(): string {
  let id = storage.getString(KEY);
  if (!id) {
    id = Crypto.randomUUID();
    storage.set(KEY, id);
  }
  return id;
}

export const platform = Platform.OS === 'ios' ? 'ios' : 'android';

/** 登录设备的平台：H5 版报 web（6.11 登录设备里显示「网页版」）。 */
export const devicePlatform: 'ios' | 'android' | 'web' = Platform.OS === 'web' ? 'web' : platform;

/** H5 版的设备名：浏览器拿不到机型，按 UA 粗分。 */
export function webDeviceName(): string {
  const ua = typeof navigator === 'undefined' ? '' : navigator.userAgent;
  const device = /iPhone/.test(ua) ? 'iPhone' : /iPad|Macintosh/.test(ua) ? (/Mobile|iPad/.test(ua) ? 'iPad' : 'Mac') : /Android/.test(ua) ? '安卓' : /Windows/.test(ua) ? 'Windows' : '';
  const app = /MicroMessenger/.test(ua) ? '微信' : '浏览器';
  return `网页版（${device ? device + ' ' : ''}${app}）`;
}
