// 7.10 需求洞察：按「院校 + 专业课代码」统计自建用户（全国统考只按代码；只有计数，不含用户资料内容）；立项做官方题库、标为未规划。
// 立项从考试科目库选一条考试科目（T50），不再手填院校、专业、代码、名称。
import { ApiError } from '@/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Form, Modal, Select, Space, Table, Tag, Typography } from 'antd';
import { useState } from 'react';
import { useNavigate } from 'react-router';
import { Loadable, PageTitle, PrivacyNote } from '../components/Page';
import { stageNames } from '../components/ProjectPicker';
import { api, pct, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';
import { courseLabel } from './Catalog';

/** 达到立项门槛的自建用户数：只做提示，是否立项由内容负责人决定。 */
const threshold = 30;

export function Demand({ roles }: { roles: Role[] }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const nav = useNavigate();
  const canAct = roles.includes('admin') || roles.includes('content_lead');
  const list = useQuery({ queryKey: ['admin', 'demand'], queryFn: () => unwrap(api.GET('/admin/demand')) });
  const [creating, setCreating] = useState<{ code: string; school: string }>();
  const mark = useMutation({
    mutationFn: (v: { code: string; school: string; unplanned: boolean }) =>
      unwrap(api.PUT('/admin/demand/{subjectCode}', { params: { path: { subjectCode: v.code } }, body: { unplanned: v.unplanned, school: v.school } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['admin', 'demand'] }),
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  return (
    <>
      <PageTitle code="7.10" title="需求洞察" />
      <Typography.Paragraph type="secondary">
        按「院校 + 专业课代码」统计自建题库的用户（全国统考与没记院校的只按代码）；达到 {threshold} 人标为「达到立项门槛」。
      </Typography.Paragraph>
      <Loadable loading={list.isPending} error={list.error}>
        <Table
          rowKey={(d) => `${d.school ?? ''}|${d.subject_code}`}
          dataSource={list.data?.items ?? []}
          columns={[
            { title: '院校', render: (_, d) => d.school || <Typography.Text type="secondary">全国统考 / 未记院校</Typography.Text> },
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
                    <Button size="small" type="primary" onClick={() => setCreating({ code: d.subject_code, school: d.school ?? '' })}>
                      立项做官方题库
                    </Button>
                    <Button size="small" onClick={() => mark.mutate({ code: d.subject_code, school: d.school ?? '', unplanned: !d.unplanned })}>
                      {d.unplanned ? '取消标记' : '标为未规划'}
                    </Button>
                  </Space>
                ),
            },
          ]}
        />
      </Loadable>
      {creating ? <CreateProject code={creating.code} school={creating.school} onClose={() => setCreating(undefined)} onCreated={() => nav('/official')} /> : null}
      <PrivacyNote />
    </>
  );
}

/**
 * 立项做官方题库：从考试科目库选一条还没立项的考试科目（T50），院校、代码、名称取自考试科目；
 * 从需求洞察进来时按代码搜好，院校相同的（或统考）排在前面。库里没有的先去考试科目库新增。
 */
export function CreateProject({ code, school, onClose, onCreated }: { code?: string; school?: string; onClose: () => void; onCreated?: () => void }) {
  const qc = useQueryClient();
  const nav = useNavigate();
  const { message } = AntApp.useApp();
  const [form] = Form.useForm<{ course: number }>();
  const [q, setQ] = useState(code ?? '');
  const courses = useQuery({
    queryKey: ['admin', 'catalog', q, 'create-project'],
    queryFn: () => unwrap(api.GET('/admin/catalog/courses', { params: { query: { q: q || undefined } } })),
  });
  const free = (courses.data?.items ?? [])
    .filter((c) => !c.project_id && c.subject_code)
    .sort((a, b) => Number(b.school === school || b.scope === 'national') - Number(a.school === school || a.scope === 'national'));
  const create = useMutation({
    mutationFn: (v: { course: number }) => unwrap(api.POST('/admin/official/projects', { body: { catalog_course_id: v.course } })),
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
      <Typography.Paragraph type="secondary">选一条考试科目，院校、考试代码、科目名称都取自它；一门考试科目只能立一个项目，全国统考所有院校共用。</Typography.Paragraph>
      <Form form={form} layout="vertical" onFinish={(v) => create.mutate(v)}>
        <Form.Item name="course" label="考试科目" rules={[{ required: true, message: '选考试科目' }]}
          extra={<>库里没有？先去 <Typography.Link onClick={() => nav('/catalog')}>考试科目库</Typography.Link> 新增（要填考试代码）</>}>
          <Select
            showSearch
            filterOption={false}
            onSearch={setQ}
            loading={courses.isPending}
            placeholder="按院校、科目名或代码搜索"
            options={free.map((c) => ({ value: c.id, label: courseLabel(c) }))}
            notFoundContent={courses.isPending ? '加载中' : '没有可立项的考试科目'}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
