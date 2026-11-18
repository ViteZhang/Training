// 使用进度：7.2 用户详情与 7.20 激活漏斗共用。阶梯按服务端顺序（注册 → 完成引导 → 有题库 → 首次作答 → 看到批改 → 隔天再练 → 7 日后仍在练 → 付费），
// 只展示服务端算好的达成时间，前端不推算。
import type { Schemas } from '@/api-client';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { Space, Statistic, Steps, Tag, Typography } from 'antd';
import { semantic } from '@/ui-tokens';
import { api, dt, unwrap } from '../lib/api';
import { Loadable } from './Page';

type Stage = Schemas['ActivationStage'];

/** 阶段名（与服务端 label 一致，列表里只拿到键时用）。 */
export const stageNames: Record<Stage, string> = {
  registered: '注册',
  onboarded: '完成引导',
  has_bank: '有题库',
  first_answer: '首次作答',
  first_grading: '看到批改',
  return_day: '隔天再练',
  week_later: '7 日后仍在练',
  paid: '付费',
};

/** 小时数 → 「3.5 小时」「2.1 天」。 */
export function hoursText(h?: number) {
  if (h === undefined || h === null) return '—';
  if (h < 1) return `${Math.max(1, Math.round(h * 60))} 分钟`;
  if (h < 48) return `${Math.round(h * 10) / 10} 小时`;
  return `${Math.round((h / 24) * 10) / 10} 天`;
}

/** 距今多少天（按 24 小时算，向下取整）。 */
export function daysAgo(iso?: string) {
  if (!iso) return undefined;
  return Math.floor((Date.now() - new Date(iso).getTime()) / 86_400_000);
}

/** 当前阶段标签：停在越前面的阶段越需要关注。 */
export function StageTag({ stage }: { stage: Stage }) {
  const early = stage === 'registered' || stage === 'onboarded' || stage === 'has_bank';
  return <Tag color={early ? 'orange' : stage === 'paid' ? 'gold' : 'green'}>{stageNames[stage]}</Tag>;
}

/** 用户详情里的「使用进度」：阶梯、最近 30 天作答。 */
export function UserJourneyCard({ userId }: { userId: number }) {
  const q = useQuery({
    queryKey: ['admin', 'user', userId, 'journey'],
    queryFn: () => unwrap(api.GET('/admin/users/{userId}/journey', { params: { path: { userId } } })),
  });
  const j = q.data;
  const current = j ? j.milestones.findIndex((m) => m.key === j.current) : 0;
  const idle = daysAgo(j?.last_active_at);
  return (
    <Loadable loading={q.isPending} error={q.error}>
      {j ? (
        <>
          <Space size="large" style={{ marginBottom: 12 }} wrap>
            <Statistic title="目前走到" valueRender={() => <StageTag stage={j.current} />} />
            <Statistic title="最近 30 天作答" value={j.answers_30} suffix="次" />
            <Statistic title="最近 30 天有作答" value={j.active_days_30} suffix="天" />
            <Statistic title="最后活跃" value={idle === undefined ? '—' : idle === 0 ? '今天' : `${idle} 天前`} />
          </Space>
          <Steps
            direction="vertical"
            size="small"
            current={current}
            items={j.milestones.map((m) => ({
              title: m.label,
              status: m.reached ? 'finish' : 'wait',
              description: m.reached ? (m.at ? dt(m.at) : '已完成（无时间记录）') : '未达成',
            }))}
          />
          <DailyBars days={j.days} />
        </>
      ) : null}
    </Loadable>
  );
}

/** 最近 30 天每天的作答次数（北京时间），没作答的日子补 0。 */
function DailyBars({ days }: { days: Schemas['AdminDayAnswers'][] }) {
  const byDay = new Map(days.map((d) => [d.day, d.answers]));
  const list: { day: string; n: number }[] = [];
  const [now] = useState(() => Date.now());
  const bjToday = new Date(now + 8 * 3_600_000);
  for (let i = 29; i >= 0; i--) {
    const d = new Date(bjToday.getTime() - i * 86_400_000).toISOString().slice(0, 10);
    list.push({ day: d, n: byDay.get(d) ?? 0 });
  }
  const max = Math.max(1, ...list.map((d) => d.n));
  return (
    <>
      <Typography.Text type="secondary">最近 30 天每天作答次数</Typography.Text>
      <div style={{ display: 'flex', alignItems: 'flex-end', gap: 2, height: 64, marginTop: 8 }} aria-label="最近 30 天每天作答次数">
        {list.map((d) => (
          <div
            key={d.day}
            title={`${d.day}：${d.n} 次`}
            style={{ flex: 1, height: d.n ? Math.max(4, (d.n / max) * 60) : 2, background: d.n ? semantic.progress : semantic.progressTrack, borderRadius: 2 }}
          />
        ))}
      </div>
      <Space style={{ width: '100%', justifyContent: 'space-between' }}>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>{list[0]?.day.slice(5)}</Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>今天</Typography.Text>
      </Space>
    </>
  );
}
