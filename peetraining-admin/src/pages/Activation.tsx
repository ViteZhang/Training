// 7.20 激活漏斗：注册用户「下载之后有没有用、用到了哪一步」。按注册日期（北京时间）选一批用户，看使用阶梯每一级的达成人数、
// 从注册到达成的用时中位数、最远停在每一级的人数；点「停在这里」列出这些用户，点用户打开 7.2 用户详情（含使用进度）。
// 第一期全部由服务端按业务表实时计算（不依赖埋点）；数据分析只看汇总，客服与管理员能看到具体用户。
import type { Schemas } from '@/api-client';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { Button, Card, Col, DatePicker, Empty, Progress, Row, Space, Statistic, Table, Tooltip, Typography } from 'antd';
import dayjs, { type Dayjs } from 'dayjs';
import { useState } from 'react';
import { daysAgo, hoursText, stageNames } from '../components/Journey';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { api, dt, pct, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';
import { UserDrawer } from './Users';

type Stage = Schemas['ActivationStage'];

const last30: [Dayjs, Dayjs] = [dayjs().subtract(29, 'day'), dayjs()];
const presets: { label: string; value: [Dayjs, Dayjs] }[] = [
  { label: '近 7 天', value: [dayjs().subtract(6, 'day'), dayjs()] },
  { label: '近 30 天', value: last30 },
  { label: '近 90 天', value: [dayjs().subtract(89, 'day'), dayjs()] },
  { label: '近一年', value: [dayjs().subtract(364, 'day'), dayjs()] },
];

export function Activation({ roles }: { roles: Role[] }) {
  const canSeeUsers = roles.includes('admin') || roles.includes('support');
  const [range, setRange] = useState<[Dayjs, Dayjs]>(last30);
  const [stage, setStage] = useState<Stage>();
  const [open, setOpen] = useState<number>();
  const from = range[0].format('YYYY-MM-DD');
  const to = range[1].format('YYYY-MM-DD');
  const q = useQuery({
    queryKey: ['admin', 'activation', from, to],
    queryFn: () => unwrap(api.GET('/admin/activation', { params: { query: { from, to } } })),
  });
  const a = q.data;
  const total = a?.total ?? 0;
  return (
    <>
      <PageTitle
        code="7.20"
        title="激活漏斗"
        extra={
          <Space>
            <Typography.Text type="secondary">注册日期</Typography.Text>
            <DatePicker.RangePicker
              allowClear={false}
              value={range}
              presets={presets}
              disabledDate={(d) => d.isAfter(dayjs(), 'day')}
              onChange={(v) => {
                if (v?.[0] && v[1]) {
                  setRange([v[0], v[1]]);
                  setStage(undefined);
                }
              }}
            />
          </Space>
        }
      />
      <Typography.Paragraph type="secondary">
        按注册日期选一批用户，看他们用到了哪一步。实时按业务数据计算（资料、题库、作答、批改、订单），历史用户也算在内；「停在这里」是最远只走到这一级的人。
      </Typography.Paragraph>
      <Loadable loading={q.isPending} error={q.error}>
        {a ? (
          <>
            <Row gutter={16} style={{ marginBottom: 16 }}>
              <Col span={6}>
                <Card><Statistic title={`${a.from} 至 ${a.to} 注册`} value={total} suffix="人" /></Card>
              </Col>
              {(['first_answer', 'return_day', 'week_later'] as Stage[]).map((k) => {
                const s = a.steps.find((x) => x.key === k);
                return (
                  <Col span={6} key={k}>
                    <Card>
                      <Statistic title={stageNames[k]} value={total ? pct(s!.reached / total) : '—'} suffix={<Typography.Text type="secondary"> {s?.reached ?? 0} 人</Typography.Text>} />
                    </Card>
                  </Col>
                );
              })}
            </Row>
            {total === 0 ? (
              <Empty description="这段时间没有新注册的用户" />
            ) : (
              <Table
                rowKey="key"
                pagination={false}
                dataSource={a.steps}
                rowClassName={(r) => (r.key === stage ? 'ant-table-row-selected' : '')}
                columns={[
                  {
                    title: '阶段',
                    render: (_, r) => (
                      <Tooltip title={r.hint}>
                        <Typography.Text strong>{r.label}</Typography.Text>
                      </Tooltip>
                    ),
                  },
                  {
                    title: '达成人数（占注册）',
                    width: 280,
                    render: (_, r) => (
                      <Space style={{ width: '100%' }} direction="vertical" size={0}>
                        <Typography.Text>{r.reached} 人</Typography.Text>
                        <Progress percent={Math.round((r.reached / total) * 1000) / 10} size="small" />
                      </Space>
                    ),
                  },
                  {
                    title: '较上一级',
                    render: (_, r, i) => {
                      const prev = a.steps[i - 1];
                      return i === 0 || !prev?.reached ? '—' : pct(r.reached / prev.reached);
                    },
                  },
                  { title: '注册后多久达成（中位数）', render: (_, r) => (r.key === 'registered' ? '—' : hoursText(r.median_hours)) },
                  {
                    title: '停在这里',
                    render: (_, r) =>
                      r.stuck === 0 ? (
                        '0 人'
                      ) : canSeeUsers ? (
                        <Button type="link" style={{ padding: 0 }} onClick={() => setStage(r.key)}>
                          {r.stuck} 人 · {pct(r.stuck / total)}
                        </Button>
                      ) : (
                        `${r.stuck} 人 · ${pct(r.stuck / total)}`
                      ),
                  },
                ]}
              />
            )}
          </>
        ) : null}
      </Loadable>
      {stage && canSeeUsers ? <StuckUsers from={from} to={to} stage={stage} onOpen={setOpen} /> : null}
      {open ? <UserDrawer id={open} roles={roles} onClose={() => setOpen(undefined)} /> : null}
      <PrivacyNote />
    </>
  );
}

function StuckUsers({ from, to, stage, onOpen }: { from: string; to: string; stage: Stage; onOpen: (id: number) => void }) {
  const q = useInfiniteQuery({
    queryKey: ['admin', 'activation', 'users', from, to, stage],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => unwrap(api.GET('/admin/activation/users', { params: { query: { from, to, stage, cursor: pageParam } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
  const rows = q.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <Card title={`停在「${stageNames[stage]}」的用户`} style={{ marginTop: 16 }} extra={<Typography.Text type="secondary">点一行看使用进度，可在详情里发站内消息</Typography.Text>}>
      <Loadable loading={q.isPending} error={q.error}>
        <Table
          rowKey="user_id"
          size="small"
          pagination={false}
          dataSource={rows}
          onRow={(r) => ({ onClick: () => onOpen(r.user_id), style: { cursor: 'pointer' } })}
          columns={[
            { title: '用户 ID', dataIndex: 'user_id' },
            { title: '手机号', dataIndex: 'phone_masked' },
            { title: '注册时间', render: (_, r) => dt(r.created_at) },
            { title: '注册天数', render: (_, r) => `${daysAgo(r.created_at) ?? 0} 天` },
            { title: '最后活跃', render: (_, r) => dt(r.last_active_at) },
            { title: '最后作答', render: (_, r) => dt(r.last_answer_at) },
          ]}
        />
        {q.hasNextPage ? (
          <Button style={{ marginTop: 12 }} loading={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
            加载更多
          </Button>
        ) : null}
      </Loadable>
    </Card>
  );
}
