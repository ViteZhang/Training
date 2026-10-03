import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { toast } from '@/components';
import { todayKeys } from '@/features/today/api';
import { api, unwrap } from '@/lib/api';

export const dashboardKeys = { subject: (subjectId: number) => ['dashboard', subjectId] as const };

export function useDashboard(subjectId: number) {
  return useQuery({
    queryKey: dashboardKeys.subject(subjectId),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/dashboard', { params: { path: { subjectId } } })),
    enabled: subjectId > 0,
  });
}

/** 「以为会了」一键加入今日训练（6.2）。 */
export function useAddFalseMastery(subjectId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST('/subjects/{subjectId}/false-mastery/plan', { params: { path: { subjectId } } })),
    onSuccess: (d) => {
      toast(d.added > 0 ? `已加入今日训练 ${d.added} 道题` : '今天的计划里已经有这些知识点的题了');
      void qc.invalidateQueries({ queryKey: todayKeys.home });
    },
    onError: (e) => toast(e instanceof Error ? e.message : '没能加入，请重试'),
  });
}
