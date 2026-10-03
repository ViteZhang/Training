import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router } from 'expo-router';
import { toast } from '@/components';
import { api, unwrap } from '@/lib/api';
import { getJSON, remove, setJSON } from '@/lib/storage';

export type Essay = Schemas['Essay'];
export type EssayHome = Schemas['EssayHome'];

export const essayKeys = {
  home: (subjectId: number) => ['essay', 'home', subjectId] as const,
  essay: (id: number) => ['essay', id] as const,
  book: (subjectId: number) => ['essay', 'book', subjectId] as const,
  rubrics: (subjectId: number) => ['essay', 'rubrics', subjectId] as const,
  model: (id: number) => ['essay', 'model', id] as const,
  kb: (subjectId: number) => ['essay', 'kb', subjectId] as const,
};

export const sourceNames: Record<Schemas['EssayTopicSource'], string> = { exam: '真题', ai: 'AI 命题', custom: '自拟' };

export function useEssayHome(subjectId: number) {
  return useQuery({
    queryKey: essayKeys.home(subjectId),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/essay-home', { params: { path: { subjectId } } })),
    enabled: subjectId > 0,
  });
}

/** 一篇作文；批改中每 5 秒刷新一次（约 30 秒出结果）。 */
export function useEssay(id: number) {
  return useQuery({
    queryKey: essayKeys.essay(id),
    queryFn: () => unwrap(api.GET('/essays/{essayId}', { params: { path: { essayId: id } } })),
    refetchInterval: (q) => (q.state.data?.status === 'grading' ? 5000 : false),
  });
}

export function useNotebook(subjectId: number) {
  return useQuery({ queryKey: essayKeys.book(subjectId), queryFn: () => unwrap(api.GET('/subjects/{subjectId}/essays', { params: { path: { subjectId } } })) });
}

export function useRubrics(subjectId: number) {
  return useQuery({
    queryKey: essayKeys.rubrics(subjectId),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/essay-rubrics', { params: { path: { subjectId } } })),
  });
}

export function useModelEssay(id: number) {
  return useQuery({ queryKey: essayKeys.model(id), queryFn: () => unwrap(api.GET('/model-essays/{modelEssayId}', { params: { path: { modelEssayId: id } } })) });
}

/** 素材（5.3）：来自作文知识库（3.10）。 */
export function useEssayKB(subjectId: number, enabled: boolean) {
  return useQuery({
    queryKey: essayKeys.kb(subjectId),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/essay-kb', { params: { path: { subjectId } } })),
    enabled,
  });
}

type CreateBody = {
  subject_id?: number;
  topic_source?: Schemas['EssayTopicSource'];
  question_id?: number;
  ai_topic_id?: number;
  topic_text?: string;
  required_words?: number;
  parent_essay_id?: number;
};

/** 开始写一篇（带幂等键）并进入写作页。 */
export function useCreateEssay() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateBody) => unwrap(api.POST('/essays', { body: { ...body, idempotency_key: Crypto.randomUUID() } })),
    onSuccess: (e) => {
      qc.setQueryData(essayKeys.essay(e.id), e);
      void qc.invalidateQueries({ queryKey: essayKeys.home(e.subject_id) });
      router.push({ pathname: '/essay/write/[id]', params: { id: String(e.id) } });
    },
    onError: (e) => toast(e instanceof Error ? e.message : '没能开始，请重试'),
  });
}

export function useGenerateTopic(subjectId: number, onQuota: () => void) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST('/subjects/{subjectId}/essay-topics', { params: { path: { subjectId } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: essayKeys.home(subjectId) }),
    onError: (e) => (e instanceof ApiError && e.isQuotaExceeded ? onQuota() : toast(e instanceof Error ? e.message : '出题失败，请重试')),
  });
}

/** 写作草稿本地保存（每 5 秒，CLAUDE.md 必须遵守第 7 条）；杀掉 App 再打开时本地的优先。 */
type LocalDraft = { content: string; seconds: number };
const draftKey = (id: number) => `draft:essay:${id}`;
export function loadEssayDraft(id: number): LocalDraft | undefined {
  return getJSON<LocalDraft>(draftKey(id));
}
export function saveEssayDraft(id: number, d: LocalDraft) {
  setJSON(draftKey(id), d);
}
export function clearEssayDraft(id: number) {
  remove(draftKey(id));
}

/** 字数：去掉空白后的字符数（与服务端一致）。 */
export function countWords(s: string) {
  return [...s.replace(/\s/g, '')].length;
}

export function fmtScore(v?: number) {
  return v === undefined ? '-' : String(Math.round(v * 10) / 10);
}
