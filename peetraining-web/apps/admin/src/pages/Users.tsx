// 7.2 用户：搜索手机号、用户 ID、邀请码；筛选：全部、付费、额度用完、解析失败、未导入资料；手机号脱敏；
// 详情只有数量与状态（资料和题目内容不可见）；操作：加解析额度、赠送会员天数、重新解析失败文件、发送消息、封禁账号；操作记录。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Descriptions, Drawer, Form, Input, InputNumber, Modal, Popconfirm, Segmented, Space, Table, Tag, Timeline, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { api, dt, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';

type Filter = '' | 'paid' | 'quota_out' | 'parse_failed' | 'no_material';
const filters: { label: string; value: Filter }[] = [
  { label: '全部', value: '' },
  { label: '付费', value: 'paid' },
  { label: '额度用完', value: 'quota_out' },
  { label: '解析失败', value: 'parse_failed' },
  { label: '未导入资料', value: 'no_material' },
];
const statusNames: Record<string, string> = { active: '正常', banned: '已封禁', deleting: '注销中' };
const tierNames: Record<string, string> = { sprint: '冲刺卡', season: '考季卡', monthly: '月卡', gift: '赠送' };

export function Users({ roles }: { roles: Role[] }) {
  const [q, setQ] = useState('');
  const [filter, setFilter] = useState<Filter>('');
  const [open, setOpen] = useState<number>();
  const list = useQuery({
    queryKey: ['admin', 'users', q, filter],
    queryFn: () => unwrap(api.GET('/admin/users', { params: { query: { q: q || undefined, filter: filter || undefined } } })),
  });
  return (
    <>
      <PageTitle code="7.2" title="用户" />
      <Space style={{ marginBottom: 16 }} wrap>
        <Input.Search placeholder="搜索手机号、用户 ID、邀请码" allowClear onSearch={(v) => setQ(v.trim())} style={{ width: 320 }} />
        <Segmented options={filters} value={filter} onChange={(v) => setFilter(v as Filter)} />
      </Space>
      <Typography.Paragraph type="secondary">手机号脱敏显示，完整号码仅管理员可查</Typography.Paragraph>
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey="id"
          dataSource={list.data?.items ?? []}
          onRow={(r) => ({ onClick: () => setOpen(r.id), style: { cursor: 'pointer' } })}
          columns={[
            { title: '手机号', dataIndex: 'phone_masked' },
            { title: '专业课', dataIndex: 'subjects' },
            { title: '资料 / 题目', render: (_, r) => `${r.materials} 份 · ${r.questions} 题` },
            { title: '会员', render: (_, r) => (r.is_member ? <Tag color="gold">会员</Tag> : '免费') },
            { title: '最近活跃', render: (_, r) => dt(r.last_active_at) },
            { title: '状态', render: (_, r) => statusNames[r.status] ?? r.status },
          ]}
        />
      </Loadable>
      {open ? <UserDrawer id={open} roles={roles} onClose={() => setOpen(undefined)} /> : null}
      <PrivacyNote />
    </>
  );
}

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

