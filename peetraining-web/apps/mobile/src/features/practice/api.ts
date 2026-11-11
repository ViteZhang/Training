import type { Schemas } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { toast } from '@/components';
import { api, unwrap } from '@/lib/api';
import { cachedSession, cacheSession } from './offline';
import { track } from '@/lib/analytics';

export type PracticeSession = Schemas['PracticeSession'];
export type PracticeQuestion = Schemas['PracticeQuestion'];
export type PracticeConfig = Schemas['PracticeConfig'];
export type PracticeKind = Schemas['PracticeKind'];

export const practiceKeys = {
  home: (subjectId: number) => ['practice', 'home', subjectId] as const,
  preview: (subjectId: number, c: PracticeConfig) => ['practice', 'preview', subjectId, c] as const,
  session: (id: number) => ['practice', 'session', id] as const,
  summary: (id: number) => ['practice', 'summary', id] as const,
  wrong: (subjectId: number) => ['practice', 'wrong', subjectId] as const,
};

export function usePracticeHome(subjectId?: number) {
  return useQuery({
    queryKey: practiceKeys.home(subjectId ?? 0),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/practice', { params: { path: { subjectId: subjectId! } } })),
    enabled: !!subjectId,
  });
}

export function usePreview(subjectId: number, config: PracticeConfig) {
  return useQuery({
    queryKey: practiceKeys.preview(subjectId, config),
    queryFn: () => unwrap(api.POST('/subjects/{subjectId}/practice/preview', { params: { path: { subjectId } }, body: config })),
    placeholderData: (prev) => prev,
  });
}

/** 会话（含整组题与客观题答案）。拉到后存本地，断网时用本地缓存继续做。 */
export function useSession(id: number) {
  return useQuery({
    queryKey: practiceKeys.session(id),
    queryFn: async () => {
      try {
        const s = await unwrap(api.GET('/practice-sessions/{sessionId}', { params: { path: { sessionId: id } } }));
        cacheSession(s);
        return s;
      } catch (e) {
        const cached = cachedSession(id);
        if (cached) return cached;
        throw e;
      }
    },
    initialData: () => cachedSession(id),
    staleTime: 0,
  });
}

type StartBody = Omit<Schemas['CreatePracticeSessionRequest'], 'ai_fill'> & { ai_fill?: boolean };

/** 开始一组练习并进入答题页。 */
export function useStartPractice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: StartBody) => unwrap(api.POST('/practice-sessions', { body: { ...body, ai_fill: body.ai_fill ?? false } })),
    onSuccess: (s) => {
      track('session_start', { kind: s.kind, questions: s.questions.length });
      cacheSession(s);
      qc.setQueryData(practiceKeys.session(s.id), s);
      void qc.invalidateQueries({ queryKey: ['practice', 'home'] });
      router.push({ pathname: '/practice/[id]', params: { id: String(s.id) } });
    },
    onError: (e) => toast(e instanceof Error && e.message ? e.message : '没能开始练习，请重试'),
  });
}

/**
 * 只练指定的几道题（题目详情「再练这道题」、知识点卡片「来一题检验」）。
 * 不知道是哪门课时（从批改结果、总结页进来的知识点卡片）先读第一道题拿到课程。
 */
export function usePracticeQuestions() {
  const start = useStartPractice();
  const [resolving, setResolving] = useState(false);
  const run = async (questionIds: number[], subjectId?: number) => {
    if (questionIds.length === 0 || start.isPending || resolving) return;
    let sid = subjectId;
    if (!sid) {
      setResolving(true);
      try {
        sid = (await unwrap(api.GET('/questions/{questionId}', { params: { path: { questionId: questionIds[0]! } } }))).subject_id;
      } catch {
        sid = undefined;
      } finally {
        setResolving(false);
      }
    }
    if (!sid) {
      toast('没能开始练习，请重试');
      return;
    }
    start.mutate({ subject_id: sid, kind: 'custom', config: { question_ids: questionIds, count: questionIds.length, only_unmastered: false, ai_fill: false } });
  };
  return { run, pending: start.isPending || resolving };
}

/**
 * 今日训练完成后的「再练一组」：直接开一组 10 道自定义练习并进入答题页，优先没掌握的知识点；
 * 都掌握了（没有符合条件的题）就从全部题里抽。不计入今日训练的进度。
 */
export function useExtraRound(replace = false) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (subjectId: number) => {
      const create = (only_unmastered: boolean) =>
        unwrap(api.POST('/practice-sessions', { body: { subject_id: subjectId, kind: 'custom', config: { count: 10, only_unmastered, ai_fill: false }, ai_fill: false } }));
      try {
        return await create(true);
      } catch {
        return create(false);
      }
    },
    onSuccess: (s) => {
      track('session_start', { kind: s.kind, questions: s.questions.length });
      cacheSession(s);
      qc.setQueryData(practiceKeys.session(s.id), s);
      void qc.invalidateQueries({ queryKey: ['practice', 'home'] });
      (replace ? router.replace : router.push)({ pathname: '/practice/[id]', params: { id: String(s.id) } });
    },
    onError: (e) => toast(e instanceof Error && e.message ? e.message : '没能开始练习，请重试'),
  });
}

export function useWrongBook(subjectId: number) {
  return useQuery({
    queryKey: practiceKeys.wrong(subjectId),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/wrong-book', { params: { path: { subjectId } } })),
  });
}

export const groupTagNames: Record<Schemas['PlanGroupKey'], string> = { new: '新知识点', review: '到期复习', weak: '薄弱查漏', recite: '背诵' };
export const lossNames = { knowledge: '知识没掌握', norm: '答题不规范', time: '时间不够' } as const;
export const addedReasonNames = { wrong: '答错', partial: '没拿满分', revealed: '看了答案' } as const;
