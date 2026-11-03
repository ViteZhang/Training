// 7.18 通用体验题（PRD v3 增补，设计稿 n_admin_sample）：统计卡（学科数、方向数、体验题数含待审核、本周被体验次数）；
// 左侧学科与方向树，可新增；右侧题目列表（题目、参考教材、题型、分值、被体验次数、状态）；「AI 批量生成初稿」按方向生成、进待审核；
// 「新建题目」手工录入；审核通过后才在 App 1.10 显示；「AI 出题缓存」列表可抽检、下线未覆盖学科的现场生成题。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Card, Col, Form, Input, InputNumber, Menu, Modal, Row, Select, Space, Statistic, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { api, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';

type Sample = Schemas['AdminSampleQuestion'];
type Input = Schemas['SampleQuestionInput'];

export const qtypeNames: Record<string, string> = { term: '名词解释', short_answer: '简答', discussion: '论述' };
const statusTag: Record<Sample['status'], { text: string; color?: string }> = {
  pending: { text: '待审核', color: 'gold' },
  online: { text: '已上线', color: 'green' },
  offline: { text: '已下线' },
};
const LIVE = 'live';

/** 采分点合计（录题时要等于分值）。 */
export const rubricSum = (r: { score?: number }[] = []) => r.reduce((s, p) => s + (p.score ?? 0), 0);

