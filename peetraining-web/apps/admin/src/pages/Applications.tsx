// 7.16 申请收录（PRD v3 增补，设计稿 n_admin_apply）：提醒条（超 24 小时未处理、达到立项阈值）、统计卡、按院校 · 专业 · 专业课汇总的列表
// （申请人数、最早待处理时间、付费 / 种子标记、状态），待处理置顶；按学科筛选、导出；右侧详情：申请人列表；
// 操作：立项（生成 7.11 项目）、联系用户（站内信）、暂不收录（必填原因）。处理结果自动通知所有申请人。只显示申请表单信息。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Col, Drawer, Form, Input, List, Modal, Row, Select, Space, Statistic, Switch, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { useNavigate } from 'react-router';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { api, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';

type Group = Schemas['AdminCourseApplicationGroup'];

export const applicationStatus: Record<Group['status'], { text: string; color?: string }> = {
  pending: { text: '待处理', color: 'gold' },
  suggested: { text: '建议立项', color: 'orange' },
  contacted: { text: '已联系', color: 'blue' },
  initiated: { text: '已立项', color: 'green' },
  rejected: { text: '暂不收录' },
};

const entryNames = { onboarding: '引导', bank: '题库页', settings: '备考设置' } as const;

/** 距现在多少小时前，如「31 小时前」。 */
export function hoursAgo(iso: string | undefined, now = Date.now()) {
  if (!iso) return '—';
  const h = Math.floor((now - new Date(iso).getTime()) / 3_600_000);
  return h < 1 ? '1 小时内' : `${h} 小时前`;
}

/** 导出为 CSV（UTF-8 带 BOM，Excel 能直接打开）。 */
export function toCSV(rows: Group[]) {
  const head = ['院校', '专业', '专业课', '代码', '学科', '申请人数', '付费', '种子', '最早待处理', '状态', '原因'];
  const esc = (v: unknown) => `"${String(v ?? '').replace(/"/g, '""')}"`;
  const lines = rows.map((g) =>
    [g.school, g.major, g.subject_name, g.subject_code, g.discipline, g.applicant_count, g.paid_count, g.seed_count, g.pending_since, applicationStatus[g.status].text, g.reason].map(esc).join(','),
  );
  return '﻿' + [head.map(esc).join(','), ...lines].join('\n');
}

