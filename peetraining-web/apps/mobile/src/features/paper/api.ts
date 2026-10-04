import { ApiError, type Schemas } from '@training/api-client';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from '@/lib/api';
import { getJSON, remove, setJSON } from '@/lib/storage';

export type PaperSession = Schemas['PaperSession'];
export type PaperItem = Schemas['PaperItem'];

export const paperKeys = {
  list: (subjectId: number) => ['papers', subjectId] as const,
  paper: (id: number) => ['paper', id] as const,
  session: (id: number) => ['paper-session', id] as const,
};

export function usePapers(subjectId: number) {
  return useQuery({ queryKey: paperKeys.list(subjectId), queryFn: () => unwrap(api.GET('/subjects/{subjectId}/papers', { params: { path: { subjectId } } })) });
}

export function usePaper(id: number) {
  return useQuery({ queryKey: paperKeys.paper(id), queryFn: () => unwrap(api.GET('/papers/{paperId}', { params: { path: { paperId: id } } })) });
}

export const modeNames: Record<Schemas['PaperMode'], string> = { practice: '练习模式', mock: '模拟考试' };

/** 作答草稿本地保存（每 5 秒，CLAUDE.md 必须遵守第 7 条）；杀掉 App 再打开时优先用本地的。 */
const draftKey = (sessionId: number) => `draft:paper:${sessionId}`;
export function loadDrafts(sessionId: number): Record<number, string> {
  return getJSON<Record<number, string>>(draftKey(sessionId)) ?? {};
}
export function saveDrafts(sessionId: number, drafts: Record<number, string>) {
  setJSON(draftKey(sessionId), drafts);
}
export function clearDrafts(sessionId: number) {
  remove(draftKey(sessionId));
  remove(activeKey(sessionId));
}

/** 最后一次在作答页的时间：模拟考试被系统中断后据此申请恢复（PRD 11.9）。 */
const activeKey = (sessionId: number) => `paper:active:${sessionId}`;
export function markActive(sessionId: number, at = Date.now()) {
  setJSON(activeKey(sessionId), at);
}
export function lastActive(sessionId: number): number | undefined {
  return getJSON<number>(activeKey(sessionId));
}

export function clock(sec: number) {
  const s = Math.max(0, Math.floor(sec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
}

export type PaperReport = Schemas['PaperReport'];

/** 整卷报告（4.24、4.25）：还在批改时服务端返回 409，每 15 秒再取一次。 */
export function usePaperReport(sessionId: number) {
  return useQuery({
    queryKey: [...paperKeys.session(sessionId), 'report'] as const,
    queryFn: () => unwrap(api.GET('/paper-sessions/{sessionId}/report', { params: { path: { sessionId } } })),
    retry: false,
    refetchInterval: (q) => (q.state.error instanceof ApiError && q.state.error.status === 409 ? 15000 : false),
  });
}

export const lossNames: Record<keyof Schemas['LossPoints'], string> = { knowledge: '知识没掌握', norm: '答题不规范', time: '时间不够' };
