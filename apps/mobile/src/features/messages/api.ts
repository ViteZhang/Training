import type { Schemas } from '@training/api-client';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import type { Href } from 'expo-router';
import { api, unwrap } from '@/lib/api';

export const messageKeys = {
  list: ['messages'] as const,
  unread: ['messages', 'unread'] as const,
};

export function useUnreadCount() {
  return useQuery({
    queryKey: messageKeys.unread,
    queryFn: () => unwrap(api.GET('/messages/unread-count')),
    refetchInterval: 60_000,
  });
}

export function useMessages() {
  return useInfiniteQuery({
    queryKey: messageKeys.list,
    queryFn: ({ pageParam }) => unwrap(api.GET('/messages', { params: { query: pageParam ? { cursor: pageParam } : {} } })),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
}

const str = (v: unknown) => (v === undefined || v === null ? '' : String(v));

/** 消息的跳转目标（2.3 点击跳转）。服务端给 page 与 params，这里映射到 App 的路由；不认识的 page 不跳转。 */
export function messageHref(m: Schemas['Message']): Href | undefined {
  const p = m.params ?? {};
  switch (m.page) {
    case 'import_review':
      return { pathname: '/import/confirm/[id]', params: { id: str(p.job_id) } };
    case 'import':
      return '/import';
    case 'today':
      return '/(tabs)/today';
    case 'paper_report':
      return { pathname: '/paper/report/[id]', params: { id: str(p.session_id) } };
    case 'essay_result':
      return { pathname: '/essay/result/[id]', params: { id: str(p.essay_id) } };
    case 'export':
      return '/mine/export';
    case 'agreement':
      return { pathname: '/(auth)/agreement', params: { kind: str(p.kind) || 'user' } };
    case 'feedback':
      return '/mine/feedback';
    case 'library':
      return '/library';
    case 'member':
      return '/member';
    case 'official_banks':
      return '/bank/official';
    case 'bank':
      return '/(tabs)/bank';
    default:
      return undefined;
  }
}

/** 列表里的时间：今天显示时刻，昨天显示「昨天」，更早显示日期。 */
export function messageTime(iso: string, now = new Date()) {
  const d = new Date(iso);
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(now) - day(d)) / 86_400_000);
  if (diff === 0) return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  if (diff === 1) return '昨天';
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

export function isToday(iso: string, now = new Date()) {
  const d = new Date(iso);
  return d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate();
}
