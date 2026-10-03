// 7.12 内容生产：AI 从授权资料拆知识点（与现有知识点树对齐：新增 / 补充来源 / 表述差异），录题与采分点（可让 AI 从参考答案提），
// 修订、下线已有条目；编辑只写草稿，提交后进审核（新编辑前 3 次全量审核，之后 30% 抽检）。
import { ApiError } from '@training/api-client';
import type { components } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Drawer, Empty, Form, Input, InputNumber, Popconfirm, Segmented, Select, Space, Table, Tabs, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { ProjectPicker } from '../components/ProjectPicker';
import { api, dt, unwrap } from '../lib/api';

type Candidate = components['schemas']['OfficialCandidate'];
type Draft = components['schemas']['OfficialDraft'];
type Point = { content: string; score: number; keywords?: string[] };
type EntityType = 'knowledge_point' | 'question';
type ChangeType = 'add' | 'revise' | 'offline';

export const qtypeNames: Record<string, string> = {
  single_choice: '单选',
  multi_choice: '多选',
  true_false: '判断',
  fill_blank: '填空',
  term: '名词解释',
  short_answer: '简答',
  essay: '论述',
  material_analysis: '材料分析',
  composition: '作文',
};
const subjective = ['term', 'short_answer', 'essay', 'material_analysis', 'composition'];
const alignNames: Record<string, { text: string; color: string }> = {
  new: { text: '新增', color: 'green' },
  supplement: { text: '补充来源', color: 'blue' },
  differ: { text: '表述差异', color: 'orange' },
};
export const draftStatusNames: Record<string, string> = { draft: '草稿', submitted: '待审核', approved: '已通过', rejected: '已退回', published: '已发布' };
export const changeNames: Record<string, string> = { add: '新增', revise: '修订', offline: '下线' };

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

/** 编辑中的草稿（新建时 id 为空）。 */
export interface Editing {
  id?: number;
  entity_type: EntityType;
  change_type: ChangeType;
  entity_id?: number;
  payload: Record<string, unknown>;
}

export function Produce() {
  const [project, setProject] = useState<number>();
  const [editing, setEditing] = useState<Editing>();
  return (
    <>
      <PageTitle code="7.12" title="内容生产" extra={<ProjectPicker value={project} onChange={setProject} />} />
      {project ? (
        <Tabs
          items={[
            { key: 'kp', label: 'AI 拆知识点', children: <KPExtract project={project} onEdit={setEditing} /> },
            { key: 'items', label: '官方题库', children: <Items project={project} onEdit={setEditing} /> },
            { key: 'drafts', label: '我的草稿', children: <Drafts project={project} onEdit={setEditing} /> },
          ]}
          tabBarExtraContent={
            <Space>
              <Button onClick={() => setEditing({ entity_type: 'knowledge_point', change_type: 'add', payload: { rubric: [] } })}>新建知识点</Button>
              <Button type="primary" onClick={() => setEditing({ entity_type: 'question', change_type: 'add', payload: { qtype: 'term', rubric: [], kp_names: [] } })}>
                录题
              </Button>
            </Space>
          }
        />
      ) : (
        <Empty description="先选择项目（内容编辑只能看到分配给自己的项目）" />
      )}
      {project && editing ? <DraftEditor project={project} editing={editing} onClose={() => setEditing(undefined)} /> : null}
    </>
  );
}

