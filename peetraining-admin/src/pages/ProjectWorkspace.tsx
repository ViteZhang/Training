// T54 项目工作台：官方题库点进一个项目，按顺序做完 ① 关联资料 ② 知识框架 ③ 导入真题与核对 ④ 手动录题 ⑤ 审核与发布。
// 原来分散的 7.11 项目抽屉、7.17 AI 解析导入、7.12 内容生产都并到这一页；每步在步骤条上显示状态。
// 内容编辑只能进分配给自己的项目，不能改项目设置、关联资料、新建资料，也看不到审核与发布的操作。
import { ApiError } from '@/api-client';
import type { Schemas } from '@/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Descriptions, Empty, List, Space, Steps, Tag, Typography } from 'antd';
import { useState } from 'react';
import { Link, useParams } from 'react-router';
import { Loadable, PageTitle } from '../components/Page';
import { stageNames } from '../components/ProjectPicker';
import { api, unwrap } from '../lib/api';
import type { Role } from '../lib/menu';
import { canManage, ProjectSettings } from './Official';
import { ImportSection, UploadMaterial } from './OfficialImport';
import { ProduceSection } from './Produce';
import { ProjectTextbooks } from './ProjectTextbooks';
import { BookDetail, BookForm, kindName } from './Textbooks';

type ImportFile = Schemas['OfficialImportFile'];
type Link_ = Schemas['UserTextbookMatch'];

export const workspaceSteps = ['materials', 'framework', 'import', 'produce', 'release'] as const;
export type WorkspaceStep = (typeof workspaceSteps)[number];

/** 还没核对的条数：已解析的资料里题目、知识点减去已核对的。 */
export function pendingReview(files: ImportFile[]) {
  return files.filter((f) => f.status === 'parsed').reduce((n, f) => n + Math.max(0, f.question_count + f.kp_count - f.reviewed_count), 0);
}

/** 路由入口：/official/:projectId。 */
export function ProjectWorkspaceRoute({ roles }: { roles: Role[] }) {
  const { projectId } = useParams();
  const id = Number(projectId);
  if (!Number.isInteger(id) || id <= 0) return <Empty description="项目不存在" />;
  return <ProjectWorkspace key={id} projectId={id} roles={roles} />;
}

