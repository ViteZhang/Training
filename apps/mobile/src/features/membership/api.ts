import type { Schemas } from '@training/api-client';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from '@/lib/api';

export const memberKeys = {
  center: ['membership', 'center'] as const,
  order: (no: string) => ['orders', no] as const,
};

export function useMemberCenter() {
  return useQuery({ queryKey: memberKeys.center, queryFn: () => unwrap(api.GET('/membership/plans')) });
}

/** 6.6 轮询订单：待支付时每 2 秒查一次，最多查 polls 次（回调通常几秒内到达）。 */
export function useOrder(orderNo: string, polls = 6) {
  return useQuery({
    queryKey: memberKeys.order(orderNo),
    queryFn: () => unwrap(api.GET('/orders/{orderNo}', { params: { path: { orderNo } } })),
    enabled: !!orderNo,
    refetchInterval: (q) => (q.state.data?.status === 'created' && q.state.dataUpdateCount < polls ? 2000 : false),
  });
}

export function yuan(cents: number) {
  return cents % 100 === 0 ? `¥${cents / 100}` : `¥${(cents / 100).toFixed(1).replace(/\.0$/, '')}`;
}

export const channelNames: Record<Schemas['PayChannel'], string> = {
  wechat: '微信支付',
  alipay: '支付宝',
  apple_iap: 'App Store',
};

/** 权益对比表的一格：不限 / 3 次/天 / 100 页。 */
export function ruleText(type: Schemas['QuotaType'], r: Schemas['QuotaRule']) {
  if (r.limit === null || r.limit === undefined) return '不限';
  const unit = type === 'parse_pages' ? '页' : type === 'essay_grading' ? '篇' : type === 'paper_grading' ? '套' : type === 'grading' ? '次' : '题';
  const per = { total: '', monthly: '/月', daily: '/天', weekly: '/周' }[r.period];
  return `${r.limit} ${unit}${per}`;
}
