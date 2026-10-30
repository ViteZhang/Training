// 按考试、题号保存照片；上传和服务端任务登记不依赖当前题目的组件生命周期。
import type { Schemas } from '@training/api-client';
import * as Crypto from 'expo-crypto';
import { Directory, File, Paths } from 'expo-file-system';
import { useState, useSyncExternalStore } from 'react';
import { uploadPhotos, type Shot } from '@/features/practice/handwriting';
import { api, unwrap } from '@/lib/api';
import { uploadVoice, type VoiceClip } from '@/features/practice/voiceUpload';
import { getJSON, remove, setJSON } from '@/lib/storage';

export type PaperHandwriting = Schemas['PaperHandwriting'];
export type LocalPhoto = {
  key: string;
  order?: number;
  seq: number;
  shots: Shot[];
  inputKind?: 'photo' | 'voice';
  keys?: string[];
  job?: PaperHandwriting;
  status: 'uploading' | 'registered' | 'failed';
  error?: string;
  confirmingText?: string;
};
const storageKey = (id: number) => `paper:photos:${id}`;
const editsKey = (id: number) => `paper:photo-edits:${id}`;
export const loadPhotoEdits = (id: number) => getJSON<Record<string, string>>(editsKey(id)) ?? {};
export const savePhotoEdits = (id: number, edits: Record<string, string>) => setJSON(editsKey(id), edits);
export const loadLocalPhotos = (id: number) => getJSON<Record<string, LocalPhoto>>(storageKey(id)) ?? {};

