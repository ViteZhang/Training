// 考试科目库（原「院校专业课库」，配置组；PRD v3 增补「考试科目库」T50）：App 1.1 的院校搜索与考试科目列表都来自这里，由运营手工维护，不接外部数据。
// 一套卷子是一条考试科目：命题方式（院校自命题 / 全国统考）、院校（统考不填）、学科（固定选项）、考试代码、科目名称、参考书目、适用年份；
// 专业只是选填的「适用专业」，不参与去重。全国统考全国一条，可登记「本校使用」的院校；每条直接「立项做官方题库」。
// 用户在 1.1 手动添加的课汇总成「待入库」，运营核对后一键入库、合并到已有科目，或合并到统考科目（同时登记这所院校）。
import type { Schemas } from '@/api-client';
import { ApiError } from '@/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { App as AntApp, Button, Form, Input, InputNumber, Modal, Radio, Select, Space, Switch, Table, Tabs, Tag, Typography } from 'antd';
import { useState } from 'react';
import { useNavigate } from 'react-router';
import { Loadable, PageTitle } from '../components/Page';
import { api, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';
import { dt } from '../lib/time';

type Course = Schemas['AdminCatalogCourse'];
type CourseInput = Schemas['CatalogCourseInput'];
type Scope = NonNullable<CourseInput['scope']>;

export const officialStatus: Record<Course['official_status'], { text: string; color?: string }> = {
  available: { text: '有官方题库', color: 'green' },
  producing: { text: '生产中', color: 'gold' },
  none: { text: '无' },
};

export const scopeNames: Record<Scope, string> = { school: '院校自命题', national: '全国统考' };

/** 参考书目：一行一本。 */
export const splitBooks = (s?: string) => (s ?? '').split('\n').map((b) => b.trim()).filter(Boolean);

/** 下拉里显示的一条考试科目：「四川大学 · 812 西方哲学史」「全国统考 · 333 教育综合」。 */
export const courseLabel = (c: Pick<Course, 'scope' | 'school' | 'subject_code' | 'subject_name' | 'exam_year'>) =>
  `${c.scope === 'national' ? '全国统考' : c.school} · ${c.subject_code ? `${c.subject_code} ` : ''}${c.subject_name}${c.exam_year ? `（${c.exam_year}）` : ''}`;

export function useDisciplines() {
  return useQuery({ queryKey: ['admin', 'catalog', 'disciplines'], queryFn: () => unwrap(api.GET('/admin/catalog/disciplines')), staleTime: Infinity });
}

export function Catalog({ roles = [] }: { roles?: Role[] }) {
  const qc = useQueryClient();
  const nav = useNavigate();
  const { message } = AntApp.useApp();
  const canCreate = roles.includes('admin') || roles.includes('content_lead');
  const [q, setQ] = useState('');
  const [scope, setScope] = useState<Scope>();
  const [discipline, setDiscipline] = useState<string>();
  const [editing, setEditing] = useState<Course | 'new'>();
  const [schoolsOf, setSchoolsOf] = useState<Course>();
  const [resolving, setResolving] = useState<{ id: number; action: 'add' | 'merge' }>();
  const disciplines = useDisciplines();
  const courses = useQuery({
    queryKey: ['admin', 'catalog', q, scope, discipline],
    queryFn: () => unwrap(api.GET('/admin/catalog/courses', { params: { query: { q: q || undefined, scope, discipline } } })),
  });
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
  // 从考试科目立项：院校、代码、名称取自考试科目，一门只能立一个项目（T50）。
  const create = useMutation({
    mutationFn: (c: Course) => unwrap(api.POST('/admin/official/projects', { body: { catalog_course_id: c.id } })),
    onSuccess: () => {
      message.success('已立项，接下来分配编辑、登记授权资料');
      void qc.invalidateQueries({ queryKey: ['admin'] });
      nav('/official');
    },
    onError: fail,
  });
  const all = courses.data?.items ?? [];
  const pendingItems = pending.data?.items ?? [];
  const disciplineOptions = (disciplines.data?.items ?? []).map((d) => ({ value: d, label: d }));
  return (
    <>
      <PageTitle code="7.8b" title="考试科目库" extra={<Button type="primary" onClick={() => setEditing('new')}>+ 新增考试科目</Button>} />
      <Typography.Paragraph type="secondary">
        App 1.1 的院校搜索和考试科目列表都来自这里。一套卷子录一条：院校 + 学科 + 考试代码 + 科目名称；很多专业共用同一套卷子，专业只是选填的「适用专业」。
        全国统考（如 311、333）全国只录一条，所有院校的考生都能选到。
      </Typography.Paragraph>
      <Tabs
        items={[
          {
            key: 'courses',
            label: `考试科目（${all.length}）`,
            children: (
              <>
                <Space wrap style={{ marginBottom: 12 }}>
                  <Input.Search allowClear placeholder="按院校、科目名或代码搜索" style={{ width: 280 }} onSearch={setQ} />
                  <Select allowClear placeholder="命题方式" style={{ width: 140 }} value={scope} onChange={setScope}
                    options={(Object.keys(scopeNames) as Scope[]).map((k) => ({ value: k, label: scopeNames[k] }))} />
                  <Select allowClear placeholder="学科" style={{ width: 140 }} value={discipline} onChange={setDiscipline} options={disciplineOptions} />
                </Space>
                <Loadable loading={courses.isPending} error={courses.error}>
                  <Table
                    rowKey="id"
                    size="small"
                    dataSource={all}
                    pagination={{ pageSize: 50 }}
                    columns={[
                      { title: '命题方式', render: (_, c) => (c.scope === 'national' ? <Tag color="blue">全国统考</Tag> : '自命题') },
                      {
                        title: '院校',
                        render: (_, c) =>
                          c.scope === 'national' ? <Typography.Text type="secondary">已登记 {c.school_count} 所</Typography.Text> : c.school,
                      },
                      { title: '学科', dataIndex: 'discipline' },
                      { title: '考试科目', render: (_, c) => `${c.subject_code ? `${c.subject_code} ` : ''}${c.subject_name}` },
                      { title: '适用专业', render: (_, c) => c.majors.join('、') || '—' },
                      { title: '参考书目', render: (_, c) => c.textbooks.join('；') },
                      { title: '年份', render: (_, c) => c.exam_year ?? '各年' },
                      { title: '官方题库', render: (_, c) => <Tag color={officialStatus[c.official_status].color}>{officialStatus[c.official_status].text}</Tag> },
                      { title: '1.1 选了的人', dataIndex: 'chosen_count' },
                      { title: '状态', render: (_, c) => (c.status === 'hidden' ? <Tag>已隐藏</Tag> : '显示') },
                      {
                        title: '操作',
                        render: (_, c) => (
                          <Space size={4}>
                            <Button size="small" onClick={() => setEditing(c)}>编辑</Button>
                            {c.scope === 'national' ? <Button size="small" onClick={() => setSchoolsOf(c)}>登记院校</Button> : null}
                            {c.project_id ? (
                              <Button size="small" onClick={() => nav('/official')}>查看项目</Button>
                            ) : canCreate ? (
                              <Button size="small" type="primary" ghost loading={create.isPending && create.variables?.id === c.id}
                                onClick={() => Modal.confirm({ title: `立项做官方题库：${courseLabel(c)}`, content: '院校、考试代码、科目名称取自这条考试科目；一门考试科目只能立一个项目。', okText: '立项', onOk: () => create.mutateAsync(c) })}>
                                立项
                              </Button>
                            ) : null}
                          </Space>
                        ),
                      },
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
                <Typography.Paragraph type="secondary">用户在 1.1 手动添加、库里没有的课，按「院校 + 代码（或课程名）」归并，只有人数。是全国统考的，合并到统考科目即可（同时登记这所院校）。</Typography.Paragraph>
                <Table
                  rowKey="id"
                  size="small"
                  dataSource={pendingItems}
                  columns={[
                    { title: '院校', dataIndex: 'school' },
                    { title: '专业', render: (_, p) => p.major || '—' },
                    { title: '专业课', render: (_, p) => `${p.subject_code ? `${p.subject_code} ` : ''}${p.subject_name}` },
                    { title: '人数', dataIndex: 'request_count' },
                    { title: '最近添加', render: (_, p) => dt(p.last_at) },
                    {
                      title: '操作',
                      render: (_, p) => (
                        <Space>
                          <Button size="small" type="primary" onClick={() => setResolving({ id: p.id, action: 'add' })}>一键入库</Button>
                          <Button size="small" onClick={() => setResolving({ id: p.id, action: 'merge' })}>合并到已有科目</Button>
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
      <CourseEditor course={editing} disciplines={disciplineOptions} loading={save.isPending} onCancel={() => setEditing(undefined)} onSave={(v) => save.mutate(v)} />
      {schoolsOf ? <SchoolsEditor course={schoolsOf} onClose={() => setSchoolsOf(undefined)} onSaved={refresh} /> : null}
      <Modal open={!!resolving} title={resolving?.action === 'add' ? '一键入库' : '合并到已有科目'} onCancel={() => setResolving(undefined)} footer={null} destroyOnHidden>
        {resolving?.action === 'merge' ? (
          <Form layout="vertical" onFinish={(v: { target: number }) => resolve.mutate({ id: resolving.id, action: 'merge', target_course_id: v.target })}>
            <Form.Item name="target" label="合并到" rules={[{ required: true }]} extra="合并到全国统考科目时，这所院校登记为使用该统考">
              <Select showSearch optionFilterProp="label" options={all.map((c) => ({ value: c.id, label: courseLabel(c) }))} />
            </Form.Item>
            <Button type="primary" htmlType="submit" loading={resolve.isPending}>合并</Button>
          </Form>
        ) : resolving ? (
          <Form
            layout="vertical"
            onFinish={(v: { discipline?: string; books?: string }) =>
              resolve.mutate({ id: resolving.id, action: 'add', course: { subject_name: '', discipline: v.discipline, textbooks: splitBooks(v.books) } })
            }
          >
            <Typography.Paragraph type="secondary">院校、课程名与代码按用户填写的入库（院校自命题），可以之后再改。</Typography.Paragraph>
            <Form.Item name="discipline" label="学科" rules={[{ required: true, message: '选学科' }]}><Select options={disciplineOptions} /></Form.Item>
            <Form.Item name="books" label="参考书目（一行一本）"><Input.TextArea rows={3} /></Form.Item>
            <Button type="primary" htmlType="submit" loading={resolve.isPending}>入库</Button>
          </Form>
        ) : null}
      </Modal>
    </>
  );
}

type EditorValues = Omit<CourseInput, 'textbooks'> & { books?: string };

function CourseEditor({ course, disciplines, loading, onCancel, onSave }: {
  course?: Course | 'new';
  disciplines: { value: string; label: string }[];
  loading: boolean;
  onCancel: () => void;
  onSave: (v: CourseInput) => void;
}) {
  const init: Partial<EditorValues> =
    course && course !== 'new'
      ? { scope: course.scope, school: course.school, discipline: course.discipline || undefined, subject_code: course.subject_code, subject_name: course.subject_name,
          majors: course.majors, exam_year: course.exam_year, books: course.textbooks.join('\n'), hidden: course.status === 'hidden' }
      : { scope: 'school' };
  return (
    <Modal open={!!course} title={course === 'new' ? '新增考试科目' : '编辑考试科目'} onCancel={onCancel} footer={null} destroyOnHidden>
      <Form
        layout="vertical"
        initialValues={init}
        onFinish={(v: EditorValues) => {
          const { books, ...rest } = v;
          onSave({ ...rest, school: rest.scope === 'national' ? undefined : rest.school, majors: rest.majors ?? [], textbooks: splitBooks(books) });
        }}
      >
        <Form.Item name="scope" label="命题方式" extra="代码不能用来判断是不是统考（333 也有院校自命题的），这里明确选">
          <Radio.Group options={[{ value: 'school', label: '院校自命题' }, { value: 'national', label: '全国统考' }]} />
        </Form.Item>
        <Form.Item noStyle shouldUpdate={(a, b) => a.scope !== b.scope}>
          {({ getFieldValue }) => {
            const national = getFieldValue('scope') === 'national';
            return (
              <>
                {national ? null : <Form.Item name="school" label="院校全称" rules={[{ required: true }]}><Input maxLength={64} /></Form.Item>}
                <Space align="start">
                  <Form.Item name="discipline" label="学科" rules={[{ required: true, message: '选学科' }]}><Select style={{ width: 140 }} options={disciplines} /></Form.Item>
                  <Form.Item name="subject_code" label="考试代码" rules={national ? [{ required: true, message: '统考要填代码' }] : []}><Input maxLength={16} style={{ width: 120 }} /></Form.Item>
                  <Form.Item name="subject_name" label="科目名称" rules={[{ required: true }]}><Input maxLength={64} /></Form.Item>
                </Space>
              </>
            );
          }}
        </Form.Item>
        <Form.Item name="majors" label="适用专业（选填，只用来展示和搜索）">
          <Select mode="tags" tokenSeparators={['、', ',', '，']} placeholder="输入后回车，如 文艺学" open={false} />
        </Form.Item>
        <Form.Item name="books" label="参考书目（一行一本）"><Input.TextArea rows={3} /></Form.Item>
        <Space>
          <Form.Item name="exam_year" label="适用年份（空表示各年通用）"><InputNumber min={2020} max={2100} /></Form.Item>
          {course !== 'new' ? <Form.Item name="hidden" label="在 1.1 隐藏" valuePropName="checked"><Switch /></Form.Item> : null}
        </Space>
        <div>
          <Button type="primary" htmlType="submit" loading={loading}>保存</Button>
        </div>
      </Form>
    </Modal>
  );
}

/** 登记使用这门全国统考的院校（选填）：登记后这所院校的搜索结果与科目数算上它；没登记的院校选到这个学科时也能看到。 */
function SchoolsEditor({ course, onClose, onSaved }: { course: Course; onClose: () => void; onSaved: () => void }) {
  const { message } = AntApp.useApp();
  const [value, setValue] = useState<string[]>();
  const list = useQuery({
    queryKey: ['admin', 'catalog', 'schools', course.id],
    queryFn: () => unwrap(api.GET('/admin/catalog/courses/{courseId}/schools', { params: { path: { courseId: course.id } } })),
  });
  const save = useMutation({
    mutationFn: (schools: string[]) => unwrap(api.PUT('/admin/catalog/courses/{courseId}/schools', { params: { path: { courseId: course.id } }, body: { schools } })),
    onSuccess: () => {
      message.success('已保存');
      onSaved();
      onClose();
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '保存失败'),
  });
  const current = value ?? list.data?.items ?? [];
  return (
    <Modal open title={`登记院校：${courseLabel(course)}`} okText="保存" onCancel={onClose} onOk={() => save.mutate(current)} confirmLoading={save.isPending}>
      <Typography.Paragraph type="secondary">选填。登记后这所院校在 1.1 的科目数算上这门统考，科目不多时直接列出；没登记的院校在学科下也能看到统考科目。</Typography.Paragraph>
      <Loadable loading={list.isPending} error={list.error}>
        <Select mode="tags" style={{ width: '100%' }} value={current} onChange={setValue} tokenSeparators={['、', ',', '，', '\n']} placeholder="输入院校全称后回车" open={false} />
      </Loadable>
    </Modal>
  );
}
