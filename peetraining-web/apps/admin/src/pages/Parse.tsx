// 7.5 资料解析监控：成功率（目标 ≥ 95%）、解析页数、平均与 P95 耗时；按资料格式的成功率与页数；失败与部分成功的任务列表；
// 任务详情只显示格式、页数、识别日志与失败环节，不显示内容；操作：重跑、给用户发拍照建议、补偿解析额度。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Card, Col, Drawer, Form, InputNumber, Row, Space, Statistic, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { api, dt, dur, pct, unwrap } from '../lib/api';

const formatNames: Record<string, string> = { pdf: 'PDF', docx: 'Word', xlsx: 'Excel', image: '拍照 / 图片', text: '粘贴文字' };
const stepNames: Record<string, string> = {
  queued: '排队', extract: '取文字', moderate: '内容安全', split: '拆题', structure: '结构化', match: '配答案', rubric: '采分点', tag: '打知识点', dedupe: '去重', done: '完成',
};
const statusColor: Record<string, string> = { failed: 'red', partial: 'orange', done: 'green' };

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

export function Parse() {
  const [open, setOpen] = useState<number>();
  const stats = useQuery({ queryKey: ['admin', 'parse', 'stats'], queryFn: () => unwrap(api.GET('/admin/parse/stats')) });
  const jobs = useQuery({ queryKey: ['admin', 'parse', 'jobs'], queryFn: () => unwrap(api.GET('/admin/parse/jobs')) });
  const s = stats.data;
  return (
    <>
      <PageTitle code="7.5" title="资料解析监控" extra={<Typography.Text type="secondary">近 7 天</Typography.Text>} />
      <Loadable loading={stats.isPending} error={stats.error}>
        {s ? (
          <>
            <Row gutter={16}>
              <Col span={6}>
                <Card><Statistic title="成功率（目标 ≥ 95%）" value={pct(s.success_rate)} valueStyle={{ color: s.success_rate < 0.95 && s.files ? '#D6453D' : undefined }} /></Card>
              </Col>
              <Col span={6}><Card><Statistic title="解析页数" value={s.pages} suffix={` / ${s.files} 个文件`} /></Card></Col>
              <Col span={6}><Card><Statistic title="平均耗时" value={dur(s.avg_seconds)} /></Card></Col>
              <Col span={6}><Card><Statistic title="P95 耗时" value={dur(s.p95_seconds)} /></Card></Col>
            </Row>
            <Card title="按资料格式" style={{ marginTop: 16 }}>
              <Table
                size="small"
                pagination={false}
                rowKey="format"
                dataSource={s.formats}
                columns={[
                  { title: '格式', render: (_, f) => formatNames[f.format] ?? f.format },
                  { title: '文件', dataIndex: 'files' },
                  { title: '页数', dataIndex: 'pages' },
                  { title: '成功 / 部分 / 失败', render: (_, f) => `${f.ok} / ${f.partial} / ${f.failed}` },
                  { title: '成功率', render: (_, f) => pct(f.rate) },
                ]}
              />
            </Card>
          </>
        ) : null}
      </Loadable>
      <Card title="失败与部分成功的任务" style={{ marginTop: 16 }}>
        <Loadable loading={jobs.isPending} error={jobs.error}>
          <Table
            rowKey="id"
            dataSource={jobs.data?.items ?? []}
            onRow={(j) => ({ onClick: () => setOpen(j.id), style: { cursor: 'pointer' } })}
            columns={[
              { title: '任务', dataIndex: 'id' },
              { title: '用户 ID', dataIndex: 'user_id' },
              { title: '文件', render: (_, j) => `${j.files} 个 · 失败 ${j.failed_files} · 部分 ${j.partial_files}` },
              { title: '计费页数', dataIndex: 'billed_pages' },
              { title: '创建', render: (_, j) => dt(j.created_at) },
            ]}
          />
        </Loadable>
      </Card>
      {open ? <JobDrawer id={open} onClose={() => setOpen(undefined)} /> : null}
      <PrivacyNote />
    </>
  );
}

function JobDrawer({ id, onClose }: { id: number; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const path = { params: { path: { jobId: id } } };
  const q = useQuery({ queryKey: ['admin', 'parse', 'job', id], queryFn: () => unwrap(api.GET('/admin/parse/jobs/{jobId}', path)) });
  const done = (msg: string) => () => {
    message.success(msg);
    void qc.invalidateQueries({ queryKey: ['admin', 'parse'] });
  };
  const rerun = useMutation({ mutationFn: () => unwrap(api.POST('/admin/parse/jobs/{jobId}/rerun', path)), onSuccess: (r) => done(`已重跑 ${r.count} 个文件`)(), onError: (e) => message.error(errMsg(e)) });
  const tips = useMutation({ mutationFn: () => unwrap(api.POST('/admin/parse/jobs/{jobId}/tips', path)), onSuccess: done('已发送拍照建议'), onError: (e) => message.error(errMsg(e)) });
  const comp = useMutation({
    mutationFn: (pages: number) =>
      unwrap(api.POST('/admin/users/{userId}/parse-pages', { params: { path: { userId: q.data!.user_id } }, body: { pages, idempotency_key: `job-${id}-${crypto.randomUUID()}` } })),
    onSuccess: done('已补偿解析额度'),
    onError: (e) => message.error(errMsg(e)),
  });
  const j = q.data;
  return (
    <Drawer open width={720} title={`任务 ${id}`} onClose={onClose}>
      <Loadable loading={q.isPending} error={q.error}>
        {j ? (
          <>
            <Typography.Paragraph>
              用户 {j.user_id} · 创建 {dt(j.created_at)} · 结束 {dt(j.finished_at)} · 预占 {j.reserved_pages} 页 · 计费 {j.billed_pages} 页
            </Typography.Paragraph>
            {j.prompt_versions ? (
              <Typography.Paragraph type="secondary">
                提示词版本：{Object.entries(j.prompt_versions).map(([k, v]) => `${k}@${v}`).join('、')}
              </Typography.Paragraph>
            ) : null}
            <Table
              size="small"
              pagination={false}
              rowKey="material_id"
              dataSource={j.file_logs}
              columns={[
                { title: '文件', dataIndex: 'material_id' },
                { title: '格式', render: (_, f) => formatNames[f.format] ?? f.format },
                { title: '页数', dataIndex: 'pages' },
                { title: '状态', render: (_, f) => <Tag color={statusColor[f.status]}>{f.status}</Tag> },
                { title: '失败环节', render: (_, f) => (f.status === 'done' ? '—' : stepNames[f.step] ?? f.step) },
                { title: '重试', dataIndex: 'attempts' },
                { title: '日志', render: (_, f) => [f.fail_reason, f.failed_pages?.length ? `失败页 ${f.failed_pages.join('、')}` : ''].filter(Boolean).join('；') || '—' },
              ]}
            />
            <Space style={{ marginTop: 16 }} wrap>
              <Button type="primary" loading={rerun.isPending} onClick={() => rerun.mutate()}>重跑失败的文件</Button>
              <Button loading={tips.isPending} onClick={() => tips.mutate()}>给用户发拍照建议</Button>
            </Space>
            <Form layout="inline" style={{ marginTop: 16 }} onFinish={(v: { pages: number }) => comp.mutate(v.pages)}>
              <Form.Item label="补偿解析额度" name="pages" rules={[{ required: true, message: '页数' }]}>
                <InputNumber min={1} max={2000} />
              </Form.Item>
              <Button htmlType="submit" loading={comp.isPending}>补偿</Button>
            </Form>
          </>
        ) : null}
      </Loadable>
    </Drawer>
  );
}
