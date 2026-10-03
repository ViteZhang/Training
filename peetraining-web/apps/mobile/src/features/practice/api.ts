import type { Schemas } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { toast } from '@/components';
import { api, unwrap } from '@/lib/api';
import { cachedSession, cacheSession } from './offline';

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
      cacheSession(s);
      qc.setQueryData(practiceKeys.session(s.id), s);
      void qc.invalidateQueries({ queryKey: ['practice', 'home'] });
      router.push({ pathname: '/practice/[id]', params: { id: String(s.id) } });
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
