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