function KPExtract({ project, onEdit }: { project: number; onEdit: (e: Editing) => void }) {
  const { message } = AntApp.useApp();
  const [text, setText] = useState('');
  const extract = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/official/projects/{projectId}/kp-candidates', { params: { path: { projectId: project } }, body: { text } })),
    onError: (e) => message.error(errMsg(e)),
  });
  const toEdit = (c: Candidate): Editing => ({
    entity_type: 'knowledge_point',
    change_type: c.alignment === 'differ' && c.existing_id ? 'revise' : 'add',
    entity_id: c.alignment === 'differ' ? c.existing_id : undefined,
    payload: { section: c.section, chapter: c.chapter, name: c.name, original_text: c.original_text, rubric: c.rubric },
  });
  return (
    <>
      <Typography.Paragraph type="secondary">粘贴授权资料的原文（须已登记在项目的授权资料清单里），AI 拆出知识点候选并与现有知识点树对齐。</Typography.Paragraph>
      <Input.TextArea rows={8} value={text} onChange={(e) => setText(e.target.value)} placeholder="资料原文" maxLength={20000} showCount />
      <Button type="primary" style={{ marginTop: 12 }} loading={extract.isPending} disabled={text.trim().length < 10} onClick={() => extract.mutate()}>
        拆知识点
      </Button>
      {extract.data ? (
        <Table
          style={{ marginTop: 16 }}
          rowKey={(c) => `${c.section}/${c.chapter}/${c.name}`}
          dataSource={extract.data.items}
          pagination={false}
          columns={[
            { title: '对齐', render: (_, c) => { const a = alignNames[c.alignment] ?? { text: c.alignment, color: 'default' }; return <Tag color={a.color}>{a.text}</Tag>; } },
            { title: '板块 / 章节', render: (_, c) => `${c.section} / ${c.chapter}` },
            { title: '知识点', dataIndex: 'name' },
            {
              title: '表述',
              render: (_, c) => (
                <>
                  <div>{c.original_text}</div>
                  {c.alignment === 'differ' ? <Typography.Text type="secondary">现有：{c.existing_text}</Typography.Text> : null}
                </>
              ),
            },
            { title: '采分点', render: (_, c) => c.rubric.length },
            {
              title: '操作',
              render: (_, c) =>
                c.alignment === 'supplement' ? (
                  <Typography.Text type="secondary">已有，无需新增</Typography.Text>
                ) : c.alignment === 'differ' ? (
                  <Space direction="vertical" size={4}>
                    <Button size="small" onClick={() => onEdit(toEdit(c))}>以新表述为准（修订）</Button>
                    <Typography.Text type="secondary">或保留现有表述，不处理</Typography.Text>
                  </Space>
                ) : (
                  <Button size="small" type="primary" onClick={() => onEdit(toEdit(c))}>
                    存为草稿
                  </Button>
                ),
            },
          ]}
        />
      ) : null}
    </>
  );
}

function Items({ project, onEdit }: { project: number; onEdit: (e: Editing) => void }) {
  const q = useQuery({
    queryKey: ['admin', 'official', 'items', project],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/items', { params: { path: { projectId: project } } })),
  });
  return (
    <Loadable loading={q.isPending} error={q.error}>
      <Table
        rowKey={(i) => `${i.entity_type}:${i.id}`}
        dataSource={q.data?.items ?? []}
        locale={{ emptyText: '官方题库还是空的' }}
        columns={[
          { title: '类型', render: (_, i) => (i.entity_type === 'knowledge_point' ? '知识点' : qtypeNames[String(i.payload.qtype)] ?? '题目') },
          { title: '内容', dataIndex: 'title' },
          { title: '状态', render: (_, i) => (i.status === 'offline' ? <Tag>已下线</Tag> : '—') },
          {
            title: '操作',
            render: (_, i) => (
              <Space>
                <Button size="small" onClick={() => onEdit({ entity_type: i.entity_type as EntityType, change_type: 'revise', entity_id: i.id, payload: i.payload })}>
                  修订
                </Button>
                {i.entity_type === 'question' && i.status !== 'offline' ? (
                  <Button size="small" danger onClick={() => onEdit({ entity_type: 'question', change_type: 'offline', entity_id: i.id, payload: i.payload })}>
                    下线
                  </Button>
                ) : null}
              </Space>
            ),
          },
        ]}
      />
    </Loadable>
  );
}

