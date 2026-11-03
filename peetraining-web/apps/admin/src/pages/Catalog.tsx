// 院校专业课库（PRD v3 增补「院校专业课库」，配置组）：App 1.1 的院校搜索与专业课列表都来自这里，由运营手工维护，不接外部数据。
// 维护字段：院校全称、专业、学科、专业课名称、代码（选填）、参考书目（多本）、考试年份；官方题库状态（有 / 无 / 生产中）按立项自动显示。
// 用户在 1.1 手动添加的课汇总成「待入库」，运营核对后一键入库或合并到已有课程。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Form, Input, InputNumber, Modal, Select, Space, Switch, Table, Tabs, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { api, unwrap } from '../lib/api';

type Course = Schemas['AdminCatalogCourse'];
type CourseInput = Schemas['CatalogCourseInput'];

export const officialStatus: Record<Course['official_status'], { text: string; color?: string }> = {
  available: { text: '有官方题库', color: 'green' },
  producing: { text: '生产中', color: 'gold' },
  none: { text: '无' },
};

/** 参考书目：一行一本。 */
export const splitBooks = (s?: string) => (s ?? '').split('\n').map((b) => b.trim()).filter(Boolean);

export function Catalog() {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [q, setQ] = useState('');
  const [editing, setEditing] = useState<Course | 'new'>();
  const [resolving, setResolving] = useState<{ id: number; action: 'add' | 'merge' }>();
  const courses = useQuery({ queryKey: ['admin', 'catalog', q], queryFn: () => unwrap(api.GET('/admin/catalog/courses', { params: { query: { q: q || undefined } } })) });
  const pending = useQuery({ queryKey: ['admin', 'catalog', 'pending'], queryFn: () => unwrap(api.GET('/admin/catalog/pending')) });
  const refresh = () => void qc.invalidateQueries({ queryKey: ['admin', 'catalog'] });
  const fail = (e: unknown) => message.error(e instanceof ApiError ? e.message : '操作失败');
  const save = useMutation({
    mutationFn: async (v: CourseInput) => {
      if (editing && editing !== 'new') await unwrap(api.PUT('/admin/catalog/courses/{courseId}', { params: { path: { courseId: editing.id } }, body: v }));
      else await unwrap(api.POST('/admin/catalog/courses', { body: v }));
    },
    onSuccess: () => {
      message.success('已保存');
      setEditing(undefined);
      refresh();
    },
    onError: fail,
  });
  const resolve = useMutation({
    mutationFn: (v: { id: number; action: 'add' | 'merge' | 'ignore'; target_course_id?: number; course?: CourseInput }) =>
      unwrap(api.POST('/admin/catalog/pending/{pendingId}/resolve', { params: { path: { pendingId: v.id } }, body: { action: v.action, target_course_id: v.target_course_id, course: v.course } })),
    onSuccess: () => {
      message.success('已处理');
      setResolving(undefined);
      refresh();
    },
    onError: fail,
  });
  const all = courses.data?.items ?? [];
  const pendingItems = pending.data?.items ?? [];
  return (
    <>
      <PageTitle code="7.8b" title="院校专业课库" extra={<Button type="primary" onClick={() => setEditing('new')}>+ 新增课程</Button>} />
      <Typography.Paragraph type="secondary">App 1.1 的院校搜索和专业课列表都来自这里。第一批：四川大学哲学 662、812，以及种子用户报考的其他院校专业。</Typography.Paragraph>
      <Tabs
        items={[
          {
            key: 'courses',
            label: `课程（${all.length}）`,
            children: (
              <>
                <Input.Search allowClear placeholder="按院校、课程名或代码搜索" style={{ width: 320, marginBottom: 12 }} onSearch={setQ} />
                <Loadable loading={courses.isPending} error={courses.error}>
                  <Table
                    rowKey="id"
                    size="small"
                    dataSource={all}
                    pagination={{ pageSize: 50 }}
                    columns={[
                      { title: '院校', dataIndex: 'school' },
                      { title: '专业', dataIndex: 'major' },
                      { title: '学科', dataIndex: 'discipline' },
                      { title: '专业课', render: (_, c) => `${c.subject_code ? `${c.subject_code} ` : ''}${c.subject_name}` },
                      { title: '参考书目', render: (_, c) => c.textbooks.join('；') },
                      { title: '考试年份', render: (_, c) => c.exam_year ?? '各年' },
                      { title: '官方题库', render: (_, c) => <Tag color={officialStatus[c.official_status].color}>{officialStatus[c.official_status].text}</Tag> },
                      { title: '1.1 选了的人', dataIndex: 'chosen_count' },
                      { title: '状态', render: (_, c) => (c.status === 'hidden' ? <Tag>已隐藏</Tag> : '显示') },
                      { title: '操作', render: (_, c) => <Button size="small" onClick={() => setEditing(c)}>编辑</Button> },
                    ]}
                  />
                </Loadable>
              </>
            ),
          },
          {
            key: 'pending',
            label: `待入库（${pendingItems.length}）`,
            children: (
              <Loadable loading={pending.isPending} error={pending.error}>
                <Typography.Paragraph type="secondary">用户在 1.1 手动添加、库里没有的课，按「院校 + 代码（或课程名）」归并，只有人数。</Typography.Paragraph>
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={pendingItems}
                  columns={[
                    { title: '院校', dataIndex: 'school' },
                    { title: '专业', dataIndex: 'major' },
                    { title: '专业课', render: (_, p) => `${p.subject_code ? `${p.subject_code} ` : ''}${p.subject_name}` },
                    { title: '人数', dataIndex: 'request_count' },
                    { title: '最近添加', render: (_, p) => new Date(p.last_at).toLocaleString('zh-CN') },
                    {
                      title: '操作',
                      render: (_, p) => (
                        <Space>
                          <Button size="small" type="primary" onClick={() => setResolving({ id: p.id, action: 'add' })}>一键入库</Button>
                          <Button size="small" onClick={() => setResolving({ id: p.id, action: 'merge' })}>合并到已有课程</Button>
                          <Button size="small" onClick={() => resolve.mutate({ id: p.id, action: 'ignore' })}>忽略</Button>
                        </Space>
                      ),
                    },
                  ]}
                />
              </Loadable>
            ),
          },
        ]}
      />
      <CourseEditor course={editing} loading={save.isPending} onCancel={() => setEditing(undefined)} onSave={(v) => save.mutate(v)} />
      <Modal open={!!resolving} title={resolving?.action === 'add' ? '一键入库' : '合并到已有课程'} onCancel={() => setResolving(undefined)} footer={null} destroyOnClose>
        {resolving?.action === 'merge' ? (
          <Form layout="vertical" onFinish={(v: { target: number }) => resolve.mutate({ id: resolving.id, action: 'merge', target_course_id: v.target })}>
            <Form.Item name="target" label="合并到" rules={[{ required: true }]}>
              <Select showSearch optionFilterProp="label" options={all.map((c) => ({ value: c.id, label: `${c.school} · ${c.major} · ${c.subject_code ?? ''} ${c.subject_name}` }))} />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={resolve.isPending}>合并</Button>
          </Form>
        ) : resolving ? (
          <Form
            layout="vertical"
            onFinish={(v: { discipline?: string; books?: string }) =>
              resolve.mutate({ id: resolving.id, action: 'add', course: { school: '', major: '', subject_name: '', discipline: v.discipline, textbooks: splitBooks(v.books) } })
            }
          >
            <Typography.Paragraph type="secondary">院校、专业、课程名与代码按用户填写的入库，可以之后再改。</Typography.Paragraph>
            <Form.Item name="discipline" label="学科"><Input placeholder="如 哲学" maxLength={16} /></Form.Item>
            <Form.Item name="books" label="参考书目（一行一本）"><Input.TextArea rows={3} /></Form.Item>
            <Button type="primary" htmlType="submit" loading={resolve.isPending}>入库</Button>
          </Form>
        ) : null}
      </Modal>
    </>
  );
}

