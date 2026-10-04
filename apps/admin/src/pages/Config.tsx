// 7.8 AI 与额度：每活跃用户每天 AI 成本与预算、本月 AI 成本及占收入比例；AI 任务列表（当前提示词版本、单次成本、成本占比）；
// 灰度（按比例放量、对比各版本表现、扩大比例 / 全量 / 回滚）；免费额度与会员权益、会员价格；
// 系统配置：协议正文与版本、各年份初试日期、功能开关（支持指定用户）、App 最新与最低版本、规则参数。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Card, Col, DatePicker, Form, Input, InputNumber, Modal, Popconfirm, Row, Select, Space, Statistic, Switch, Table, Tabs, Tag, Typography } from 'antd';
import dayjs from 'dayjs';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { ParamEditor } from '../components/ParamEditor';
import { api, dt, pct, unwrap } from '../lib/api';

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

const paramGroups = { quota: ['quota'], pricing: ['pricing', 'growth', 'ai_budget'] };
const kindNames: Record<string, string> = { user: '用户协议', privacy: '隐私政策', membership: '会员服务协议' };

export function Config() {
  return (
    <>
      <PageTitle code="7.8" title="AI 与额度" />
      <Tabs
        items={[
          { key: 'ai', label: 'AI 成本与灰度', children: <AIPanel /> },
          { key: 'quota', label: '免费额度与会员权益', children: <ParamsPanel keys={paramGroups.quota} /> },
          { key: 'pricing', label: '价格与奖励', children: <ParamsPanel keys={paramGroups.pricing} /> },
          { key: 'agreements', label: '协议', children: <AgreementsPanel /> },
          { key: 'exam', label: '初试日期', children: <ExamDatesPanel /> },
          { key: 'flags', label: '功能开关', children: <FlagsPanel /> },
          { key: 'versions', label: 'App 版本', children: <VersionsPanel /> },
          { key: 'rules', label: '规则参数', children: <ParamsPanel exclude={[...paramGroups.quota, ...paramGroups.pricing, 'system_messages']} /> },
        ]}
      />
    </>
  );
}