function Drafts({ project, onEdit }: { project: number; onEdit: (e: Editing) => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [status, setStatus] = useState<'' | 'draft' | 'rejected' | 'submitted'>('');
  const path = { params: { path: { projectId: project } } };
  const q = useQuery({
    queryKey: ['admin', 'official', 'drafts', project, status],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/drafts', { params: { path: { projectId: project }, query: { status: status || undefined, mine: true } } })),
  });
  const done = () => void qc.invalidateQueries({ queryKey: ['admin', 'official'] });
  const submit = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/official/projects/{projectId}/drafts/{draftId}/submit', { params: { path: { projectId: project, draftId: id } } })),
    onSuccess: (r) => {
      message.success(r.mode === 'skipped' ? '本次未抽中审核，已直接通过' : '已提交审核');
      done();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const del = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/admin/official/projects/{projectId}/drafts/{draftId}', { params: { path: { projectId: project, draftId: id } } })),
    onSuccess: done,
    onError: (e) => message.error(errMsg(e)),
  });
  void path;
  return (
    <>
      <Segmented
        style={{ marginBottom: 16 }}
        value={status}
        onChange={(v) => setStatus(v as typeof status)}
        options={[
          { label: '全部', value: '' },
          { label: '草稿', value: 'draft' },
          { label: '已退回', value: 'rejected' },
          { label: '待审核', value: 'submitted' },
        ]}
      />
      <Loadable loading={q.isPending} error={q.error}>
        <Table
          rowKey="id"
          dataSource={q.data?.items ?? []}
          columns={[
            { title: '变更', render: (_, d: Draft) => `${changeNames[d.change_type]}${d.entity_type === 'knowledge_point' ? '知识点' : '题目'}` },
            { title: '内容', render: (_, d: Draft) => String(d.payload.name ?? d.payload.stem ?? '') },
            {
              title: '状态',
              render: (_, d: Draft) => (
                <>
                  {draftStatusNames[d.status]}
                  {d.reject_reason ? <div><Typography.Text type="danger">退回原因：{d.reject_reason}</Typography.Text></div> : null}
                </>
              ),
            },
            { title: '更新', render: (_, d: Draft) => dt(d.submitted_at ?? d.created_at) },
            {
              title: '操作',
              render: (_, d: Draft) =>
                d.status === 'draft' || d.status === 'rejected' ? (
                  <Space>
                    <Button size="small" onClick={() => onEdit({ id: d.id, entity_type: d.entity_type as EntityType, change_type: d.change_type as ChangeType, entity_id: d.entity_id, payload: d.payload })}>
                      修改
                    </Button>
                    <Button size="small" type="primary" loading={submit.isPending} onClick={() => submit.mutate(d.id)}>
                      提交审核
                    </Button>
                    <Popconfirm title="删除这条草稿？" onConfirm={() => del.mutate(d.id)}>
                      <Button size="small" danger>删除</Button>
                    </Popconfirm>
                  </Space>
                ) : null,
            },
          ]}
        />
      </Loadable>
    </>
  );
}

/** 选项按行写「A. 内容」。 */
function parseOptions(text: string) {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const m = /^([A-Z])[.、．\s]\s*(.*)$/.exec(l);
      return m ? { key: m[1], text: m[2] } : { key: '', text: l };
    });
}