function CourseEditor({ course, loading, onCancel, onSave }: { course?: Course | 'new'; loading: boolean; onCancel: () => void; onSave: (v: CourseInput) => void }) {
  const init = course && course !== 'new' ? { ...course, books: course.textbooks.join('\n'), hidden: course.status === 'hidden' } : {};
  return (
    <Modal open={!!course} title={course === 'new' ? '新增课程' : '编辑课程'} onCancel={onCancel} footer={null} destroyOnClose>
      <Form
        layout="vertical"
        initialValues={init}
        onFinish={(v: CourseInput & { books?: string }) => {
          const { books, ...rest } = v;
          onSave({ ...rest, textbooks: splitBooks(books) });
        }}
      >
        <Form.Item name="school" label="院校全称" rules={[{ required: true }]}><Input maxLength={64} /></Form.Item>
        <Space>
          <Form.Item name="major" label="专业" rules={[{ required: true }]}><Input maxLength={64} /></Form.Item>
          <Form.Item name="discipline" label="学科"><Input maxLength={16} placeholder="如 哲学" /></Form.Item>
        </Space>
        <Space>
          <Form.Item name="subject_code" label="专业课代码"><Input maxLength={16} style={{ width: 120 }} /></Form.Item>
          <Form.Item name="subject_name" label="专业课名称" rules={[{ required: true }]}><Input maxLength={64} /></Form.Item>
        </Space>
        <Form.Item name="books" label="参考书目（一行一本）"><Input.TextArea rows={3} /></Form.Item>
        <Space>
          <Form.Item name="exam_year" label="考试年份（空表示各年通用）"><InputNumber min={2020} max={2100} /></Form.Item>
          {course !== 'new' ? <Form.Item name="hidden" label="在 1.1 隐藏" valuePropName="checked"><Switch /></Form.Item> : null}
        </Space>
        <div>
          <Button type="primary" htmlType="submit" loading={loading}>保存</Button>
        </div>
      </Form>
    </Modal>
  );
}