function AIPanel() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const q = useQuery({ queryKey: ['admin', 'ai'], queryFn: () => unwrap(api.GET('/admin/ai')) });
  const [edit, setEdit] = useState<Schemas['AdminAITask']>();
  const save = useMutation({
    mutationFn: ({ cap, body }: { cap: string; body: { stable_model: string; stable_prompt: string; candidate_model?: string; candidate_prompt?: string; candidate_percent: number } }) =>
      unwrap(api.PUT('/admin/ai/rollouts/{capability}', { params: { path: { capability: cap } }, body })),
    onSuccess: () => {
      message.success('灰度已更新');
      setEdit(undefined);
      void qc.invalidateQueries({ queryKey: ['admin', 'ai'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const o = q.data;
  return (
    <Loadable loading={q.isPending} error={q.error}>
      {o ? (
        <>
          <Row gutter={16}>
            <Col span={8}>
              <Card>
                <Statistic
                  title="每活跃用户每天 AI 成本（近 7 天）"
                  value={`¥${o.cost_per_active_user_day.toFixed(2)}`}
                  suffix={<Typography.Text type="secondary"> 预算 ¥{o.budget_per_user_day.toFixed(2)}</Typography.Text>}
                  valueStyle={{ color: o.cost_per_active_user_day > o.budget_per_user_day ? '#D6453D' : undefined }}
                />
              </Card>
            </Col>
            <Col span={8}><Card><Statistic title="本月 AI 成本" value={`¥${o.month_cost_yuan.toFixed(2)}`} /></Card></Col>
            <Col span={8}>
              <Card>
                <Statistic title="占本月收入" value={o.month_revenue_yuan ? pct(o.revenue_ratio) : '—'} suffix={<Typography.Text type="secondary"> 上限 {pct(o.ratio_max)}</Typography.Text>} />
              </Card>
            </Col>
          </Row>
          <Table
            style={{ marginTop: 16 }}
            rowKey="capability"
            dataSource={o.tasks}
            expandable={{
              expandedRowRender: (t) => (
                <Table
                  size="small"
                  pagination={false}
                  rowKey={(v) => `${v.model}@${v.prompt}`}
                  dataSource={t.versions}
                  columns={[
                    { title: '模型 @ 提示词', render: (_, v) => `${v.model} @ ${v.prompt}` },
                    { title: '调用', dataIndex: 'calls' },
                    { title: '成功率', render: (_, v) => pct(v.success_rate) },
                    { title: '单次成本', render: (_, v) => `¥${v.avg_cost_yuan.toFixed(4)}` },
                    { title: '平均耗时', render: (_, v) => `${v.avg_latency_ms} ms` },
                    { title: '异议率', render: (_, v) => (v.dispute_rate === undefined ? '—' : pct(v.dispute_rate)) },
                  ]}
                />
              ),
            }}
            columns={[
              { title: 'AI 任务', dataIndex: 'capability' },
              { title: '稳定版', render: (_, t) => `${t.stable_model || '默认模型'} @ ${t.stable_prompt}` },
              { title: '灰度', render: (_, t) => (t.candidate_prompt ? <Tag color="blue">{`${t.candidate_model} @ ${t.candidate_prompt} · ${t.candidate_percent}%`}</Tag> : '—') },
              { title: '近 7 天调用', dataIndex: 'calls' },
              { title: '单次成本', render: (_, t) => (t.calls ? `¥${(t.cost_yuan / t.calls).toFixed(4)}` : '—') },
              { title: '成本占比', render: (_, t) => pct(t.cost_share) },
              { title: '操作', render: (_, t) => <Button size="small" onClick={() => setEdit(t)}>灰度设置</Button> },
            ]}
          />
          {edit ? (
            <Modal open title={`灰度 · ${edit.capability}`} footer={null} onCancel={() => setEdit(undefined)} destroyOnHidden>
              <Form
                layout="vertical"
                initialValues={{
                  stable_model: edit.stable_model,
                  stable_prompt: edit.stable_prompt,
                  candidate_model: edit.candidate_model,
                  candidate_prompt: edit.candidate_prompt,
                  candidate_percent: edit.candidate_percent,
                }}
                onFinish={(v: { stable_model: string; stable_prompt: string; candidate_model?: string; candidate_prompt?: string; candidate_percent: number }) =>
                  save.mutate({ cap: edit.capability, body: { ...v, candidate_percent: v.candidate_percent ?? 0 } })
                }
              >
                <Form.Item label="稳定版模型" name="stable_model" rules={[{ required: true, message: '请填写模型' }]}><Input /></Form.Item>
                <Form.Item label="稳定版提示词" name="stable_prompt" rules={[{ required: true }]}>
                  <Select options={edit.prompts.map((p) => ({ value: p, label: p }))} />
                </Form.Item>
                <Form.Item label="候选版模型" name="candidate_model"><Input placeholder="不灰度时留空" /></Form.Item>
                <Form.Item label="候选版提示词" name="candidate_prompt">
                  <Select allowClear options={edit.prompts.map((p) => ({ value: p, label: p }))} />
                </Form.Item>
                <Form.Item label="放量比例（按用户）" name="candidate_percent"><InputNumber min={0} max={100} addonAfter="%" /></Form.Item>
                <Space wrap>
                  <Button type="primary" htmlType="submit" loading={save.isPending}>保存（扩大 / 缩小比例）</Button>
                  {edit.candidate_prompt ? (
                    <>
                      <Popconfirm
                        title="候选版设为稳定版，全部用户使用"
                        onConfirm={() =>
                          save.mutate({ cap: edit.capability, body: { stable_model: edit.candidate_model ?? edit.stable_model, stable_prompt: edit.candidate_prompt!, candidate_percent: 0 } })
                        }
                      >
                        <Button>全量</Button>
                      </Popconfirm>
                      <Popconfirm
                        title="停止灰度，全部回到稳定版"
                        onConfirm={() => save.mutate({ cap: edit.capability, body: { stable_model: edit.stable_model, stable_prompt: edit.stable_prompt, candidate_percent: 0 } })}
                      >
                        <Button danger>回滚</Button>
                      </Popconfirm>
                    </>
                  ) : null}
                </Space>
                <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
                  改提示词或模型后须跑评测（make eval），低于门槛不放量；同一用户在同一能力下稳定落在同一边，方便对比异议率与抽检一致率。
                </Typography.Paragraph>
              </Form>
            </Modal>
          ) : null}
        </>
      ) : null}
    </Loadable>
  );
}

export function ParamsPanel({ keys, exclude }: { keys?: string[]; exclude?: string[] }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const q = useQuery({ queryKey: ['admin', 'params'], queryFn: () => unwrap(api.GET('/admin/config/params')) });
  const save = useMutation({
    mutationFn: ({ p, value }: { p: Schemas['AdminParam']; value: Record<string, unknown> }) =>
      unwrap(api.PUT('/admin/config/params/{key}', { params: { path: { key: p.key } }, body: { value, version: p.version } })),
    onSuccess: () => {
      message.success('已保存，App 下一次请求即生效');
      void qc.invalidateQueries({ queryKey: ['admin', 'params'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const list = (q.data?.items ?? []).filter((p) => (keys ? keys.includes(p.key) : !exclude?.includes(p.key)));
  return (
    <Loadable loading={q.isPending} error={q.error}>
      {list.map((p) => (
        <Card key={`${p.key}-${p.version}`} title={p.key} size="small" style={{ marginBottom: 16 }} extra={<Typography.Text type="secondary">版本 {p.version} · {dt(p.updated_at)}</Typography.Text>}>
          <ParamEditor param={p} saving={save.isPending} onSave={(value) => save.mutate({ p, value })} />
        </Card>
      ))}
    </Loadable>
  );
}

function AgreementsPanel() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const q = useQuery({ queryKey: ['admin', 'agreements'], queryFn: () => unwrap(api.GET('/admin/config/agreements')) });
  const [editing, setEditing] = useState<Schemas['AdminAgreement'] | 'new'>();
  const refresh = () => void qc.invalidateQueries({ queryKey: ['admin', 'agreements'] });
  const publish = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/config/agreements/{agreementId}/publish', { params: { path: { agreementId: id } } })),
    onSuccess: () => {
      message.success('已发布：老用户下次启动时会看到更新提示');
      refresh();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Loadable loading={q.isPending} error={q.error}>
      <Button type="primary" style={{ marginBottom: 12 }} onClick={() => setEditing('new')}>新建版本</Button>
      <Table
        rowKey="id"
        dataSource={q.data?.items ?? []}
        columns={[
          { title: '协议', render: (_, a) => kindNames[a.kind] },
          { title: '版本', dataIndex: 'version' },
          { title: '变更摘要', dataIndex: 'change_summary', ellipsis: true },
          { title: '生效', render: (_, a) => dt(a.effective_at) },
          { title: '状态', render: (_, a) => (a.published_at ? <Tag color="green">已发布 {dt(a.published_at)}</Tag> : <Tag>草稿</Tag>) },
          {
            title: '操作',
            render: (_, a) =>
              a.published_at ? null : (
                <Space>
                  <Button size="small" onClick={async () => setEditing(await unwrap(api.GET('/admin/config/agreements/{agreementId}', { params: { path: { agreementId: a.id } } })))}>
                    编辑
                  </Button>
                  <Popconfirm title="发布后不能再改；老用户启动时会看到 0.4b 确认" onConfirm={() => publish.mutate(a.id)}>
                    <Button size="small" type="primary">发布</Button>
                  </Popconfirm>
                </Space>
              ),
          },
        ]}
      />
      {editing ? <AgreementForm agreement={editing === 'new' ? undefined : editing} onDone={() => { setEditing(undefined); refresh(); }} /> : null}
    </Loadable>
  );
}

function AgreementForm({ agreement, onDone }: { agreement?: Schemas['AdminAgreement']; onDone: () => void }) {
  const { message } = AntApp.useApp();
  const save = useMutation({
    mutationFn: async (v: { kind: 'user' | 'privacy' | 'membership'; version: string; title: string; body: string; change_summary?: string; effective: dayjs.Dayjs }) => {
      const base = { title: v.title, body: v.body, change_summary: v.change_summary, effective_at: v.effective.toISOString() };
      return agreement
        ? unwrap(api.PUT('/admin/config/agreements/{agreementId}', { params: { path: { agreementId: agreement.id } }, body: base }))
        : unwrap(api.POST('/admin/config/agreements', { body: { ...base, kind: v.kind, version: v.version } }));
    },
    onSuccess: () => {
      message.success('草稿已保存');
      onDone();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Modal open width={760} title={agreement ? `编辑草稿 · ${kindNames[agreement.kind]} ${agreement.version}` : '新建协议版本'} footer={null} onCancel={onDone}>
      <Form
        layout="vertical"
        initialValues={{
          kind: agreement?.kind ?? 'user',
          version: agreement?.version,
          title: agreement?.title,
          body: agreement?.body,
          change_summary: agreement?.change_summary,
          effective: agreement ? dayjs(agreement.effective_at) : dayjs(),
        }}
        onFinish={save.mutate}
      >
        <Space>
          <Form.Item label="协议" name="kind">
            <Select disabled={!!agreement} style={{ width: 160 }} options={Object.entries(kindNames).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
          <Form.Item label="版本号" name="version" rules={[{ required: true, message: '如 1.1' }]}>
            <Input disabled={!!agreement} style={{ width: 120 }} />
          </Form.Item>
          <Form.Item label="生效时间" name="effective" rules={[{ required: true }]}>
            <DatePicker showTime />
          </Form.Item>
        </Space>
        <Form.Item label="标题" name="title" rules={[{ required: true }]}><Input maxLength={64} /></Form.Item>
        <Form.Item label="变更摘要（0.4b 弹窗里显示）" name="change_summary"><Input.TextArea rows={2} /></Form.Item>
        <Form.Item label="正文" name="body" rules={[{ required: true }]}><Input.TextArea rows={12} /></Form.Item>
        <Button type="primary" htmlType="submit" loading={save.isPending}>保存草稿</Button>
      </Form>
    </Modal>
  );
}

function ExamDatesPanel() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const q = useQuery({ queryKey: ['admin', 'exam-dates'], queryFn: () => unwrap(api.GET('/admin/config/exam-dates')) });
  const save = useMutation({
    mutationFn: (v: { year: number; label?: string; range: [dayjs.Dayjs, dayjs.Dayjs]; subject: dayjs.Dayjs }) =>
      unwrap(
        api.PUT('/admin/config/exam-dates/{year}', {
          params: { path: { year: v.year } },
          body: { label: v.label, first_exam_start: v.range[0].format('YYYY-MM-DD'), first_exam_end: v.range[1].format('YYYY-MM-DD'), subject_exam_date: v.subject.format('YYYY-MM-DD') },
        }),
      ),
    onSuccess: () => {
      message.success('已保存');
      void qc.invalidateQueries({ queryKey: ['admin', 'exam-dates'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Loadable loading={q.isPending} error={q.error}>
      <Table
        rowKey="year"
        pagination={false}
        dataSource={(q.data?.items ?? []) as unknown as Schemas['AdminExamDate'][]}
        columns={[
          { title: '年份', dataIndex: 'year' },
          { title: '名称', dataIndex: 'label' },
          { title: '初试', render: (_, d) => `${d.first_exam_start} 至 ${d.first_exam_end}` },
          { title: '专业课考试日（倒计时以它为准）', dataIndex: 'subject_exam_date' },
        ]}
      />
      <Card size="small" title="新增或修改某年" style={{ marginTop: 16 }}>
        <Form layout="inline" onFinish={save.mutate}>
          <Form.Item name="year" rules={[{ required: true, message: '年份' }]}><InputNumber placeholder="2028" min={2000} max={2100} /></Form.Item>
          <Form.Item name="label"><Input placeholder="2028 研考" /></Form.Item>
          <Form.Item name="range" rules={[{ required: true, message: '初试日期' }]}><DatePicker.RangePicker /></Form.Item>
          <Form.Item name="subject" rules={[{ required: true, message: '专业课考试日' }]}><DatePicker placeholder="专业课考试日" /></Form.Item>
          <Button type="primary" htmlType="submit" loading={save.isPending}>保存</Button>
        </Form>
        <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
          冲刺卡至当年初试结束、考季卡至次年初试结束，都按这里的日期算；次年日期没配置时考季卡暂不能购买。
        </Typography.Paragraph>
      </Card>
    </Loadable>
  );
}

function FlagsPanel() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const q = useQuery({ queryKey: ['admin', 'flags'], queryFn: () => unwrap(api.GET('/admin/config/flags')) });
  const save = useMutation({
    mutationFn: ({ key, all, ids }: { key: string; all: boolean; ids: number[] }) =>
      unwrap(api.PUT('/admin/config/flags/{flagKey}', { params: { path: { flagKey: key } }, body: { enabled_all: all, user_ids: ids } })),
    onSuccess: () => {
      message.success('已保存，立即生效');
      void qc.invalidateQueries({ queryKey: ['admin', 'flags'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Loadable loading={q.isPending} error={q.error}>
      <Table
        rowKey="key"
        pagination={false}
        dataSource={q.data?.items ?? []}
        columns={[
          { title: '开关', render: (_, f) => <><Typography.Text strong>{f.key}</Typography.Text><br /><Typography.Text type="secondary">{f.description}</Typography.Text></> },
          {
            title: '全部打开',
            render: (_, f) => <Switch aria-label={`${f.key} 全部打开`} checked={f.enabled_all} onChange={(all) => save.mutate({ key: f.key, all, ids: f.user_ids })} />,
          },
          {
            title: '只对指定用户打开（用户 ID，逗号分隔）',
            render: (_, f) => (
              <Input.Search
                defaultValue={f.user_ids.join(',')}
                enterButton="保存"
                onSearch={(v) =>
                  save.mutate({ key: f.key, all: f.enabled_all, ids: v.split(/[,，\s]+/).map((x) => Number(x)).filter((x) => Number.isInteger(x) && x > 0) })
                }
              />
            ),
          },
        ]}
      />
      <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>关闭的功能 App 不显示入口、接口返回 404。上线前先只对自己的账号打开自测。</Typography.Paragraph>
    </Loadable>
  );
}

function VersionsPanel() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const q = useQuery({ queryKey: ['admin', 'app-versions'], queryFn: () => unwrap(api.GET('/admin/config/app-versions')) });
  const save = useMutation({
    mutationFn: (v: { platform: 'ios' | 'android'; latest: string; min: string; download_url: string; release_notes?: string }) =>
      unwrap(api.PUT('/admin/config/app-versions/{platform}', { params: { path: { platform: v.platform } }, body: v })),
    onSuccess: () => {
      message.success('已保存');
      void qc.invalidateQueries({ queryKey: ['admin', 'app-versions'] });
    },
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <Loadable loading={q.isPending} error={q.error}>
      <Row gutter={16}>
        {(['ios', 'android'] as const).map((pl) => {
          const v = q.data?.items.find((x) => x.platform === pl);
          return (
            <Col span={12} key={pl}>
              <Card title={pl === 'ios' ? 'iOS' : '安卓'} size="small">
                <Form layout="vertical" initialValues={{ platform: pl, ...v }} onFinish={save.mutate}>
                  <Form.Item name="platform" hidden><Input /></Form.Item>
                  <Form.Item label="最新版本" name="latest" rules={[{ required: true, pattern: /^\d+\.\d+\.\d+$/, message: '如 1.2.0' }]}><Input /></Form.Item>
                  <Form.Item label="最低版本（低于它强制更新）" name="min" rules={[{ required: true, pattern: /^\d+\.\d+\.\d+$/, message: '如 1.0.0' }]}><Input /></Form.Item>
                  <Form.Item label="下载地址" name="download_url" rules={[{ required: true, pattern: /^https:\/\//, message: '必须是 https 地址' }]}><Input /></Form.Item>
                  <Form.Item label="更新说明" name="release_notes"><Input.TextArea rows={3} /></Form.Item>
                  <Button type="primary" htmlType="submit" loading={save.isPending}>保存</Button>
                </Form>
              </Card>
            </Col>
          );
        })}
      </Row>
    </Loadable>
  );
}
