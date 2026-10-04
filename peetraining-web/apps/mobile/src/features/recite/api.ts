import type { Schemas } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { toast } from '@/components';
import { api, unwrap } from '@/lib/api';

export type ReciteSession = Schemas['ReciteSession'];
export type ReciteItem = Schemas['ReciteItem'];
export type ReciteMode = Schemas['ReciteMode'];
export type ReciteLevel = Schemas['ReciteResultLevel'];

export const reciteKeys = {
  session: (id: number) => ['recite', id] as const,
  summary: (id: number) => ['recite', id, 'summary'] as const,
};

export function useReciteSession(id: number) {
  return useQuery({ queryKey: reciteKeys.session(id), queryFn: () => unwrap(api.GET('/recite-sessions/{sessionId}', { params: { path: { sessionId: id } } })) });
}

/** 开始一轮背诵（或再背没记住的）并进入 4.14。 */
export function useStartRecite(replace = false) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { subject_id: number; retry_of?: number }) => unwrap(api.POST('/recite-sessions', { body })),
    onSuccess: (s) => {
      qc.setQueryData(reciteKeys.session(s.id), s);
      void qc.invalidateQueries({ queryKey: ['practice', 'home'] });
      const to = { pathname: '/recite/[id]' as const, params: { id: String(s.id) } };
      if (replace) router.replace(to);
      else router.push(to);
    },
    onError: (e) => toast(e instanceof Error ? e.message : '没能开始背诵，请重试'),
  });
}

export const levelNames: Record<ReciteLevel, string> = { forgot: '没记住', vague: '模糊', remembered: '记住了' };
export const modeNames: Record<ReciteMode, string> = { cloze: '挖空', dictation: '默写', oral: '口述' };