export function Samples({ roles }: { roles: Role[] }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const canReview = roles.includes('admin') || roles.includes('content_lead');
  const [dir, setDir] = useState<string>();
  const [editing, setEditing] = useState<Sample | 'new'>();
  const [generating, setGenerating] = useState(false);
  const [adding, setAdding] = useState(false);
  const overview = useQuery({ queryKey: ['admin', 'samples'], queryFn: () => unwrap(api.GET('/admin/samples')) });
  const dirs = overview.data?.items ?? [];
  const current = dir ?? (dirs[0] ? String(dirs[0].id) : undefined);
  const directionId = current && current !== LIVE ? Number(current) : undefined;
  const questions = useQuery({
    queryKey: ['admin', 'samples', 'questions', current],
    queryFn: () => unwrap(api.GET('/admin/samples/questions', { params: { query: { direction_id: directionId } } })),
    enabled: !!current,
  });
  const refresh = () => void qc.invalidateQueries({ queryKey: ['admin', 'samples'] });
  const fail = (e: unknown) => message.error(e instanceof ApiError ? e.message : '操作失败');
  const status = useMutation({
    mutationFn: (v: { id: number; status: 'online' | 'offline' }) =>
      unwrap(api.POST('/admin/samples/questions/{sampleId}/status', { params: { path: { sampleId: v.id } }, body: { status: v.status } })),
    onSuccess: refresh,
    onError: fail,
  });
  const save = useMutation({
    mutationFn: async (v: Input) => {
      if (editing && editing !== 'new') {
        await unwrap(api.PUT('/admin/samples/questions/{sampleId}', { params: { path: { sampleId: editing.id } }, body: v }));
      } else {
        await unwrap(api.POST('/admin/samples/questions', { body: { ...v, direction_id: directionId! } }));
      }
    },
    onSuccess: () => {
      message.success(editing === 'new' ? '已新建，进入待审核' : '已保存');
      setEditing(undefined);
      refresh();
    },
    onError: fail,
  });
  const generate = useMutation({
    mutationFn: (v: { qtype: Input['qtype']; score: number; count: number; textbook?: string }) =>
      unwrap(api.POST('/admin/samples/generate', { body: { ...v, direction_id: directionId! } })),
    onSuccess: (r) => {
      message.success(`已生成 ${r.created} 道初稿，进入待审核`);
      setGenerating(false);
      refresh();
    },
    onError: fail,
  });
  const addDir = useMutation({
    mutationFn: (v: { discipline: string; name: string }) => unwrap(api.POST('/admin/samples/directions', { body: v })),
    onSuccess: () => {
      setAdding(false);
      refresh();
    },
    onError: fail,
  });

  const o = overview.data;
  const disciplines = [...new Set(dirs.map((d) => d.discipline))];
  const dirName = dirs.find((d) => String(d.id) === current);
  return (
    <>
      <PageTitle code="7.18" title="通用体验题" />
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}><Card><Statistic title="学科" value={o?.disciplines ?? 0} /></Card></Col>
        <Col span={6}><Card><Statistic title="方向" value={o?.directions ?? 0} /></Card></Col>
        <Col span={6}><Card><Statistic title="体验题" value={o?.questions ?? 0} suffix={<Typography.Text type="secondary" style={{ fontSize: 12 }}>待审核 {o?.pending ?? 0}</Typography.Text>} /></Card></Col>
        <Col span={6}><Card><Statistic title="本周被体验次数" value={o?.week_tries ?? 0} /></Card></Col>
      </Row>
      <Loadable loading={overview.isPending} error={overview.error}>
        <Row gutter={16}>
          <Col span={6}>
            <Card title="学科与方向" size="small" extra={canReview ? <Button size="small" onClick={() => setAdding(true)}>+ 新增</Button> : null}>
              <Menu
                mode="inline"
                selectedKeys={current ? [current] : []}
                onClick={(e) => setDir(e.key)}
                items={[
                  ...disciplines.map((d) => ({
                    key: `g-${d}`,
                    type: 'group' as const,
                    label: d,
                    children: dirs.filter((x) => x.discipline === d).map((x) => ({ key: String(x.id), label: `${x.name}（${x.question_count}）` })),
                  })),
                  { key: LIVE, label: 'AI 出题缓存' },
                ]}
              />
            </Card>
          </Col>
          <Col span={18}>
            <Card
              size="small"
              title={current === LIVE ? 'AI 出题缓存（未覆盖学科的现场生成题）' : dirName ? `${dirName.discipline} · ${dirName.name}` : ''}
              extra={
                directionId ? (
                  <Space>
                    <Button onClick={() => setGenerating(true)}>AI 批量生成初稿</Button>
                    <Button type="primary" onClick={() => setEditing('new')}>+ 新建题目</Button>
                  </Space>
                ) : null
              }
            >
              <Loadable loading={questions.isPending && !!current} error={questions.error}>
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={questions.data?.items ?? []}
                  pagination={{ pageSize: 30 }}
                  expandable={{
                    expandedRowRender: (q) => (
                      <Space direction="vertical">
                        <Typography.Text>参考答案：{q.answer}</Typography.Text>
                        {q.rubric.map((p, i) => <Typography.Text key={i} type="secondary">{i + 1}. {p.content}（{p.score} 分）</Typography.Text>)}
                      </Space>
                    ),
                  }}
                  columns={[
                    { title: '题目', dataIndex: 'stem', render: (s: string, q) => <Space>{s}{q.origin !== 'manual' ? <Tag>AI 出题</Tag> : null}</Space> },
                    current === LIVE ? { title: '专业课', dataIndex: 'subject_label' } : { title: '参考教材', dataIndex: 'textbook' },
                    { title: '题型', render: (_, q) => qtypeNames[q.qtype] },
                    { title: '分值', dataIndex: 'score' },
                    { title: '被体验次数', dataIndex: 'try_count' },
                    { title: '状态', render: (_, q) => <Tag color={statusTag[q.status].color}>{statusTag[q.status].text}</Tag> },
                    {
                      title: '操作',
                      render: (_, q) => (
                        <Space>
                          {current !== LIVE ? <Button size="small" onClick={() => setEditing(q)}>编辑</Button> : null}
                          {canReview && q.status !== 'online' ? <Button size="small" type="primary" onClick={() => status.mutate({ id: q.id, status: 'online' })}>审核通过</Button> : null}
                          {canReview && q.status === 'online' ? <Button size="small" danger onClick={() => status.mutate({ id: q.id, status: 'offline' })}>下线</Button> : null}
                        </Space>
                      ),
                    },
                  ]}
                />
              </Loadable>
              <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
                已覆盖学科：AI 批量生成、团队审核后上线。未覆盖学科的用户，AI 按其专业课现场生成一道，标「AI 出题」，同一门课的结果缓存复用，在「AI 出题缓存」中抽检。
              </Typography.Paragraph>
            </Card>
          </Col>
        </Row>
      </Loadable>
      <SampleEditor sample={editing} loading={save.isPending} onCancel={() => setEditing(undefined)} onSave={(v) => save.mutate(v)} />
      <GenerateModal open={generating} loading={generate.isPending} onCancel={() => setGenerating(false)} onOk={(v) => generate.mutate(v)} />
      <Modal open={adding} title="新增学科或方向" onCancel={() => setAdding(false)} footer={null} destroyOnClose>
        <Form layout="vertical" onFinish={(v) => addDir.mutate(v)}>
          <Form.Item name="discipline" label="学科" rules={[{ required: true }]}><Input placeholder="如 哲学" maxLength={16} /></Form.Item>
          <Form.Item name="name" label="方向" rules={[{ required: true }]}><Input placeholder="如 西方哲学" maxLength={32} /></Form.Item>
          <Button type="primary" htmlType="submit" loading={addDir.isPending}>新增</Button>
        </Form>
      </Modal>
    </>
  );
}