export function Applications({ roles }: { roles: Role[] }) {
  const qc = useQueryClient();
  const nav = useNavigate();
  const { message } = AntApp.useApp();
  const canAct = roles.includes('admin') || roles.includes('content_lead');
  const [onlyPending, setOnlyPending] = useState(false);
  const [discipline, setDiscipline] = useState<string>();
  const [current, setCurrent] = useState<Group>();
  const [action, setAction] = useState<'initiate' | 'contact' | 'reject'>();
  const summary = useQuery({ queryKey: ['admin', 'applications', 'summary'], queryFn: () => unwrap(api.GET('/admin/course-applications/summary')) });
  const list = useQuery({
    queryKey: ['admin', 'applications', discipline, onlyPending],
    queryFn: () => unwrap(api.GET('/admin/course-applications', { params: { query: { discipline, only_pending: onlyPending || undefined } } })),
  });
  const applicants = useQuery({
    queryKey: ['admin', 'applications', 'applicants', current?.id],
    queryFn: () => unwrap(api.GET('/admin/course-applications/{groupId}/applicants', { params: { path: { groupId: current!.id } } })),
    enabled: !!current,
  });
  const refresh = () => void qc.invalidateQueries({ queryKey: ['admin', 'applications'] });
  const fail = (e: unknown) => message.error(e instanceof ApiError ? e.message : '操作失败');
  const initiate = useMutation({
    mutationFn: (v: { subject_code?: string; major?: string }) =>
      unwrap(api.POST('/admin/course-applications/{groupId}/initiate', { params: { path: { groupId: current!.id } }, body: v })),
    onSuccess: () => {
      message.success('已立项，已通知所有申请人');
      setAction(undefined);
      setCurrent(undefined);
      refresh();
      nav('/official');
    },
    onError: fail,
  });
  const contact = useMutation({
    mutationFn: (v: { message: string }) => unwrap(api.POST('/admin/course-applications/{groupId}/contact', { params: { path: { groupId: current!.id } }, body: v })),
    onSuccess: () => {
      message.success('已给所有申请人发站内信');
      setAction(undefined);
      refresh();
    },
    onError: fail,
  });
  const reject = useMutation({
    mutationFn: (v: { reason: string }) => unwrap(api.POST('/admin/course-applications/{groupId}/reject', { params: { path: { groupId: current!.id } }, body: v })),
    onSuccess: () => {
      message.success('已标为暂不收录，已通知所有申请人');
      setAction(undefined);
      setCurrent(undefined);
      refresh();
    },
    onError: fail,
  });

  const items = list.data?.items ?? [];
  const disciplines = [...new Set(items.map((g) => g.discipline).filter(Boolean))];
  const s = summary.data;
  const exportCSV = () => {
    const url = URL.createObjectURL(new Blob([toCSV(items)], { type: 'text/csv;charset=utf-8' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = '申请收录.csv';
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <>
      <PageTitle code="7.16" title="申请收录" />
      {s && (s.overdue > 0 || s.suggested > 0) ? (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message={`${s.overdue} 门课超过 24 小时未处理，${s.suggested} 门课达到立项阈值。新申请已同步推送到运营企业微信群。`}
          action={<Button size="small" onClick={() => setOnlyPending(true)}>只看待处理</Button>}
        />
      ) : null}
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={6}><Card><Statistic title="待处理申请" value={s?.pending ?? 0} suffix={s?.overdue ? <Typography.Text type="danger" style={{ fontSize: 12 }}>超 24 小时 {s.overdue}</Typography.Text> : null} /></Card></Col>
        <Col span={6}><Card><Statistic title="建议立项（同一门课 ≥ 3 人）" value={s?.suggested ?? 0} /></Card></Col>
        <Col span={6}><Card><Statistic title="本周新增申请" value={s?.week_new ?? 0} /></Card></Col>
        <Col span={6}><Card><Statistic title="涉及院校专业" value={s?.school_majors ?? 0} /></Card></Col>
      </Row>
      <Space style={{ marginBottom: 12 }}>
        <Select allowClear placeholder="按学科筛选" style={{ width: 160 }} value={discipline} onChange={setDiscipline} options={disciplines.map((d) => ({ value: d, label: d }))} />
        <Space>
          <Switch checked={onlyPending} onChange={setOnlyPending} />
          只看待处理
        </Space>
        <Button onClick={exportCSV} disabled={items.length === 0}>导出</Button>
      </Space>
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey="id"
          dataSource={items}
          pagination={{ pageSize: 50 }}
          columns={[
            { title: '院校 · 专业 · 专业课', render: (_, g) => `${g.school} · ${g.major} · ${g.subject_code ? `${g.subject_code} ` : ''}${g.subject_name}` },
            { title: '申请人数', dataIndex: 'applicant_count' },
            {
              title: '最早待处理',
              render: (_, g) => (g.pending_since ? <Typography.Text type={g.overdue ? 'danger' : undefined}>{hoursAgo(g.pending_since)}</Typography.Text> : '—'),
            },
            {
              title: '申请人标记',
              render: (_, g) => (
                <Space size={4}>
                  {g.seed_count > 0 ? <Tag color="blue">种子 {g.seed_count}</Tag> : null}
                  {g.paid_count > 0 ? <Tag color="gold">付费 {g.paid_count}</Tag> : null}
                </Space>
              ),
            },
            { title: '状态', render: (_, g) => <Tag color={applicationStatus[g.status].color}>{applicationStatus[g.status].text}</Tag> },
            { title: '操作', render: (_, g) => <Button size="small" onClick={() => setCurrent(g)}>{g.status === 'initiated' ? '查看' : '处理'}</Button> },
          ]}
        />
      </Loadable>
      <Drawer open={!!current} width={480} onClose={() => setCurrent(undefined)} title={current ? `${current.school} · ${current.major} · ${current.subject_name}` : ''}>
        {current ? (
          <Space direction="vertical" style={{ width: '100%' }} size="middle">
            <Space>
              <Tag color={applicationStatus[current.status].color}>{applicationStatus[current.status].text}</Tag>
              {current.reason ? <Typography.Text type="secondary">原因：{current.reason}</Typography.Text> : null}
            </Space>
            <Loadable loading={applicants.isPending} error={applicants.error}>
              <List
                dataSource={applicants.data?.items ?? []}
                renderItem={(a) => (
                  <List.Item>
                    <Space direction="vertical" size={0}>
                      <Typography.Text>用户 {a.nickname || '（未设昵称）'}</Typography.Text>
                      <Typography.Text type="secondary">
                        {a.exam_year} 研考 · {hoursAgo(a.applied_at)} · 入口：{entryNames[a.entry]}
                      </Typography.Text>
                    </Space>
                    <Space size={4}>
                      {a.seed ? <Tag color="blue">种子</Tag> : null}
                      {a.paid ? <Tag color="gold">付费</Tag> : null}
                    </Space>
                  </List.Item>
                )}
              />
            </Loadable>
            <Typography.Paragraph type="secondary">处理结果会自动发消息通知所有申请人。「暂不收录」须填写原因。</Typography.Paragraph>
            {canAct && current.status !== 'initiated' ? (
              <Space direction="vertical" style={{ width: '100%' }}>
                <Button type="primary" block onClick={() => setAction('initiate')}>立项，进入官方题库</Button>
                <Button block onClick={() => setAction('contact')}>联系用户（站内信）</Button>
                <Button block danger onClick={() => setAction('reject')}>暂不收录</Button>
              </Space>
            ) : null}
          </Space>
        ) : null}
      </Drawer>
      <ActionModal
        action={action}
        group={current}
        loading={initiate.isPending || contact.isPending || reject.isPending}
        onCancel={() => setAction(undefined)}
        onInitiate={(v) => initiate.mutate(v)}
        onContact={(v) => contact.mutate(v)}
        onReject={(v) => reject.mutate(v)}
      />
      <PrivacyNote />
    </>
  );
}

function ActionModal({
  action,
  group,
  loading,
  onCancel,
  onInitiate,
  onContact,
  onReject,
}: {
  action?: 'initiate' | 'contact' | 'reject';
  group?: Group;
  loading: boolean;
  onCancel: () => void;
  onInitiate: (v: { subject_code?: string; major?: string }) => void;
  onContact: (v: { message: string }) => void;
  onReject: (v: { reason: string }) => void;
}) {
  const [form] = Form.useForm();
  const title = action === 'initiate' ? '立项' : action === 'contact' ? '联系用户' : '暂不收录';
  return (
    <Modal open={!!action} title={title} onCancel={onCancel} confirmLoading={loading} onOk={() => form.submit()} destroyOnClose>
      <Form
        form={form}
        layout="vertical"
        preserve={false}
        initialValues={{ subject_code: group?.subject_code, major: group?.major }}
        onFinish={(v) => (action === 'initiate' ? onInitiate(v) : action === 'contact' ? onContact(v) : onReject(v))}
      >
        {action === 'initiate' ? (
          <>
            <Form.Item name="major" label="专业"><Input /></Form.Item>
            <Form.Item name="subject_code" label="专业课代码" rules={[{ required: true, message: '官方题库要有专业课代码' }]}><Input /></Form.Item>
            <Typography.Text type="secondary">立项后生成 7.11 官方题库项目，通知所有申请人「已立项」。</Typography.Text>
          </>
        ) : action === 'contact' ? (
          <Form.Item name="message" label="站内信内容" rules={[{ required: true, message: '要填内容' }]}>
            <Input.TextArea rows={4} maxLength={500} showCount placeholder="如：想请你帮忙提供这门课的回忆版真题，回复客服即可" />
          </Form.Item>
        ) : (
          <Form.Item name="reason" label="原因（会通知申请人）" rules={[{ required: true, message: '暂不收录要填原因' }]}>
            <Input.TextArea rows={3} maxLength={200} showCount />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}