export class PaperPhotoQueue {
  private records: Record<string, LocalPhoto>;
  private listeners = new Set<() => void>();
  private running = new Map<string, Promise<void>>();
  constructor(readonly sessionId: number) { this.records = loadLocalPhotos(sessionId); }
  snapshot = () => this.records;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private update(key: string, record: LocalPhoto) {
    this.records = { ...this.records, [key]: record };
    setJSON(storageKey(this.sessionId), this.records);
    this.listeners.forEach((listener) => listener());
  }
  addVoice(seq: number, clip: VoiceClip) { return this.add(seq, [clip], 'voice'); }
  add(seq: number, shots: Shot[], inputKind: 'photo' | 'voice' = 'photo') {
    const key = Crypto.randomUUID();
    const order = Math.max(Date.now() * 1000, ...Object.values(this.records).map((photo) => (photo.order ?? 0) + 1));
    // 先记录相机 URI；即使复制或网络失败也不丢拍照内容。
    this.update(key, { key, order, seq, shots, inputKind, status: 'uploading' });
    void this.run(key).catch(() => undefined);
    return key;
  }
  run(key: string): Promise<void> {
    const existing = this.running.get(key);
    if (existing) return existing;
    const work = this.process(key).finally(() => this.running.delete(key));
    this.running.set(key, work);
    return work;
  }
  private async process(key: string) {
    let photo = this.records[key];
    if (!photo || photo.status === 'registered') return;
    this.update(key, { ...photo, status: 'uploading', error: undefined });
    try {
      if (!photo.keys) {
        const directory = new Directory(Paths.document, 'paper-handwriting');
        directory.create({ idempotent: true, intermediates: true });
        const shots = photo.shots.map((shot, i) => {
          if (shot.uri.startsWith(directory.uri)) return shot;
          const extension = photo!.inputKind === 'voice' ? 'm4a' : shot.mimeType === 'image/png' ? 'png' : shot.mimeType === 'image/heic' ? 'heic' : shot.mimeType === 'image/webp' ? 'webp' : 'jpg';
          const saved = new File(directory, `${this.sessionId}-${key}-${i}.${extension}`);
          new File(shot.uri).copy(saved);
          return { ...shot, uri: saved.uri };
        });
        photo = { ...photo, shots };
        this.update(key, { ...photo, status: 'uploading' });
        const keys = photo.inputKind === 'voice' ? [await uploadVoice(shots[0] as VoiceClip)] : await uploadPhotos(shots);
        photo = { ...photo, keys };
        this.update(key, { ...photo, status: 'uploading' });
      }
      const job = await unwrap(api.POST('/paper-sessions/{sessionId}/items/{seq}/handwriting', {
        params: { path: { sessionId: this.sessionId, seq: photo.seq } },
        body: { object_keys: photo.keys!, idempotency_key: key, capture_order: photo.order ?? 0, ...(photo.inputKind === 'voice' ? { input_kind: 'voice' as const } : {}) },
      }));
      if (!this.records[key]) return;
      this.update(key, { ...photo, status: 'registered', job, error: undefined });
    } catch (error) {
      if (!this.records[key]) return;
      // 登记成功但响应丢失时，服务端轮询可能已找回原任务。
      if (this.records[key]?.status === 'registered') return;
      const message = error instanceof Error ? error.message : '照片保存失败，请重试';
      this.update(key, { ...photo, status: 'failed', error: message });
      throw error;
    }
  }
  resume() {
    for (const photo of Object.values(this.records)) {
      if (photo.status === 'uploading') void this.run(photo.key).catch(() => undefined);
    }
  }
  reconcile(items: Schemas['PaperItem'][]) {
    for (const item of items) for (const job of item.handwriting ?? []) {
      const photo = this.records[job.idempotency_key];
      if (photo && photo.status !== 'registered') this.update(photo.key, { ...photo, status: 'registered', job, error: undefined });
    }
  }
  async confirm(job: PaperHandwriting, text: string, confirm: boolean, apply: (item: Schemas['PaperItem']) => boolean) {
    const key = job.idempotency_key;
    if (confirm) {
      const photo = this.records[key] ?? { key, seq: job.seq, order: job.capture_order, shots: [], inputKind: job.input_kind, keys: job.object_keys, job, status: 'registered' as const };
      this.update(key, { ...photo, confirmingText: text });
    }
    const item = await unwrap(api.PUT('/paper-sessions/{sessionId}/items/{seq}/handwriting/{handwritingId}', {
      params: { path: { sessionId: this.sessionId, seq: job.seq, handwritingId: job.id } }, body: { text, confirm },
    }));
    // 页面已经离开时保留确认记录；下一次进入先找回服务端结果，再允许写草稿。
    if (apply(item) && confirm && this.records[key]) {
      this.update(key, { ...this.records[key]!, job: item.handwriting?.find((candidate) => candidate.id === job.id) ?? job, confirmingText: undefined });
    }
  }
  async recoverConfirmations(apply: (item: Schemas['PaperItem']) => boolean) {
    for (const photo of Object.values(this.records)) {
      if (photo.confirmingText !== undefined && photo.job) await this.confirm(photo.job, photo.confirmingText, true, apply);
    }
  }
  async waitForUploads() {
    const results = await Promise.allSettled([...this.running.values()]);
    const failed = Object.values(this.records).find((photo) => photo.status !== 'registered');
    if (failed || results.some((result) => result.status === 'rejected')) throw new Error(`第 ${failed?.seq ?? ''} 题照片或录音尚未保存成功，请重试后交卷`);
  }
  discardLocal(key: string) {
    // 上传中不能删除：避免后台稍后登记已被界面丢弃的照片。
    if (this.running.has(key)) return;
    const next = { ...this.records };
    delete next[key];
    this.records = next;
    setJSON(storageKey(this.sessionId), next);
    this.listeners.forEach((listener) => listener());
  }
  clearRegistered() {
    const remaining: Record<string, LocalPhoto> = {};
    for (const photo of Object.values(this.records)) {
      if (photo.status !== 'registered') { remaining[photo.key] = photo; continue; }
      for (const shot of photo.shots) {
        try { const file = new File(shot.uri); if (file.exists && shot.uri.startsWith(Paths.document.uri)) file.delete(); } catch { /* 仅清理本 App 保存的副本。 */ }
      }
    }
    this.records = remaining;
    if (Object.keys(remaining).length) setJSON(storageKey(this.sessionId), remaining);
    else remove(storageKey(this.sessionId));
    remove(editsKey(this.sessionId));
    this.listeners.forEach((listener) => listener());
  }
}
const queues = new Map<number, PaperPhotoQueue>();
export function paperPhotoQueue(id: number) {
  let queue = queues.get(id);
  if (!queue) { queue = new PaperPhotoQueue(id); queues.set(id, queue); }
  return queue;
}
export function usePaperPhotos(id: number) {
  const [queue] = useState(() => paperPhotoQueue(id));
  return { queue, photos: useSyncExternalStore(queue.subscribe, queue.snapshot, queue.snapshot) };
}

/** 同一次登记的本地快照和服务端轮询合并，后者为最终状态。 */
export function mergePaperPhotos(items: Schemas['PaperItem'][], photos: Record<string, LocalPhoto>): PaperHandwriting[] {
  const jobs = new Map<string, PaperHandwriting>();
  for (const photo of Object.values(photos)) if (photo.job) jobs.set(photo.key, photo.job);
  for (const item of items) for (const job of item.handwriting ?? []) jobs.set(job.idempotency_key, job);
  return [...jobs.values()].filter((job) => job.status !== 'discarded').sort((a, b) => (a.capture_order ?? a.id) - (b.capture_order ?? b.id) || a.id - b.id);
}
