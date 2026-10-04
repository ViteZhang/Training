// 7.15 后台账号与权限：角色与成员管理、添加成员；操作日志查询。新成员用初始密码首次登录必须修改；所有账号强制两步验证。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Card, Form, Input, Modal, Select, Space, Switch, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { api, dt, unwrap } from '../lib/api';

export const roleNames: Record<string, string> = { admin: '管理员', support: '客服', content_lead: '内容负责人', content_editor: '内容编辑', analyst: '数据分析' };
const roleDesc: Record<string, string> = {
  admin: '全部',
  support: '用户、订单（只读）、反馈、兑换码查询；经用户授权查看资料 72 小时',
  content_lead: '官方题库全部、审核、发布与回滚',
  content_editor: '被分配的官方题库生产任务',
  analyst: '概览、需求洞察，只看统计',
};
const roleOptions = Object.entries(roleNames).map(([value, label]) => ({ value, label }));

function errMsg(e: unknown) {
  return e instanceof ApiError ? e.message : '操作失败，请重试';
}

export function Roles() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const accounts = useQuery({ queryKey: ['admin', 'accounts'], queryFn: () => unwrap(api.GET('/admin/accounts')) });
  const logs = useQuery({ queryKey: ['admin', 'audit', 'all'], queryFn: () => unwrap(api.GET('/admin/audit-logs')) });
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Schemas['AdminAccount']>();
  const [resetting, setResetting] = useState<Schemas['AdminAccount']>();
  const refresh = () => void qc.invalidateQueries({ queryKey: ['admin', 'accounts'] });
  const ok = (msg: string) => () => {
    message.success(msg);
    setAdding(false);
    setEditing(undefined);
    setResetting(undefined);
    refresh();
  };
  const add = useMutation({
    mutationFn: (v: { username: string; display_name: string; phone: string; roles: string[]; password: string }) => unwrap(api.POST('/admin/accounts', { body: v })),
    onSuccess: ok('已添加，请线下告知初始密码'),
    onError: (e) => message.error(errMsg(e)),
  });
  const update = useMutation({
    mutationFn: (v: { id: number; display_name: string; roles: string[]; active: boolean }) =>
      unwrap(api.PUT('/admin/accounts/{accountId}', { params: { path: { accountId: v.id } }, body: { display_name: v.display_name, roles: v.roles, active: v.active } })),
    onSuccess: ok('已保存，该成员需重新登录'),
    onError: (e) => message.error(errMsg(e)),
  });
  const reset = useMutation({
    mutationFn: (v: { id: number; password: string }) => unwrap(api.POST('/admin/accounts/{accountId}/password', { params: { path: { accountId: v.id } }, body: { password: v.password } })),
    onSuccess: ok('密码已重置，对方下次登录须修改'),
    onError: (e) => message.error(errMsg(e)),
  });
  return (
    <>
      <PageTitle code="7.15" title="后台账号与权限" extra={<Button type="primary" onClick={() => setAdding(true)}>添加成员</Button>} />
      <Card title="角色" size="small" style={{ marginBottom: 16 }}>
        {Object.entries(roleNames).map(([k, v]) => (
          <Typography.Paragraph key={k} style={{ margin: 0 }}>
            <Tag>{v}</Tag>
            {roleDesc[k]}
          </Typography.Paragraph>
        ))}
      </Card>
      <Card title="成员" size="small" style={{ marginBottom: 16 }}>
        <Loadable loading={accounts.isPending} error={accounts.error}>
          <Table
            rowKey="id"
            pagination={false}
            dataSource={accounts.data?.items ?? []}
            columns={[
              { title: '账号', dataIndex: 'username' },
              { title: '显示名', dataIndex: 'display_name' },
              { title: '手机号（两步验证）', dataIndex: 'phone_masked' },
              { title: '角色', render: (_, a) => a.roles.map((r) => <Tag key={r}>{roleNames[r] ?? r}</Tag>) },
              { title: '状态', render: (_, a) => (a.status === 'active' ? (a.must_change_password ? '待改初始密码' : '正常') : <Tag color="red">已停用</Tag>) },
              { title: '最近登录', render: (_, a) => dt(a.last_login_at) },
              {
                title: '操作',
                render: (_, a) => (
                  <Space>
                    <Button size="small" onClick={() => setEditing(a)}>编辑</Button>
                    <Button size="small" onClick={() => setResetting(a)}>重置密码</Button>
                  </Space>
                ),
              },
            ]}
          />
        </Loadable>
      </Card>
      <Card title="操作日志（保留 180 天，不能删除）" size="small">
        <Loadable loading={logs.isPending} error={logs.error}>
          <Table
            size="small"
            rowKey="id"
            dataSource={logs.data?.items ?? []}
            columns={[
              { title: '时间', render: (_, l) => dt(l.created_at) },
              { title: '操作人', dataIndex: 'admin_name' },
              { title: '操作', dataIndex: 'action' },
              { title: '对象', render: (_, l) => (l.target_type ? `${l.target_type} ${l.target_id ?? ''}` : '—') },
            ]}
          />
        </Loadable>
      </Card>

      <Modal open={adding} title="添加成员" footer={null} onCancel={() => setAdding(false)} destroyOnHidden>
        <Form layout="vertical" onFinish={add.mutate}>
          <Form.Item label="账号" name="username" rules={[{ required: true, pattern: /^[a-z0-9_]{3,32}$/, message: '3–32 位小写字母、数字、下划线' }]}><Input /></Form.Item>
          <Form.Item label="显示名（水印与日志里显示）" name="display_name" rules={[{ required: true }]}><Input maxLength={32} /></Form.Item>
          <Form.Item label="手机号（接收两步验证码）" name="phone" rules={[{ required: true, pattern: /^1[3-9]\d{9}$/, message: '请输入手机号' }]}><Input /></Form.Item>
          <Form.Item label="角色" name="roles" rules={[{ required: true, message: '至少选一个角色' }]}><Select mode="multiple" options={roleOptions} /></Form.Item>
          <Form.Item label="初始密码（首次登录必须修改）" name="password" rules={[{ required: true, pattern: /^(?=.*[A-Za-z])(?=.*\d).{10,}$/, message: '至少 10 位，同时包含字母和数字' }]}>
            <Input.Password />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={add.isPending}>添加</Button>
        </Form>
      </Modal>
      {editing ? (
        <Modal open title={`编辑 · ${editing.username}`} footer={null} onCancel={() => setEditing(undefined)} destroyOnHidden>
          <Form
            layout="vertical"
            initialValues={{ display_name: editing.display_name, roles: editing.roles, active: editing.status === 'active' }}
            onFinish={(v: { display_name: string; roles: string[]; active: boolean }) => update.mutate({ id: editing.id, ...v })}
          >
            <Form.Item label="显示名" name="display_name" rules={[{ required: true }]}><Input maxLength={32} /></Form.Item>
            <Form.Item label="角色" name="roles" rules={[{ required: true }]}><Select mode="multiple" options={roleOptions} /></Form.Item>
            <Form.Item label="启用" name="active" valuePropName="checked"><Switch /></Form.Item>
            <Button type="primary" htmlType="submit" loading={update.isPending}>保存</Button>
          </Form>
        </Modal>
      ) : null}
      {resetting ? (
        <Modal open title={`重置密码 · ${resetting.username}`} footer={null} onCancel={() => setResetting(undefined)} destroyOnHidden>
          <Form layout="vertical" onFinish={(v: { password: string }) => reset.mutate({ id: resetting.id, password: v.password })}>
            <Form.Item label="新的初始密码" name="password" rules={[{ required: true, pattern: /^(?=.*[A-Za-z])(?=.*\d).{10,}$/, message: '至少 10 位，同时包含字母和数字' }]}>
              <Input.Password />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={reset.isPending}>重置</Button>
          </Form>
        </Modal>
      ) : null}
    </>
  );
}
