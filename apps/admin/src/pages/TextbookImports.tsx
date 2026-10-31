// 7.16 教材文件导入：私有直传 → 后台逐页识别 → 预览 → 明确采用到草稿。
import { ApiError, type components } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Drawer, Image, InputNumber, List, Modal, Radio, Space, Tag, Typography, Upload } from 'antd';
import { useRef, useState } from 'react';
import { Loadable } from '../components/Page';
import { api, dt, unwrap } from '../lib/api';
import { uploadTextbookFile } from '../lib/textbookUpload';

type ImportJob = components['schemas']['TextbookImport'];
type Path = { bookId: number; editionId: number; revisionId: number };
const root = '/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/imports' as const;
const names: Record<ImportJob['status'], string> = { uploading: '待上传', queued: '排队中', processing: '识别中', ready: '待导入草稿', failed: '识别失败', applied: '已导入草稿' };
const failure = (e: unknown) => e instanceof Error ? e.message : '操作失败，请重试';
const accept = '.pdf,.docx,.md,.txt';

export function TextbookImports({ path, onClose }: { path: Path; onClose: () => void }) {
  const qc = useQueryClient(); const { message, modal } = AntApp.useApp();
  const settings = useQuery({ queryKey: ['admin', 'textbook-import-settings'], queryFn: () => unwrap(api.GET('/admin/textbook-import-settings')) });
  const tasks = useQuery({ queryKey: ['admin', 'textbooks', 'imports', path], queryFn: () => unwrap(api.GET(root, { params: { path } })), refetchInterval: (q) => q.state.data?.items.some((j) => j.status === 'queued' || j.status === 'processing') ? 2500 : false });
  const revision = useQuery({ queryKey: ['admin', 'textbooks', path.bookId, path.editionId, path.revisionId], queryFn: () => unwrap(api.GET('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}', { params: { path } })) });
  const [adoptVersion, setAdoptVersion] = useState<number>();
  const [preview, setPreview] = useState<ImportJob>(); const [adopting, setAdopting] = useState<ImportJob>(); const [mode, setMode] = useState<'append' | 'replace'>('append');
  const keys = useRef(new Map<string, string>());
  const requestKey = (scope: string) => { let k = keys.current.get(scope); if (!k) { k = crypto.randomUUID(); keys.current.set(scope, k); } return k; };
  const invalidate = () => void qc.invalidateQueries({ queryKey: ['admin', 'textbooks'] });
  const upload = useMutation({ mutationFn: async ({ file, job }: { file: File; job?: ImportJob }) => {
    if (!settings.data) throw new Error('正在读取服务端限制，请稍后重试');
    if (file.size < 1 || file.size > settings.data.max_file_mb * 1024 * 1024) throw new Error(`单个文件需为 1 字节至 ${settings.data.max_file_mb} MB`);
    const format = file.name.split('.').at(-1)?.toLowerCase(); if (!['pdf', 'docx', 'md', 'txt'].includes(format ?? '')) throw new Error('仅支持 PDF、DOCX、Markdown、TXT');
    const data = await file.arrayBuffer(); const digest = await crypto.subtle.digest('SHA-256', data); const sha256 = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
    const target = job
      ? await unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/imports/{importId}/upload', { params: { path: { ...path, importId: job.id } } }))
      : await unwrap(api.POST(root, { params: { path }, body: { file_name: file.name, format: format as 'pdf' | 'docx' | 'md' | 'txt', size: file.size, sha256, idempotency_key: requestKey(`upload:${file.name}:${sha256}`) } }));
    // OSS 直传地址由服务端签名，不能附加后台会话令牌。
    if (target.job.status === 'uploading' || target.job.status === 'failed') {
      await uploadTextbookFile(file, target);
    }
    const actionPath = { ...path, importId: target.job.id };
    return target.job.status === 'failed'
      ? unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/imports/{importId}/retry', { params: { path: actionPath }, body: { idempotency_key: requestKey(`retry:${target.job.id}`) } }))
      : unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/imports/{importId}/commit', { params: { path: actionPath }, body: { idempotency_key: requestKey(`commit:${target.job.id}`) } }));
  }, onSuccess: (j) => { invalidate(); message.success('已提交后台处理，可以关闭此窗口'); keys.current.delete(`retry:${j.id}`); }, onError: (e) => { invalidate(); message.error(failure(e)); } });
  const retry = useMutation({ mutationFn: (job: ImportJob) => unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/imports/{importId}/retry', { params: { path: { ...path, importId: job.id } }, body: { idempotency_key: requestKey(`retry:${job.id}`) } })), onSuccess: (j) => { keys.current.delete(`retry:${j.id}`); invalidate(); }, onError: (e) => message.error(failure(e)) });
  const apply = useMutation({ mutationFn: async () => {
    if (!adopting || !adoptVersion) throw new Error('请先读取草稿');
    return unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/imports/{importId}/apply', { params: { path: { ...path, importId: adopting.id } }, body: { expected_version: adoptVersion, mode } }));
  }, onSuccess: () => { setAdopting(undefined); invalidate(); message.success('已导入草稿，请核对原文后确认校对'); } });
  const choose = (job?: ImportJob) => <Upload key={job?.id ?? 'new'} accept={accept} showUploadList={false} beforeUpload={(file) => { upload.mutate({ file, job }); return false; }} disabled={upload.isPending || !settings.data || revision.data?.status === 'published'}><Button type={job ? 'default' : 'primary'} loading={upload.isPending} disabled={!settings.data || revision.data?.status === 'published'}>{job ? '重新上传原件' : '选择文件并导入'}</Button></Upload>;
  const close = () => { if (upload.isPending) modal.confirm({ title: '文件正在上传，关闭窗口？', content: '建议等待上传完成。未完成的上传可在任务列表重新选择同一原件继续。', onOk: onClose }); else onClose(); };
  return <Drawer open width={860} title="教材文件导入" onClose={close} maskClosable={false}>
    <Loadable loading={settings.isPending} error={settings.error}>{settings.data && <Typography.Paragraph type="secondary">支持 PDF、DOCX、Markdown、TXT。单文件最多 {settings.data.max_file_mb} MB / {settings.data.max_pages} 页，本修订最多 {settings.data.max_imports_per_revision} 个文件。所有 PDF 页均由视觉模型识别；全局并发 {settings.data.max_concurrent_imports} 个任务，每日 PDF 页处理上限 {settings.data.max_daily_pdf_pages} 次。Word 和文本的单元序号不代表纸质页码。</Typography.Paragraph>}</Loadable>
    {choose()}
    {upload.isError && <Alert type="error" title={failure(upload.error)} style={{ marginTop: 16 }} />}
    <Alert type="info" title="识别完成后需要明确导入草稿，不会自动覆盖当前原文。" description="不确定的原文会保留待核对标记，校对时逐项确认。教材不会因识别完成而自动发布。" style={{ margin: '20px 0' }} />
    <Loadable loading={tasks.isPending} error={tasks.error}>
      <List dataSource={tasks.data?.items ?? []} locale={{ emptyText: '还没有导入任务' }} renderItem={(job) => <List.Item actions={[
        ...(job.completed_pages > 0 ? [<Button key="preview" onClick={() => setPreview(job)}>预览</Button>] : []),
        ...(job.status === 'uploading' ? [choose(job)] : []),
        ...(job.status === 'failed' ? [<Button key="retry" loading={retry.isPending} disabled={retry.isPending} onClick={() => retry.mutate(job)}>继续识别</Button>, ...(job.can_reupload ? [choose(job)] : [])] : []),
        ...(job.status === 'ready' ? [<Button key="apply" type="primary" disabled={!revision.data || revision.isError} onClick={() => { setMode('append'); setAdoptVersion(revision.data?.version); setAdopting(job); apply.reset(); }}>导入草稿</Button>] : []),
      ]}><List.Item.Meta title={<Space wrap>{job.file_name}<Tag>{names[job.status]}</Tag></Space>} description={<><span>已完成 {job.completed_pages} / {job.page_count || '待确认'} {job.format === 'pdf' ? '页' : '个文本单元'} · 待核对 {job.uncertain_pages} · {dt(job.updated_at)}</span>{job.failure_message && <div><Typography.Text type="danger">{job.failure_message}</Typography.Text></div>}</>} /></List.Item>} />
    </Loadable>
    {revision.isError && <Alert type="error" title="无法读取草稿版本，暂不能采用识别结果" />}
    {preview && <ImportPreview path={path} job={preview} onClose={() => setPreview(undefined)} />}
    <Modal open={!!adopting} title="将识别结果导入草稿" okText="确认导入" confirmLoading={apply.isPending} onOk={() => apply.mutate()} onCancel={() => { if (!apply.isPending) setAdopting(undefined); }}>
      <Typography.Paragraph>{adopting?.file_name}。导入后状态为待校对；已有线上版本保持不变。</Typography.Paragraph>
      <Radio.Group value={mode} onChange={(e) => setMode(e.target.value as 'append' | 'replace')} options={[{ value: 'append', label: '追加到草稿末尾' }, { value: 'replace', label: '替换当前草稿全部原文' }]} />
      {mode === 'replace' && <Alert type="warning" title="将替换草稿中现有的全部原文，请确认已保存需要保留的内容。" style={{ marginTop: 16 }} />}
      {apply.isError && <Alert type="error" title={failure(apply.error)} description={apply.error instanceof ApiError && apply.error.status === 409 ? '草稿可能已变化。关闭本窗口，重新打开任务后核对最新草稿再采用。' : undefined} style={{ marginTop: 16 }} />}
    </Modal>
  </Drawer>;
}
function ImportPreview({ path, job, onClose }: { path: Path; job: ImportJob; onClose: () => void }) {
  const [page, setPage] = useState(1);
  const q = useQuery({ queryKey: ['admin', 'textbooks', 'import-page', job.id, page], queryFn: () => unwrap(api.GET('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/imports/{importId}/pages/{pageNo}', { params: { path: { ...path, importId: job.id, pageNo: page } } })), retry: false });
  return <Drawer open width={760} title={job.file_name} onClose={onClose}>
    <Space style={{ marginBottom: 20 }}><span>{job.format === 'pdf' ? '文件页序' : '文本单元'}</span><InputNumber aria-label="预览页序" value={page} min={1} max={job.page_count || job.completed_pages} precision={0} onChange={(p) => { if (p) setPage(p); }} /></Space>
    <Loadable loading={q.isPending} error={q.error}>{q.data && <>
      {q.data.image_url && <Image src={q.data.image_url} alt={`PDF 第 ${page} 页原图`} style={{ maxHeight: 400, objectFit: 'contain' }} />}
      {q.data.printed_page && <Typography.Paragraph type="secondary">书内页码：{q.data.printed_page}</Typography.Paragraph>}
      {q.data.blank && <Typography.Paragraph type="secondary">该页识别为空白页，保留文件页序。</Typography.Paragraph>}
      {q.data.blocks.map((b, i) => <div key={i} style={{ marginTop: 20 }}>{b.heading && <Typography.Title level={5}>{b.heading}</Typography.Title>}{b.uncertain && <Tag color="orange">待核对</Tag>}<Typography.Paragraph style={{ whiteSpace: 'pre-wrap' }}>{b.text}</Typography.Paragraph></div>)}
    </>}</Loadable>
  </Drawer>;
}
