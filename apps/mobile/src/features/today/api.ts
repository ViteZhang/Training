import type { Schemas } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, unwrap } from '@/lib/api';

export type Home = Schemas['Home'];
export type TodayPlan = Schemas['TodayPlan'];
export type PlanGroupKey = Schemas['PlanGroupKey'];

export const todayKeys = {
  home: ['home'] as const,
  summary: ['home', 'summary'] as const,
};

/** 首页聚合（2.1）：状态、计划、主推、题库、以为会了都由服务端算好。题库整理中时轮询进度。 */
export function useHome() {
  return useQuery({
    queryKey: todayKeys.home,
    queryFn: () => unwrap(api.GET('/home')),
    refetchInterval: (q) => (q.state.data?.state === 'organizing' ? 5000 : false),
  });
}

export function useTodaySummary() {
  return useQuery({ queryKey: todayKeys.summary, queryFn: () => unwrap(api.GET('/plans/today/summary')) });
}

/** 2.1e：接受立即切换阶段并重排今天的计划；拒绝留在当前阶段。 */
export function useAnswerStagePrompt() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (accept: boolean) => unwrap(api.POST('/home/stage-prompt', { body: { accept } })),
    onSuccess: (home) => {
      qc.setQueryData(todayKeys.home, home);
      void qc.invalidateQueries({ queryKey: ['profile'] });
    },
  });
}

/** 2.1f：改目标分（PATCH /subjects/{id}），null 表示不设。 */
export function useSetTarget() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ subjectId, target }: { subjectId: number; target: number | null }) =>
      unwrap(api.PATCH('/subjects/{subjectId}', { params: { path: { subjectId } }, body: { target_score: target } })),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['subjects'] });
      void qc.invalidateQueries({ queryKey: todayKeys.home });
    },
  });
}

/** 计划分组名：强化期以后「薄弱查漏」显示为题型专项（PRD 模块 2）。 */
export const groupNames: Record<PlanGroupKey, string> = { new: '新知识点', review: '到期复习', weak: '薄弱查漏', recite: '背诵' };

/** 2.1e 构成对比的顺序与名称（设计稿：新知识点 · 复习 · 查漏 · 背诵）。 */
export const mixOrder: { key: PlanGroupKey; name: string }[] = [
  { key: 'new', name: '新知识点' },
  { key: 'review', name: '复习' },
  { key: 'weak', name: '查漏' },
  { key: 'recite', name: '背诵' },
];

export const stageDesc: Record<Schemas['Stage'], string> = {
  foundation: '系统学习新知识点，边学边练。',
  strengthen: '题型专项和查漏为主，每周专练一种题型，练到采分点写全。',
  sprint: '接下来不再安排新知识点，查漏和复习的比重提高。建议每周按考试时间做一套整卷。',
  final: '以背诵和模拟考试为主，保持手感；最后 3 天不再做新卷。',
};

export const lossNames = { knowledge: '知识没掌握', norm: '答题不规范', time: '时间不够' } as const;

export function minutes(v: number) {
  return Math.round(v);
}
