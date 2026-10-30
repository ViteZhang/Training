import * as Crypto from 'expo-crypto';
import { Directory, File, Paths } from 'expo-file-system';
import type { ResolvedSharePayload } from 'expo-sharing';
import { useSyncExternalStore } from 'react';
import { Platform } from 'react-native';
import { getJSON, setJSON } from '@/lib/storage';
import { checkLimits, formatOf, type Picked } from './files';

type SharedFile = Picked & { sourceUri: string };
const KEY = 'import:shared-inbox';
const RECEIPTS = `${KEY}:receipts`;
let received = new Set(getJSON<string[]>(RECEIPTS) ?? []);
export function clearSharedReceipts() { received = new Set(); setJSON(RECEIPTS, []); }
let files = getJSON<SharedFile[]>(KEY) ?? [];
const listeners = new Set<() => void>();
const publish = (next: SharedFile[]) => { files = next; setJSON(KEY, files); listeners.forEach((listener) => listener()); };
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };
export function useSharedInbox() { return useSyncExternalStore(subscribe, () => files, () => files); }
export const sharedInbox = () => files;

/** 先复制出微信/系统的临时授权 URI，再等待登录、选择课程；不会因跳转而丢失。 */
export async function receiveSharedFiles(payloads: ResolvedSharePayload[], preserveReceipts = false) {
  const errors: string[] = [];
  for (const payload of payloads) {
    // Android 解析器的缓存文件名可能重复；优先复制原始授权 URI，防止同名文件覆盖或误去重。
    const uri = Platform.OS === 'android' && /^(file|content):\/\//.test(payload.value) ? payload.value : payload.contentUri;
    if (!uri || !/^(file|content):\/\//.test(uri) || !['file', 'image'].includes(payload.contentType ?? '')) {
      errors.push('请分享文件或图片；聊天文字、网页链接和视频暂不支持');
      continue;
    }
    if ((preserveReceipts && received.has(uri)) || files.some((file) => file.sourceUri === uri)) continue;
    try {
      const source = new File(uri);
      const name = (payload.originalName ?? decodeURIComponent(uri.split('/').pop() ?? '文件')).replace(/[\\/]/g, '_');
      const format = formatOf(name, payload.contentMimeType ?? payload.mimeType);
      const size = source.size || payload.contentSize;
      if (!size) throw new Error(`「${name}」为空，请先在微信下载完成后再分享`);
      const error = checkLimits(files, [{ name, format, size }]);
      if (error) throw new Error(error);
      const key = Crypto.randomUUID();
      const directory = new Directory(Paths.document, 'shared-imports');
      directory.create({ intermediates: true, idempotent: true });
      const target = new File(directory, `${key}-${name}`);
      source.copy(target);
      const actualSize = target.size;
      const actualError = !actualSize ? '文件为空或未下载完成' : checkLimits(files, [{ name, format, size: actualSize }]);
      if (actualError) { target.delete(); throw new Error(actualError); }
      publish([...files, { key, sharedKey: key, sourceUri: uri, name, format, size: actualSize, uri: target.uri, mimeType: payload.contentMimeType ?? payload.mimeType, status: 'pending', progress: 0 }]);
      // 部分成功时原生载荷仍需重试；已导入并从列表移除的文件也不能再次回灌。
      if (preserveReceipts) { received.add(uri); setJSON(RECEIPTS, [...received]); }
    } catch (error) { errors.push(error instanceof Error ? error.message : '文件保存失败，请重新分享'); }
  }
  return errors;
}

/** 开始上传不会移除待导入文件；只有解析任务成功建立或用户明确移除才清理。 */
export function removeSharedFiles(keys: string[]) {
  const removing = new Set(keys);
  for (const file of files) {
    if (removing.has(file.key) && file.uri?.startsWith(new Directory(Paths.document, 'shared-imports').uri)) {
      try { new File(file.uri).delete(); } catch { /* 文件已移除时仍清理列表。 */ }
    }
  }
  publish(files.filter((file) => !removing.has(file.key)));
}
