import type { Schemas } from '@training/api-client';
import * as Crypto from 'expo-crypto';
import { Directory, File, Paths } from 'expo-file-system';
import { useSyncExternalStore } from 'react';
import { api, unwrap } from '@/lib/api';
import { getJSON, setJSON } from '@/lib/storage';
import { uploadVoice, type VoiceClip } from './voiceUpload';

export type VoiceJob = Schemas['VoiceTranscription'];
export type LocalVoice = { key: string; order?: number; clip: VoiceClip; objectKey?: string; status: 'uploading' | 'registered' | 'failed'; error?: string; job?: VoiceJob };
export class PracticeVoiceQueue {
  private records: Record<string, LocalVoice>;
  private listeners = new Set<() => void>();
  private running = new Map<string, Promise<void>>();
  readonly storageKey: string;
  constructor(readonly sessionId: number, readonly questionId: number) {
    this.storageKey = `practice:voice:${sessionId}:${questionId}`;
    this.records = getJSON<Record<string, LocalVoice>>(this.storageKey) ?? {};
  }
  snapshot = () => this.records;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  private update(key: string, value: LocalVoice) { this.records = { ...this.records, [key]: value }; this.save(); }
  private save() { setJSON(this.storageKey, this.records); this.listeners.forEach((fn) => fn()); }
  add(clip: VoiceClip) {
    const key = Crypto.randomUUID();
    const order = Math.max(Date.now() * 1000, ...Object.values(this.records).map((record) => (record.order ?? 0) + 1));
    this.update(key, { key, order, clip, status: 'uploading' });
    void this.run(key).catch(() => undefined);
    return key;
  }
  run(key: string) {
    const previous = this.running.get(key);
    if (previous) return previous;
    const work = this.process(key).finally(() => this.running.delete(key));
    this.running.set(key, work); return work;
  }
  private async process(key: string) {
    let record = this.records[key];
    if (!record || record.status === 'registered') return;
    this.update(key, { ...record, status: 'uploading', error: undefined });
    try {
      if (!record.objectKey) {
        const dir = new Directory(Paths.document, 'practice-voice'); dir.create({ intermediates: true, idempotent: true });
        if (!record.clip.uri.startsWith(dir.uri)) {
          const target = new File(dir, `${this.sessionId}-${this.questionId}-${key}.m4a`);
          new File(record.clip.uri).copy(target);
          record = { ...record, clip: { ...record.clip, uri: target.uri } }; this.update(key, record);
        }
        const objectKey = await uploadVoice(record.clip);
        record = { ...record, objectKey }; this.update(key, record);
      }
      const job = await this.register(record.key, record.objectKey!);
      this.update(key, { ...record, status: 'registered', job, error: undefined });
    } catch (error) {
      if (!this.records[key] || this.records[key]?.status === 'registered') return;
      this.update(key, { ...record, status: 'failed', error: error instanceof Error ? error.message : '录音上传失败' });
      throw error;
    }
  }
  register(key: string, objectKey: string) {
    return unwrap(api.POST('/practice-sessions/{sessionId}/questions/{questionId}/voice', { params: { path: { sessionId: this.sessionId, questionId: this.questionId } }, body: { object_key: objectKey, idempotency_key: key, capture_order: this.records[key]?.order ?? 0 } }));
  }
  async list() {
    const response = await unwrap(api.GET('/practice-sessions/{sessionId}/questions/{questionId}/voice', { params: { path: { sessionId: this.sessionId, questionId: this.questionId } } }));
    for (const job of response.items) {
      const local = this.records[job.idempotency_key];
      if (local && local.status !== 'registered') this.update(local.key, { ...local, status: 'registered', job, error: undefined });
    }
    return response.items;
  }
  resume() { Object.values(this.records).filter((record) => record.status === 'uploading').forEach((record) => { void this.run(record.key).catch(() => undefined); }); }
  async waitForUploads() {
    await Promise.allSettled([...this.running.values()]);
    if (Object.values(this.records).some((record) => record.status !== 'registered')) throw new Error('录音尚未上传保存成功，请重试或弃用后提交');
  }
  async discard(record: LocalVoice | VoiceJob) {
    if ('clip' in record && this.running.has(record.key)) return;
    let job = 'clip' in record ? record.job : record;
    // 登记响应丢失时先用同一标识找回，确保不会在服务端留下被本机静默删除的任务。
    if ('clip' in record && !job && record.objectKey) job = await this.register(record.key, record.objectKey);
    if (job) await unwrap(api.DELETE('/practice-sessions/{sessionId}/questions/{questionId}/voice/{voiceId}', { params: { path: { sessionId: this.sessionId, questionId: this.questionId, voiceId: job.id } } }));
    const key = 'clip' in record ? record.key : record.idempotency_key;
    const local = this.records[key];
    if (local?.clip.uri.startsWith(new Directory(Paths.document, 'practice-voice').uri)) { try { new File(local.clip.uri).delete(); } catch { /* 仅清理本机副本 */ } }
    const next = { ...this.records }; delete next[key]; this.records = next; this.save();
  }
}
const queues = new Map<string, PracticeVoiceQueue>();
export function practiceVoiceQueue(sessionId: number, questionId: number) {
  const key = `${sessionId}:${questionId}`;
  let queue = queues.get(key);
  if (!queue) { queue = new PracticeVoiceQueue(sessionId, questionId); queues.set(key, queue); }
  return queue;
}
export function useLocalVoice(queue: PracticeVoiceQueue) { return useSyncExternalStore(queue.subscribe, queue.snapshot, queue.snapshot); }
