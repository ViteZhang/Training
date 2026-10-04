import type { Schemas } from '@training/api-client';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from '@/lib/api';

export const mineKeys = {
  me: ['me'] as const,
  profile: ['profile'] as const,
  subjects: ['subjects'] as const,
  overview: ['me', 'overview'] as const,
  survey: ['me', 'survey'] as const,
  feedbacks: ['feedbacks'] as const,
  exportPreview: (subjectId: number) => ['export', 'preview', subjectId] as const,
  export: (id: number) => ['export', id] as const,
};

export function useMe() {
  return useQuery({ queryKey: mineKeys.me, queryFn: () => unwrap(api.GET('/me')) });
}

export function useProfile() {
  return useQuery({ queryKey: mineKeys.profile, queryFn: () => unwrap(api.GET('/profile')) });
}

export function useSubjects() {
  return useQuery({ queryKey: mineKeys.subjects, queryFn: () => unwrap(api.GET('/subjects')) });
}

export function useOverview() {
  return useQuery({ queryKey: mineKeys.overview, queryFn: () => unwrap(api.GET('/me/overview')) });
}

export const tierNames: Record<NonNullable<Schemas['MembershipStatus']['tier']>, string> = {
  sprint: '冲刺卡',
  season: '考季卡',
  monthly: '月卡',
  gift: '赠送会员',
};

export const feedbackTypes: { key: Schemas['FeedbackType']; label: string }[] = [
  { key: 'suggestion', label: '功能建议' },
  { key: 'recognition', label: '识别不准' },
  { key: 'grading', label: '批改不准' },
  { key: 'bug', label: '出现问题' },
  { key: 'infringement', label: '侵权投诉' },
];

export function ymd(iso: string) {
  const d = new Date(iso);
  return `${d.getFullYear()} 年 ${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}