function SampleEditor({ sample, loading, onCancel, onSave }: { sample?: Sample | 'new'; loading: boolean; onCancel: () => void; onSave: (v: Input) => void }) {
  const [form] = Form.useForm<Input>();
  const score = Form.useWatch('score', form);
  const rubric = Form.useWatch('rubric', form) as { score?: number }[] | undefined;
  const sum = rubricSum(rubric);
  const init = sample && sample !== 'new' ? sample : { qtype: 'term', score: 5, rubric: [{ content: '', score: undefined }] };
  return (
    <Modal open={!!sample} width={720} title={sample === 'new' ? '新建题目' : '编辑题目'} onCancel={onCancel} onOk={() => form.submit()} confirmLoading={loading} destroyOnClose>
      <Form form={form} layout="vertical" preserve={false} initialValues={init} onFinish={onSave}>
        <Space>
          <Form.Item name="qtype" label="题型" rules={[{ required: true }]}>
            <Select style={{ width: 140 }} options={Object.entries(qtypeNames).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
          <Form.Item name="score" label="分值" rules={[{ required: true }]}><InputNumber min={1} max={300} /></Form.Item>
          <Form.Item name="textbook" label="参考教材"><Input style={{ width: 300 }} maxLength={128} /></Form.Item>
        </Space>
        <Form.Item name="stem" label="题干" rules={[{ required: true }]}><Input.TextArea rows={2} maxLength={2000} /></Form.Item>
        <Form.Item name="answer" label="参考答案（用团队自己的话写）" rules={[{ required: true }]}><Input.TextArea rows={4} maxLength={6000} /></Form.Item>
        <Typography.Text type={score && Math.abs(sum - score) > 0.01 ? 'danger' : 'secondary'}>采分点合计 {sum} 分{score ? `，分值 ${score} 分` : ''}</Typography.Text>
        <Form.List name="rubric">
          {(fields, { add, remove }) => (
            <>
              {fields.map((f) => (
                <Space key={f.key} style={{ display: 'flex', marginTop: 8 }}>
                  <Form.Item name={[f.name, 'content']} rules={[{ required: true, message: '要点' }]} noStyle><Input style={{ width: 460 }} placeholder="要点" /></Form.Item>
                  <Form.Item name={[f.name, 'score']} rules={[{ required: true, message: '分值' }]} noStyle><InputNumber min={0.5} step={0.5} placeholder="分值" /></Form.Item>
                  <Button onClick={() => remove(f.name)} disabled={fields.length <= 1}>删除</Button>
                </Space>
              ))}
              <Button style={{ marginTop: 8 }} onClick={() => add({ content: '' })}>+ 采分点</Button>
            </>
          )}
        </Form.List>
      </Form>
    </Modal>
  );
}

function GenerateModal({ open, loading, onCancel, onOk }: { open: boolean; loading: boolean; onCancel: () => void; onOk: (v: { qtype: Input['qtype']; score: number; count: number; textbook?: string }) => void }) {
  const [form] = Form.useForm();
  return (
    <Modal open={open} title="AI 批量生成初稿" onCancel={onCancel} onOk={() => form.submit()} confirmLoading={loading} destroyOnClose>
      <Form form={form} layout="vertical" preserve={false} initialValues={{ qtype: 'term', score: 5, count: 3 }} onFinish={onOk}>
        <Form.Item name="qtype" label="题型"><Select options={Object.entries(qtypeNames).map(([value, label]) => ({ value, label }))} /></Form.Item>
        <Form.Item name="score" label="分值"><InputNumber min={1} max={300} /></Form.Item>
        <Form.Item name="count" label="生成几道（最多 10）"><InputNumber min={1} max={10} /></Form.Item>
        <Form.Item name="textbook" label="参考教材（选填）"><Input maxLength={128} /></Form.Item>
        <Typography.Text type="secondary">生成的题目进入待审核，审核通过后才在 App 1.10 显示。</Typography.Text>
      </Form>
    </Modal>
  );
}
