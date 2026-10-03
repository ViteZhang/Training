// 7.10 需求洞察：按专业课代码统计自建用户（只有计数，不含用户资料内容）；立项做官方题库、标为未规划。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Form, Input, Modal, Space, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { useNavigate } from 'react-router';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { stageNames } from '../components/ProjectPicker';
import { api, pct, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';

/** 达到立项门槛的自建用户数：只做提示，是否立项由内容负责人决定。 */
const threshold = 30;

export function Demand({ roles }: { roles: Role[] }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const nav = useNavigate();
  const canAct = roles.includes('admin') || roles.includes('content_lead');
  const list = useQuery({ queryKey: ['admin', 'demand'], queryFn: () => unwrap(api.GET('/admin/demand')) });
  const [creating, setCreating] = useState<string>();
  const mark = useMutation({
    mutationFn: (v: { code: string; unplanned: boolean }) =>
      unwrap(api.PUT('/admin/demand/{subjectCode}', { params: { path: { subjectCode: v.code } }, body: { unplanned: v.unplanned } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['admin', 'demand'] }),
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  return (
    <>
      <PageTitle code="7.10" title="需求洞察" />
      <Typography.Paragraph type="secondary">按专业课代码统计自建题库的用户；达到 {threshold} 人标为「达到立项门槛」。</Typography.Paragraph>
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey="subject_code"
          dataSource={list.data?.items ?? []}
          columns={[
            { title: '专业课代码', dataIndex: 'subject_code' },
            { title: '自建用户', dataIndex: 'users', sorter: (a, b) => a.users - b.users, defaultSortOrder: 'descend' },
            { title: '人均题量', dataIndex: 'avg_questions' },
            { title: '近 7 天新增', dataIndex: 'week_new' },
            { title: '填了目标院校', dataIndex: 'with_target' },
            { title: '付费率', render: (_, d) => pct(d.paid_rate) },
            { title: '人均待复习', dataIndex: 'avg_review' },
            {
              title: '状态',
              render: (_, d) =>
                d.project_id ? (
                  <Tag color="green">已立项 · {stageNames[d.project_stage ?? ''] ?? d.project_stage}</Tag>
                ) : d.unplanned ? (
                  <Tag>未规划</Tag>
                ) : d.users >= threshold ? (
                  <Tag color="orange">达到立项门槛</Tag>
                ) : (
                  '—'
                ),
            },
            {
              title: '操作',
              render: (_, d) =>
                !canAct ? null : d.project_id ? (
                  <Button size="small" onClick={() => nav('/official')}>
                    查看项目
                  </Button>
                ) : (
                  <Space>
                    <Button size="small" type="primary" onClick={() => setCreating(d.subject_code)}>
                      立项做官方题库
                    </Button>
                    <Button size="small" onClick={() => mark.mutate({ code: d.subject_code, unplanned: !d.unplanned })}>
                      {d.unplanned ? '取消标记' : '标为未规划'}
                    </Button>
                  </Space>
                ),
            },
          ]}
        />
      </Loadable>
      {creating ? <CreateProject code={creating} onClose={() => setCreating(undefined)} onCreated={() => nav('/official')} /> : null}
      <PrivacyNote />
    </>
  );
}

export function CreateProject({ code, onClose, onCreated }: { code?: string; onClose: () => void; onCreated?: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [form] = Form.useForm<{ school: string; major: string; subject_code: string; subject_name: string }>();
  const create = useMutation({
    mutationFn: (v: { school: string; major: string; subject_code: string; subject_name: string }) => unwrap(api.POST('/admin/official/projects', { body: v })),
    onSuccess: () => {
      message.success('已立项，接下来分配编辑、登记授权资料');
      void qc.invalidateQueries({ queryKey: ['admin'] });
      onClose();
      onCreated?.();
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '立项失败'),
  });
  return (
    <Modal open title="立项做官方题库" okText="立项" onCancel={onClose} onOk={() => form.submit()} confirmLoading={create.isPending}>
      <Form form={form} layout="vertical" initialValues={{ subject_code: code }} onFinish={(v) => create.mutate(v)}>
        <Form.Item name="school" label="院校" rules={[{ required: true }]}>
          <Input maxLength={64} />
        </Form.Item>
        <Form.Item name="major" label="专业" rules={[{ required: true }]}>
          <Input maxLength={64} />
        </Form.Item>
        <Form.Item name="subject_code" label="专业课代码" rules={[{ required: true }]}>
          <Input maxLength={32} />
        </Form.Item>
        <Form.Item name="subject_name" label="专业课名称" rules={[{ required: true }]}>
          <Input maxLength={64} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
