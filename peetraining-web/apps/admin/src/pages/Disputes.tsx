// 7.6 批改异议：异议率（异议次数 / 主观题批改次数）、本周异议数、重批后分数变化比例、平均处理时长；异议原因分布；
// 人工抽检队列（重批自动完成，人工只抽检）；授权可见的题目详情（采分点、重批前后分数）；归因进入每周质量复盘。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Col, Descriptions, Modal, Row, Segmented, Select, Space, Statistic, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { api, dt, dur, pct, unwrap } from '../lib/api';

const reasonNames: Record<string, string> = { hit_missed: '答到了没给分', rubric_wrong: '采分点不对', score_unfair: '分数不合理', other: '其他' };
const statusNames: Record<string, string> = { rechecking: '重批中', rechecked: '已重批', sampled: '已抽检', manual_changed: '人工改判', closed: '已关闭' };
const attributionNames: Record<string, string> = { rubric_incomplete: '采分点识别不全', model_error: '批改模型误判', answer_insufficient: '用户答案确实不足' };
type Status = '' | 'rechecked' | 'sampled';

export function Disputes() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [status, setStatus] = useState<Status>('rechecked');
  const [view, setView] = useState<number>();
  const stats = useQuery({ queryKey: ['admin', 'disputes', 'stats'], queryFn: () => unwrap(api.GET('/admin/disputes/stats')) });
  const list = useQuery({
    queryKey: ['admin', 'disputes', status],
    queryFn: () => unwrap(api.GET('/admin/disputes', { params: { query: { status: status || undefined } } })),
  });
  const attribute = useMutation({
    mutationFn: ({ id, a }: { id: number; a: 'rubric_incomplete' | 'model_error' | 'answer_insufficient' }) =>
      unwrap(api.POST('/admin/disputes/{disputeId}/attribution', { params: { path: { disputeId: id } }, body: { attribution: a } })),
    onSuccess: () => {
      message.success('已记录归因');
      void qc.invalidateQueries({ queryKey: ['admin', 'disputes'] });
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  const s = stats.data;
  return (
    <>
      <PageTitle code="7.6" title="批改异议" extra={<Typography.Text type="secondary">本周</Typography.Text>} />
      {s ? (
        <>
          <Row gutter={16}>
            <Col span={6}><Card><Statistic title="异议率" value={pct(s.rate)} suffix={` · ${s.disputes} / ${s.gradings}`} /></Card></Col>
            <Col span={6}><Card><Statistic title="本周异议" value={s.disputes} /></Card></Col>
            <Col span={6}><Card><Statistic title="重批后分数变化" value={pct(s.changed_ratio)} /></Card></Col>
            <Col span={6}><Card><Statistic title="平均处理时长" value={dur(s.avg_seconds)} /></Card></Col>
          </Row>
          <Typography.Paragraph style={{ marginTop: 12 }}>
            原因分布：{Object.entries(s.reasons).map(([k, v]) => `${reasonNames[k] ?? k} ${v}`).join('；') || '暂无'}
          </Typography.Paragraph>
        </>
      ) : null}
      <Segmented
        style={{ margin: '12px 0' }}
        value={status}
        onChange={(v) => setStatus(v as Status)}
        options={[
          { label: '待抽检', value: 'rechecked' },
          { label: '已抽检', value: 'sampled' },
          { label: '全部', value: '' },
        ]}
      />
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey="id"
          dataSource={list.data?.items ?? []}
          columns={[
            { title: '异议', dataIndex: 'id' },
            { title: '用户 ID', dataIndex: 'user_id' },
            { title: '原因', render: (_, d) => reasonNames[d.reason] ?? d.reason },
            { title: '重批前 → 后', render: (_, d) => `${d.score_before ?? '—'} → ${d.score_after ?? '—'}` },
            { title: '状态', render: (_, d) => statusNames[d.status] ?? d.status },
            { title: '授权', render: (_, d) => (d.granted ? <Tag color="blue">已授权</Tag> : '未授权') },
            { title: '时间', render: (_, d) => dt(d.created_at) },
            {
              title: '归因',
              render: (_, d) => (
                <Space>
                  {d.granted ? <Button size="small" onClick={() => setView(d.id)}>查看题目</Button> : null}
                  <Select
                    size="small"
                    style={{ width: 150 }}
                    placeholder="选择归因"
                    value={d.attribution}
                    onChange={(a) => attribute.mutate({ id: d.id, a: a as 'rubric_incomplete' })}
                    options={Object.entries(attributionNames).map(([value, label]) => ({ value, label }))}
                  />
                </Space>
              ),
            },
          ]}
        />
      </Loadable>
      {view ? <DisputeContent id={view} onClose={() => setView(undefined)} /> : null}
      <PrivacyNote />
    </>
  );
}

function DisputeContent({ id, onClose }: { id: number; onClose: () => void }) {
  const q = useQuery({
    queryKey: ['admin', 'dispute-content', id],
    queryFn: () => unwrap(api.GET('/admin/disputes/{disputeId}/content', { params: { path: { disputeId: id } } })),
    retry: false,
    staleTime: Infinity,
  });
  const g = q.data;
  return (
    <Modal open width={760} title="授权查看 · 题目与作答" footer={null} onCancel={onClose}>
      <Alert type="info" showIcon message="这次查看已记入日志，并已通知用户" style={{ marginBottom: 12 }} />
      <Loadable loading={q.isPending} error={q.error}>
        {g ? (
          <Descriptions column={1} bordered size="small">
            <Descriptions.Item label="授权截止">{dt(g.grant_expires_at)}</Descriptions.Item>
            <Descriptions.Item label="题目">{g.stem}</Descriptions.Item>
            <Descriptions.Item label="作答">{g.answer || '（拍照作答）'}</Descriptions.Item>
            <Descriptions.Item label="重批前">{g.score ?? '—'} / {g.full_score ?? '—'}</Descriptions.Item>
            <Descriptions.Item label="重批后">{g.regrade?.score ?? '—'}</Descriptions.Item>
            <Descriptions.Item label="采分点（含来源与用户补充）">
              <pre style={{ whiteSpace: 'pre-wrap', margin: 0 }}>{JSON.stringify(g.regrade?.rubric ?? g.rubric, null, 2)}</pre>
            </Descriptions.Item>
          </Descriptions>
        ) : null}
      </Loadable>
    </Modal>
  );
}