export function ProjectWorkspace({ projectId, roles, initialStep = 'materials' }: { projectId: number; roles: Role[]; initialStep?: WorkspaceStep }) {
  const manage = canManage(roles);
  const [step, setStep] = useState<WorkspaceStep>(initialStep);
  const [fileId, setFileId] = useState<number>();
  const [settings, setSettings] = useState(false);
  const path = { params: { path: { projectId } } };
  const project = useQuery({ queryKey: ['admin', 'official', 'project', projectId], queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}', path)) });
  const links = useQuery({ queryKey: ['admin', 'project-textbooks', projectId], queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/textbooks', path)) });
  const files = useQuery({ queryKey: ['admin', 'official-import', projectId], queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/imports', path)) });
  const p = project.data;
  const linked = links.data?.items ?? [];
  const usable = linked.filter((l) => l.available);
  const list = files.data?.items ?? [];
  const exams = list.filter((f) => f.kind === 'exam' || f.kind === 'table');
  const pending = pendingReview(list);
  const openFile = (id: number) => {
    setFileId(id);
    setStep('import');
  };
  const items: { key: WorkspaceStep; title: string; description: string; done: boolean }[] = [
    { key: 'materials', title: '关联资料', description: usable.length ? `已关联 ${linked.length} 份` : linked.length ? '关联的资料还不能检索' : '还没关联，未完成', done: usable.length > 0 },
    { key: 'framework', title: '知识框架', description: p?.kp_count ? `知识点 ${p.kp_count} 个` : '还没有知识点', done: !!p?.kp_count },
    { key: 'import', title: '导入真题与核对', description: pending ? `待核对 ${pending} 条` : exams.length ? `${exams.length} 份资料，已核对完` : '还没导入真题', done: exams.length > 0 && pending === 0 },
    { key: 'produce', title: '手动录题', description: '可选：补录、修改题目与知识点', done: false },
    { key: 'release', title: '审核与发布', description: p?.version ? `当前版本 ${p.version}` : '未发布', done: !!p?.version },
  ];
  const current = workspaceSteps.indexOf(step);
  return (
    <Loadable loading={project.isPending} error={project.error}>
      {p ? (
        <>
          <PageTitle
            code="7.11"
            title={`${p.school} ${p.subject_code} ${p.subject_name}`}
            extra={
              <Space>
                <Tag>{stageNames[p.stage]}</Tag>
                <Link to="/official">返回项目列表</Link>
                {manage ? <Button onClick={() => setSettings(true)}>项目设置</Button> : null}
              </Space>
            }
          />
          <Steps
            type="navigation"
            size="small"
            current={current}
            onChange={(i) => setStep(workspaceSteps[i]!)}
            style={{ marginBottom: 24 }}
            items={items.map((it, i) => ({ title: it.title, description: it.description, status: i === current ? 'process' : it.done ? 'finish' : 'wait' }))}
          />
          {step === 'materials' ? <MaterialsStep projectId={projectId} manage={manage} linked={linked} /> : null}
          {step === 'framework' ? <FrameworkStep projectId={projectId} linked={linked} files={list} onOpenFile={openFile} /> : null}
          {step === 'import' ? <ImportSection projectId={projectId} fileId={fileId} onFile={setFileId} /> : null}
          {step === 'produce' ? <ProduceSection project={projectId} /> : null}
          {step === 'release' ? <ReleaseStep projectId={projectId} manage={manage} version={p.version} subscribers={p.subscribers} /> : null}
          {settings ? <ProjectSettings id={projectId} onClose={() => setSettings(false)} /> : null}
        </>
      ) : null}
    </Loadable>
  );
}

/** ① 关联资料：内容负责人可以关联已有资料，或在这里直接新建资料（导入、校对、建索引、发布后再关联）。 */
function MaterialsStep({ projectId, manage, linked }: { projectId: number; manage: boolean; linked: Link_[] }) {
  const [creating, setCreating] = useState(false);
  const [bookId, setBookId] = useState<number>();
  return (
    <>
      {linked.length === 0 ? (
        <Alert
          type="warning"
          showIcon
          style={{ marginBottom: 16 }}
          message="这个项目还没有关联资料"
          description="AI 补参考答案时会优先引用这里关联的教材、讲义，围绕考纲的要求作答、参考经验总结的答题结构；不关联的话只能按通用知识生成。"
        />
      ) : null}
      <ProjectTextbooks projectId={projectId} canEdit={manage} extra={<Button onClick={() => setCreating(true)}>新建资料</Button>} />
      {manage ? (
        <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>
          资料库里没有的，点「新建资料」：填好信息后导入原文、校对、建索引并发布，发布后点「关联已有资料」选上。
        </Typography.Paragraph>
      ) : (
        <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>关联资料由内容负责人维护。</Typography.Paragraph>
      )}
      {creating ? <BookForm onClose={() => setCreating(false)} onDone={(b) => { setCreating(false); setBookId(b.id); }} /> : null}
      {bookId ? <BookDetail bookId={bookId} onClose={() => setBookId(undefined)} /> : null}
    </>
  );
}

/** ② 知识框架：按已关联的考纲（全文）或教材、讲义（章节目录）一键生成；资料库里没有的可以上传参考书目录。 */
function FrameworkStep({ projectId, linked, files, onOpenFile }: { projectId: number; linked: Link_[]; files: ImportFile[]; onOpenFile: (id: number) => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [uploading, setUploading] = useState(false);
  const sources = linked.filter((l) => l.kind !== 'experience');
  const generate = useMutation({
    mutationFn: (editionId: number) => unwrap(api.POST('/admin/official/projects/{projectId}/framework', { params: { path: { projectId } }, body: { edition_id: editionId } })),
    onSuccess: (f) => {
      message.success('已开始生成知识框架，生成好后在核对区逐条接受');
      void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] });
      onOpenFile(f.id);
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '生成失败，请稍后再试'),
  });
  const frameworks = files.filter((f) => f.kind === 'catalog');
  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card size="small" title="从已关联的资料生成" extra={<Button onClick={() => setUploading(true)}>上传参考书目录</Button>}>
        <List
          dataSource={sources}
          locale={{ emptyText: '还没有关联考纲、教材或讲义；先在「关联资料」一步关联，或上传参考书目录' }}
          renderItem={(l) => (
            <List.Item
              actions={[
                <Button key="gen" type="primary" disabled={!l.available} loading={generate.isPending && generate.variables === l.edition_id} onClick={() => generate.mutate(l.edition_id)}>
                  生成知识框架
                </Button>,
              ]}
            >
              <List.Item.Meta
                title={<Space><Tag>{kindName(l.kind)}</Tag>{l.title}</Space>}
                description={`${l.author} · ${l.edition} · ${l.kind === 'syllabus' ? '按考纲全文生成' : '按章节目录生成'}${l.available ? '' : '（还不能用：先在资料库发布）'}`}
              />
            </List.Item>
          )}
        />
      </Card>
      <Card size="small" title="已生成的知识框架">
        <List
          dataSource={frameworks}
          locale={{ emptyText: '还没有' }}
          renderItem={(f) => (
            <List.Item actions={f.status === 'parsed' ? [<Button key="open" onClick={() => onOpenFile(f.id)}>去核对</Button>] : []}>
              <List.Item.Meta
                title={f.file_name}
                description={f.status === 'parsed' ? `知识点 ${f.kp_count} 个 · 已核对 ${f.reviewed_count}` : f.status === 'failed' ? f.fail_reason ?? '解析失败' : '生成中…'}
              />
            </List.Item>
          )}
        />
      </Card>
      <UploadMaterial projectId={projectId} open={uploading} onClose={() => setUploading(false)} kinds={['catalog']} />
    </Space>
  );
}

/** ⑤ 审核与发布：本项目待审核与可发布的数量，内容负责人点进审核队列、版本发布。 */
function ReleaseStep({ projectId, manage, version, subscribers }: { projectId: number; manage: boolean; version?: string; subscribers: number }) {
  const reviews = useQuery({
    queryKey: ['admin', 'official', 'reviews', projectId],
    queryFn: () => unwrap(api.GET('/admin/official/reviews', { params: { query: { project_id: projectId } } })),
    enabled: manage,
  });
  const preview = useQuery({
    queryKey: ['admin', 'official', 'preview', projectId],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/release-preview', { params: { path: { projectId } } })),
    enabled: manage,
  });
  return (
    <Card size="small">
      <Descriptions column={2} size="small" style={{ marginBottom: 16 }}>
        <Descriptions.Item label="当前版本">{version || '未发布'}</Descriptions.Item>
        <Descriptions.Item label="已添加用户">{subscribers}</Descriptions.Item>
        {manage ? <Descriptions.Item label="待审核">{reviews.data ? `${reviews.data.items.length} 条` : '…'}</Descriptions.Item> : null}
        {manage ? <Descriptions.Item label="待发布">{preview.data ? `${preview.data.changes.length} 处变更（下个版本 ${preview.data.next_version}）` : '…'}</Descriptions.Item> : null}
      </Descriptions>
      {manage ? (
        <Space>
          <Link to={`/review?project=${projectId}`}><Button type="primary">去审核</Button></Link>
          <Link to={`/release?project=${projectId}`}><Button>去发布</Button></Link>
        </Space>
      ) : (
        <Typography.Text type="secondary">核对接受、录题提交的内容由内容负责人审核、发布。</Typography.Text>
      )}
    </Card>
  );
}
