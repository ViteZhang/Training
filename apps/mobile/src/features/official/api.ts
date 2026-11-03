// 官方题库先行（PRD v3 增补）：院校专业课库、体验次数、申请收录、补充资料提示的请求与流程跳转。
import type { Schemas } from '@training/api-client';
import { useQuery } from '@tanstack/react-query';
import { router, type Href } from 'expo-router';
import { setStep } from '@/features/onboarding/api';
import { saveDraft } from '@/features/onboarding/draft';
import { api, unwrap } from '@/lib/api';

export type CatalogCourse = Schemas['CatalogCourse'];
export type Trial = Schemas['Trial'];
export type SampleQuestion = Schemas['SampleQuestion'];

export const officialKeys = {
  schools: (q: string) => ['catalog', 'schools', q] as const,
  courses: (school: string, year?: number) => ['catalog', 'courses', school, year] as const,
  trialStatus: ['trial-status'] as const,
  trial: (id: number) => ['trial', id] as const,
  samples: ['sample-questions'] as const,
  supplement: (subjectId: number) => ['supplement-prompt', subjectId] as const,
};

export function useCatalogSchools(q: string) {
  const term = q.trim();
  return useQuery({
    queryKey: officialKeys.schools(term),
    queryFn: () => unwrap(api.GET('/catalog/schools', { params: { query: { q: term } } })),
    enabled: term.length > 0,
    staleTime: 60_000,
  });
}

export function useCatalogCourses(school: string | undefined, year?: number) {
  return useQuery({
    queryKey: officialKeys.courses(school ?? '', year),
    queryFn: () => unwrap(api.GET('/catalog/courses', { params: { query: { school: school ?? '', exam_year: year } } })),
    enabled: !!school,
  });
}

export function useTrialStatus(enabled = true) {
  return useQuery({ queryKey: officialKeys.trialStatus, queryFn: () => unwrap(api.GET('/trials/status')), enabled });
}

export const qtypeLabel: Record<string, string> = { term: '名词解释', short_answer: '简答', discussion: '论述' };

/** 题型 · 分值，如「简答 · 15 分」。 */
export const qtypeScore = (qtype: string, score: number) => `${qtypeLabel[qtype] ?? qtype} · ${score} 分`;

/**
 * 首次批改完成后：1.2 目标分、1.3 备考安排移到这之后，可跳过（PRD v3 增补「引导分流」），走完再去 after。
 * after 为空时 1.3 之后进今日。
 */
export async function afterFirstGrade(after: Href = '/(tabs)/today') {
  saveDraft({ after: String(after) });
  await setStep('1.2');
  router.replace('/(onboarding)/target');
}