function DraftEditor({ project, editing, onClose }: { project: number; editing: Editing; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [form] = Form.useForm();
  const isKP = editing.entity_type === 'knowledge_point';
  const p = editing.payload;
  const items = useQuery({
    queryKey: ['admin', 'official', 'items', project],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/items', { params: { path: { projectId: project } } })),
    enabled: !isKP,
  });
  const kpNames = (items.data?.items ?? []).filter((i) => i.entity_type === 'knowledge_point').map((i) => i.title);
  const save = useMutation({
    mutationFn: (payload: Record<string, unknown>) => {
      const body = { entity_type: editing.entity_type, change_type: editing.change_type, entity_id: editing.entity_id, payload };
      return editing.id
        ? unwrap(api.PUT('/admin/official/projects/{projectId}/drafts/{draftId}', { params: { path: { projectId: project, draftId: editing.id } }, body }))
        : unwrap(api.POST('/admin/official/projects/{projectId}/drafts', { params: { path: { projectId: project } }, body }));
    },
    onSuccess: () => {
      message.success('已存为草稿');
      void qc.invalidateQueries({ queryKey: ['admin', 'official'] });
      onClose();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const suggest = useMutation({
    mutationFn: (v: { qtype: string; stem: string; answer: string; score?: number }) =>
      unwrap(api.POST('/admin/official/rubric-candidates', { body: { ...v, qtype: v.qtype as components['schemas']['QuestionType'] } })),
    onSuccess: (r) => form.setFieldValue('rubric', r.items),
    onError: (e) => message.error(errMsg(e)),
  });
  const onFinish = (v: Record<string, unknown>) => {
    if (editing.change_type === 'offline') return save.mutate(p);
    const rubric = ((v.rubric as Point[] | undefined) ?? []).filter((r) => r?.content);
    if (isKP) return save.mutate({ ...v, rubric });
    const { options_text, ...rest } = v as { options_text?: string };
    return save.mutate({ ...rest, rubric, options: options_text ? parseOptions(options_text) : undefined, kp_names: v.kp_names ?? [] });
  };
  const options = Array.isArray(p.options) ? (p.options as { key: string; text: string }[]).map((o) => `${o.key}. ${o.text}`).join('\n') : '';
  const title = `${changeNames[editing.change_type]}${isKP ? '知识点' : '题目'}${editing.id ? '（修改草稿）' : ''}`;
  return (
    <Drawer open width={720} title={title} onClose={onClose} extra={<Button type="primary" loading={save.isPending} onClick={() => form.submit()}>存为草稿</Button>}>
      {editing.change_type === 'offline' ? (
        <Alert type="warning" showIcon message="下线后用户题库里的这道题会归档，不再出题；用户自己改过的保留" description={String(p.stem ?? '')} />
      ) : null}
      <Form form={form} layout="vertical" initialValues={{ ...p, options_text: options }} onFinish={onFinish} disabled={editing.change_type === 'offline'}>
        {isKP ? (
          <>
            <Space>
              <Form.Item name="section" label="板块" rules={[{ required: true }]}>
                <Input style={{ width: 200 }} />
              </Form.Item>
              <Form.Item name="chapter" label="章节" rules={[{ required: true }]}>
                <Input style={{ width: 200 }} />
              </Form.Item>
            </Space>
            <Form.Item name="name" label="知识点" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="original_text" label="原文表述（须出自授权资料）">
              <Input.TextArea rows={4} />
            </Form.Item>
          </>
        ) : (
          <>
            <Space>
              <Form.Item name="qtype" label="题型" rules={[{ required: true }]}>
                <Select style={{ width: 140 }} options={Object.entries(qtypeNames).map(([value, label]) => ({ value, label }))} />
              </Form.Item>
              <Form.Item name="score" label="分值">
                <InputNumber min={0} max={150} />
              </Form.Item>
              <Form.Item name="exam_year" label="真题年份">
                <InputNumber min={2000} max={2100} />
              </Form.Item>
            </Space>
            <Form.Item name="stem" label="题干" rules={[{ required: true }]}>
              <Input.TextArea rows={3} />
            </Form.Item>
            <Form.Item noStyle shouldUpdate={(a, b) => a.qtype !== b.qtype}>
              {({ getFieldValue }) =>
                subjective.includes(getFieldValue('qtype')) ? null : (
                  <Form.Item name="options_text" label="选项（每行一个，如「A. 内容」）">
                    <Input.TextArea rows={4} />
                  </Form.Item>
                )
              }
            </Form.Item>
            <Form.Item name="answer" label="参考答案">
              <Input.TextArea rows={4} />
            </Form.Item>
            <Form.Item name="analysis" label="解析">
              <Input.TextArea rows={2} />
            </Form.Item>
            <Form.Item name="kp_names" label="关联知识点（第一个为主知识点）">
              <Select mode="multiple" options={kpNames.map((n) => ({ value: n, label: n }))} />
            </Form.Item>
          </>
        )}
        <Card
          size="small"
          title="采分点"
          extra={
            isKP ? null : (
              <Button
                size="small"
                loading={suggest.isPending}
                onClick={() => {
                  const v = form.getFieldsValue() as { qtype: string; stem: string; answer: string; score?: number };
                  if (!v.stem || !v.answer) return message.warning('先填题干和参考答案');
                  suggest.mutate(v);
                }}
              >
                AI 从参考答案提采分点
              </Button>
            )
          }
        >
          <Form.List name="rubric">
            {(fields, { add, remove }) => (
              <>
                {fields.map((f) => (
                  <Space key={f.key} align="start">
                    <Form.Item name={[f.name, 'content']} rules={[{ required: true, message: '填采分点' }]}>
                      <Input placeholder="采分点" style={{ width: 420 }} />
                    </Form.Item>
                    <Form.Item name={[f.name, 'score']} rules={[{ required: true, message: '分值' }]}>
                      <InputNumber min={0} placeholder="分值" />
                    </Form.Item>
                    <Button onClick={() => remove(f.name)}>删除</Button>
                  </Space>
                ))}
                <Button onClick={() => add({ score: 2 })}>添加采分点</Button>
              </>
            )}
          </Form.List>
        </Card>
      </Form>
    </Drawer>
  );
}
