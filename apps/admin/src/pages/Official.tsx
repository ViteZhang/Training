// 7.11 官方题库：项目列表与进度（立项 → 授权资料入库 → 知识框架 → 内容生产 → 审核 → 发布上线），
// 分配编辑、维护授权资料清单（须有授权或为公开真题）。
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Descriptions, Drawer, Form, Input, Select, Space, Steps, Table, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { stageNames, useProjects } from '../components/ProjectPicker';
import { api, dt, unwrap } from '../lib/api';
import { CreateProject } from './Demand';

const stages = Object.keys(stageNames);
const basisNames: Record<string, string> = { authorized: '已获授权', public_exam: '公开真题' };

export function Official() {
  const list = useProjects();
  const [open, setOpen] = useState<number>();
  const [creating, setCreating] = useState(false);
  return (
    <>
      <PageTitle code="7.11" title="官方题库" extra={<Button type="primary" onClick={() => setCreating(true)}>新建项目</Button>} />
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey="id"
          dataSource={list.data?.items ?? []}
          onRow={(p) => ({ onClick: () => setOpen(p.id), style: { cursor: 'pointer' } })}
          columns={[
            { title: '院校 · 专业', render: (_, p) => `${p.school} · ${p.major}` },
            { title: '专业课', render: (_, p) => `${p.subject_code} ${p.subject_name}` },
            { title: '进度', render: (_, p) => stageNames[p.stage] },
            { title: '知识点', dataIndex: 'kp_count' },
            { title: '题目', dataIndex: 'question_count' },
            { title: '真题', dataIndex: 'exam_count' },
            { title: '版本', render: (_, p) => p.version || '未发布' },
            { title: '已添加用户', dataIndex: 'subscribers' },
          ]}
        />
      </Loadable>
      {open ? <ProjectDrawer id={open} onClose={() => setOpen(undefined)} /> : null}
      {creating ? <CreateProject onClose={() => setCreating(false)} /> : null}
    </>
  );
}

interface Material {
  name: string;
  basis: 'authorized' | 'public_exam';
  note?: string;
}

function ProjectDrawer({ id, onClose }: { id: number; onClose: () => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const path = { params: { path: { projectId: id } } };
  const q = useQuery({ queryKey: ['admin', 'official', 'project', id], queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}', path)) });
  const accounts = useQuery({ queryKey: ['admin', 'accounts'], queryFn: () => unwrap(api.GET('/admin/accounts')), retry: false });
  const save = useMutation({
    mutationFn: (v: { editor_ids: number[]; materials: Material[]; stage: (typeof stages)[number] }) =>
      unwrap(api.PUT('/admin/official/projects/{projectId}', { ...path, body: v as never })),
    onSuccess: () => {
      message.success('已保存');
      void qc.invalidateQueries({ queryKey: ['admin', 'official'] });
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '保存失败'),
  });
  const p = q.data;
  const editors = (accounts.data?.items ?? []).filter((a) => a.roles.includes('content_editor') || a.roles.includes('content_lead'));
  return (
    <Drawer open width={720} title={p ? `${p.school} ${p.subject_code} ${p.subject_name}` : '项目'} onClose={onClose}>
      <Loadable loading={q.isPending} error={q.error}>
        {p ? (
          <>
            <Steps size="small" current={stages.indexOf(p.stage)} items={stages.map((s) => ({ title: stageNames[s] }))} style={{ marginBottom: 24 }} />
            <Descriptions column={2} size="small" style={{ marginBottom: 16 }}>
              <Descriptions.Item label="知识点">{p.kp_count}</Descriptions.Item>
              <Descriptions.Item label="题目">{p.question_count}（真题 {p.exam_count}）</Descriptions.Item>
              <Descriptions.Item label="当前版本">{p.version || '未发布'}</Descriptions.Item>
              <Descriptions.Item label="已添加用户">{p.subscribers}</Descriptions.Item>
              <Descriptions.Item label="立项时间">{dt(p.created_at)}</Descriptions.Item>
            </Descriptions>
            <Form
              layout="vertical"
              initialValues={{ editor_ids: p.editor_ids, materials: p.materials, stage: p.stage }}
              onFinish={(v: { editor_ids: number[]; materials: Material[]; stage: string }) => save.mutate({ ...v, materials: v.materials ?? [] })}
            >
              <Form.Item name="stage" label="阶段" extra="「发布上线」在版本发布页完成">
                <Select options={stages.map((s) => ({ value: s, label: stageNames[s], disabled: s === 'published' && p.stage !== 'published' }))} />
              </Form.Item>
              <Form.Item name="editor_ids" label="分配编辑">
                <Select mode="multiple" options={editors.map((a) => ({ value: a.id, label: `${a.display_name}（${a.username}）` }))} placeholder="内容编辑只能访问被分配的项目" />
              </Form.Item>
              <Typography.Text strong>授权资料清单</Typography.Text>
              <Typography.Paragraph type="secondary">只能使用已获授权的资料或公开真题，逐项登记依据。</Typography.Paragraph>
              <Form.List name="materials">
                {(fields, { add, remove }) => (
                  <>
                    {fields.map((f) => (
                      <Space key={f.key} align="start">
                        <Form.Item name={[f.name, 'name']} rules={[{ required: true, message: '填资料名称' }]}>
                          <Input placeholder="资料名称" style={{ width: 240 }} />
                        </Form.Item>
                        <Form.Item name={[f.name, 'basis']} rules={[{ required: true, message: '选依据' }]}>
                          <Select placeholder="依据" style={{ width: 120 }} options={Object.entries(basisNames).map(([value, label]) => ({ value, label }))} />
                        </Form.Item>
                        <Form.Item name={[f.name, 'note']}>
                          <Input placeholder="备注（授权方、合同编号）" style={{ width: 200 }} />
                        </Form.Item>
                        <Button onClick={() => remove(f.name)}>删除</Button>
                      </Space>
                    ))}
                    <Button onClick={() => add({ basis: 'authorized' })} style={{ marginBottom: 16 }}>
                      添加资料
                    </Button>
                  </>
                )}
              </Form.List>
              <div>
                <Button type="primary" htmlType="submit" loading={save.isPending}>
                  保存
                </Button>
              </div>
            </Form>
          </>
        ) : null}
      </Loadable>
    </Drawer>
  );
}