function UserDrawer({ id, roles, onClose }: { id: number; roles: Role[]; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const isAdmin = roles.includes('admin');
  const path = { params: { path: { userId: id } } };
  const user = useQuery({ queryKey: ['admin', 'user', id], queryFn: () => unwrap(api.GET('/admin/users/{userId}', path)) });
  const logs = useQuery({
    queryKey: ['admin', 'audit', 'user', id],
    queryFn: () => unwrap(api.GET('/admin/audit-logs', { params: { query: { target_type: 'user', target_id: String(id) } } })),
  });
  const [phone, setPhone] = useState<string>();
  const [modal, setModal] = useState<'pages' | 'days' | 'message'>();
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ['admin', 'user', id] });
    void qc.invalidateQueries({ queryKey: ['admin', 'audit', 'user', id] });
    void qc.invalidateQueries({ queryKey: ['admin', 'users'] });
  };
  const act = useMutation({
    mutationFn: async (fn: () => Promise<unknown>) => fn(),
    onSuccess: () => {
      message.success('已完成');
      setModal(undefined);
      refresh();
    },
    onError: (e) => message.error(errMsg(e)),
  });
  const u = user.data;
  return (
    <Drawer open width={640} title={u ? `${u.phone_masked} · ${u.nickname}` : '用户详情'} onClose={onClose}>
      <Loadable loading={user.isPending} error={user.error}>
        {u ? (
          <>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="注册">{dt(u.created_at)} · 邀请码 {u.invite_code} · {statusNames[u.status]}</Descriptions.Item>
              <Descriptions.Item label="专业课">
                {u.subject_stats.map((s) => `${s.code ?? ''} ${s.name}${s.est_low !== undefined ? ` · 预估 ${s.est_low}–${s.est_high}` : ''}`).join('；') || '—'}
              </Descriptions.Item>
              <Descriptions.Item label="资料">{u.materials} 份 · {u.pages} 页 · 解析失败 {u.failed_materials} 份</Descriptions.Item>
              <Descriptions.Item label="解析额度">
                {u.quota.filter((x) => x.quota_type === 'parse_pages').map((x) => `已用 ${x.used} / ${x.limit ?? '不限'} 页`).join('') || '—'}
              </Descriptions.Item>
              <Descriptions.Item label="题库">{u.questions} 题 · {u.knowledge_points} 个知识点</Descriptions.Item>
              <Descriptions.Item label="整卷">已做 {u.papers} 套</Descriptions.Item>
              <Descriptions.Item label="本周批改">{u.week_gradings} 次</Descriptions.Item>
              <Descriptions.Item label="会员">{u.is_member ? `${tierNames[u.member_tier ?? ''] ?? '会员'} · 至 ${dt(u.member_until)}` : '免费版'}</Descriptions.Item>
              <Descriptions.Item label="邀请">邀请 {u.invited} 人 · 获得 {u.invite_days} 天</Descriptions.Item>
              {isAdmin ? (
                <Descriptions.Item label="完整手机号">
                  {phone ?? (
                    <Button size="small" onClick={async () => setPhone((await unwrap(api.GET('/admin/users/{userId}/phone', path))).phone)}>
                      查看（记日志）
                    </Button>
                  )}
                </Descriptions.Item>
              ) : null}
            </Descriptions>
            <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
              资料和题目内容：不可见。用户在反馈里勾选「允许查看这份资料排查问题」后，客服可查看 72 小时，操作会留痕。
            </Typography.Paragraph>
            <Space wrap style={{ margin: '12px 0' }}>
              <Button onClick={() => setModal('pages')}>加解析额度</Button>
              {isAdmin ? <Button onClick={() => setModal('days')}>赠送会员天数</Button> : null}
              <Button
                loading={act.isPending}
                onClick={() => act.mutate(() => unwrap(api.POST('/admin/users/{userId}/reparse', path)))}
              >
                重新解析失败文件
              </Button>
              <Button onClick={() => setModal('message')}>发送消息</Button>
              {isAdmin ? (
                <Popconfirm
                  title={u.status === 'banned' ? '解封这个账号？' : '封禁后用户立即退出登录，确定吗？'}
                  onConfirm={() => act.mutate(() => unwrap(api.POST('/admin/users/{userId}/status', { ...path, body: { banned: u.status !== 'banned' } })))}
                >
                  <Button danger>{u.status === 'banned' ? '解封账号' : '封禁账号'}</Button>
                </Popconfirm>
              ) : null}
            </Space>
            <Typography.Title level={5}>操作记录</Typography.Title>
            <Timeline
              items={(logs.data?.items ?? []).map((l: Schemas['AdminAudit']) => ({ children: `${dt(l.created_at)} · ${l.admin_name} · ${l.action}` }))}
            />
          </>
        ) : null}
      </Loadable>
      <Modal open={modal === 'pages'} title="加解析额度" footer={null} onCancel={() => setModal(undefined)} destroyOnHidden>
        <Form
          layout="vertical"
          onFinish={(v: { pages: number }) =>
            act.mutate(() => unwrap(api.POST('/admin/users/{userId}/parse-pages', { ...path, body: { pages: v.pages, idempotency_key: crypto.randomUUID() } })))
          }
        >
          <Form.Item label="页数" name="pages" rules={[{ required: true, message: '请输入页数' }]}>
            <InputNumber min={1} max={2000} style={{ width: '100%' }} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={act.isPending}>确定</Button>
        </Form>
      </Modal>
      <Modal open={modal === 'days'} title="赠送会员天数" footer={null} onCancel={() => setModal(undefined)} destroyOnHidden>
        <Form layout="vertical" onFinish={(v: { days: number }) => act.mutate(() => unwrap(api.POST('/admin/users/{userId}/membership-days', { ...path, body: v })))}>
          <Form.Item label="天数（叠加到当前会员之后）" name="days" rules={[{ required: true, message: '请输入天数' }]}>
            <InputNumber min={1} max={400} style={{ width: '100%' }} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={act.isPending}>确定</Button>
        </Form>
      </Modal>
      <Modal open={modal === 'message'} title="发送站内消息" footer={null} onCancel={() => setModal(undefined)} destroyOnHidden>
        <Form layout="vertical" onFinish={(v: { title: string; body: string }) => act.mutate(() => unwrap(api.POST('/admin/users/{userId}/messages', { ...path, body: v })))}>
          <Form.Item label="标题" name="title" rules={[{ required: true, min: 2, message: '请输入标题' }]}>
            <Input maxLength={64} />
          </Form.Item>
          <Form.Item label="内容" name="body" rules={[{ required: true, min: 2, message: '请输入内容' }]}>
            <Input.TextArea maxLength={500} rows={4} />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={act.isPending}>发送</Button>
        </Form>
      </Modal>
    </Drawer>
  );
}
