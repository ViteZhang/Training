// 7.16 官方教材库 B05/B06：真实索引代次、试检索与校验发布。
import { ApiError, type components } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Descriptions, Drawer, Empty, Form, Input, List, Modal, Select, Space, Table, Tag, Typography } from 'antd';
import { useRef, useState } from 'react';
import { Loadable } from '../components/Page';
import { api, dt, unwrap } from '../lib/api';

type Book = components['schemas']['Textbook'];
type Edition = components['schemas']['TextbookEdition'];
type Index = components['schemas']['TextbookIndex'];
type Path = { bookId: number; editionId: number; revisionId: number };
const indexPath = '/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/indexes';
const names = { queued: '等待构建', processing: '构建中', failed: '构建失败', ready: '就绪', stale: '已失效' };
const errorText = (e: unknown) => e instanceof ApiError ? e.message : '操作失败，请重试';
const key = ['admin', 'textbooks'];
function useRequestKey() {
  const keys = useRef(new Map<string, string>());
  return (value: unknown) => { const name = JSON.stringify(value); let k = keys.current.get(name); if (!k) { k = crypto.randomUUID(); keys.current.set(name, k); } return k; };
}
export function TextbookRetrievalStatus() {
  const q = useQuery({ queryKey: [...key, 'retrieval-settings'], queryFn: () => unwrap(api.GET('/admin/textbook-retrieval-settings')) });
  if (q.isPending) return null;
  if (q.error) return <Alert type="error" title="无法读取教材检索配置" action={<Button onClick={() => void q.refetch()}>重试</Button>} style={{ marginBottom: 20 }} />;
  const s = q.data;
  return s && <Alert type={s.embedding_enabled && s.vector_configured ? 'info' : 'warning'} title={`教材向量化：${s.embedding_enabled ? s.embedding_model : '未启用'} · ${s.embedding_dimensions} 维 · 向量服务${s.vector_configured ? '已配置' : '未配置'}`} description={s.embedding_provider === 'mock' ? '当前为模拟模型，仅供流程联调，不能用于真实效果验收或生产发布。' : '密钥仅由后端环境变量管理。配置完整后仍需实际构建与校验，不代表服务已连通。'} style={{ marginBottom: 20 }} />;
}
export function TextbookIndexes({ path, book, edition, onClose }: { path: Path; book: Book; edition: Edition; onClose: () => void }) {
  const qc = useQueryClient(); const { message } = AntApp.useApp(); const requestKey = useRequestKey();
  const [selected, setSelected] = useState<number>();
  const [buildRound,setBuildRound]=useState(0);
  const [query, setQuery] = useState('');
  const [publishing, setPublishing] = useState<{ index: Index; revisionVersion: number; editionVersion: number }>();
  const [summary, setSummary] = useState('');
  const rev = useQuery({ queryKey: [...key, path.bookId, path.editionId, path.revisionId], queryFn: () => unwrap(api.GET('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}', { params: { path } })) });
  const settings = useQuery({ queryKey: [...key, 'retrieval-settings'], queryFn: () => unwrap(api.GET('/admin/textbook-retrieval-settings')) });
  const indexes = useQuery({ queryKey: [...key, 'indexes', path], queryFn: () => unwrap(api.GET(indexPath, { params: { path } })), refetchInterval: (q) => q.state.data?.items.some((g) => g.status === 'queued' || g.status === 'processing') ? 2500 : false });
  const invalidate = () => { void qc.invalidateQueries({ queryKey: key }); };
  const build = useMutation({ mutationFn: (version: number) => unwrap(api.POST(indexPath, { params: { path }, body: { expected_version: version, idempotency_key: requestKey(['build', path, version,buildRound, settings.data?.profile_id, settings.data?.parameters]) } })), onSuccess: () => { setBuildRound(n=>n+1);invalidate(); message.success('已创建索引任务，可以关闭窗口后返回查看'); }, onError: (e) => message.error(errorText(e)) });
  const retry=useMutation({mutationFn:(g:Index)=>unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/indexes/{generationId}/retry',{params:{path:{...path,generationId:g.id}},body:{idempotency_key:requestKey(['retry',g.id,g.updated_at])}})),onSuccess:invalidate,onError:(e)=>message.error(errorText(e))});
  const search = useMutation({ mutationFn: ({ generation, text }: { generation: number; text: string }) => unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/search', { params: { path }, body: { generation_id: generation, query: text } })) });
  const publish = useMutation({ mutationFn: async () => {
    if (!publishing) throw new Error('未选择代次');
    const body = { expected_version: publishing.revisionVersion, expected_edition_version: publishing.editionVersion, generation_id: publishing.index.id, summary, idempotency_key: requestKey(['publish', path, publishing, summary]) };
    return unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}/publish', { params: { path }, body }));
  }, onSuccess: () => { setPublishing(undefined); invalidate(); message.success('教材修订已发布'); } });
  const items = indexes.data?.items ?? [];
  const r = rev.data;
  const ready = items.filter((g) => g.status === 'ready' && g.content_hash === r?.content_hash && g.profile_id === settings.data?.profile_id);
  const chosen = ready.find((g) => g.id === selected) ?? ready[0];
  const busy = items.some((g) => g.status === 'queued' || g.status === 'processing');
  return <Drawer open width={900} title={`${book.title} · ${edition.edition} · 索引与发布`} onClose={onClose}>
    <TextbookRetrievalStatus />
    <Loadable loading={rev.isPending} error={rev.error}>
      {r && <><Descriptions column={2} size="small" items={[{ key: 'revision', label: '内容修订', children: `r${r.revision_no} · ${r.status === 'published' ? '已发布' : r.status === 'reviewed' ? '已校对' : '待校对'}` }, { key: 'online', label: '当前线上修订', children: edition.current_revision_id ? `#${edition.current_revision_id}` : '尚未发布' }]} />
      <Space style={{ margin: '20px 0' }}><Button type="primary" loading={build.isPending} disabled={busy || r.status === 'draft' || !settings.data?.embedding_enabled || !settings.data.vector_configured} onClick={() => build.mutate(r.version)}>构建新索引代次</Button><Button onClick={() => { void rev.refetch(); void indexes.refetch(); void settings.refetch(); }}>刷新状态</Button></Space>
      <Typography.Paragraph type="secondary">每次构建保留独立代次；原文或模型配置变化后需重新构建。失败时已发布修订继续服务。</Typography.Paragraph></>}
    </Loadable>
    <Loadable loading={indexes.isPending} error={indexes.error}>
      <Table<Index> rowKey="id" dataSource={items} pagination={false} locale={{ emptyText: <Empty description="还没有索引代次" /> }} columns={[
        { title: '代次', render: (_, g) => `#${g.id}` },
        { title: '模型 / 维度', render: (_, g) => <>{g.model}<br />{g.dimensions} 维 · {g.parameters.version}</> },
        { title: '状态 / 进度', render: (_, g) => <><Tag>{g.content_hash !== r?.content_hash ? '原文已变化' : names[g.status]}</Tag><div>{g.completed_chunks} / {g.chunk_count || '待切分'} 段</div>{g.failure_message && <Typography.Text type="danger">{g.failure_message}</Typography.Text>}</> },
        { title: '创建时间', render: (_, g) => dt(g.created_at) },
        { title: '操作', render: (_, g) => <Space orientation="vertical">{g.status==='failed'&&<Button loading={retry.isPending} onClick={()=>retry.mutate(g)}>继续构建</Button>}<Button disabled={!r || g.status !== 'ready' || g.content_hash !== r.content_hash || g.profile_id !== settings.data?.profile_id || g.id !== r.ready_generation_id} onClick={() => { if (r) { publish.reset(); setSummary(''); setPublishing({ index: g, revisionVersion: r.version, editionVersion: edition.version ?? 1 }); } }}>校验发布</Button></Space> },
      ]} />
    </Loadable>
    <Card title="检索试用" size="small" style={{ marginTop: 28 }}>
      <Typography.Paragraph type="secondary">当前明确修订的后台试用，草稿不会自动发布。融合排名仅作诊断，不代表答案准确率。</Typography.Paragraph>
      <Select aria-label="试用索引代次" value={chosen?.id} onChange={(v) => { setSelected(v); search.reset(); }} style={{ width: '100%', marginBottom: 16 }} options={ready.map((g) => ({ value: g.id, label: `索引 #${g.id} · ${g.model} / ${g.dimensions} 维` }))} placeholder="需有与当前原文和模型兼容的就绪代次" />
      <Input.TextArea aria-label="检索问题" rows={3} maxLength={1800} placeholder="填写题干及相关材料，保留专业术语和否定条件" value={query} onChange={(e) => { setQuery(e.target.value); search.reset(); }} />
      <Button style={{ marginTop: 16 }} loading={search.isPending} disabled={!chosen || !query.trim()} onClick={() => chosen && search.mutate({ generation: chosen.id, text: query })}>试检索原文</Button>
      {search.isError && <Alert type="error" title={errorText(search.error)} style={{ marginTop: 16 }} />}
      {search.data && <><Alert type="info" title={search.data.notice} style={{ marginTop: 20 }} /><List dataSource={search.data.hits} locale={{ emptyText: '本修订未召回相关片段' }} renderItem={(h) => <List.Item><div style={{ width: '100%' }}><Space wrap><Typography.Text strong>{h.heading || `原文单元 ${h.section_no}`}</Typography.Text><Tag>片段 {h.ordinal}</Tag>{h.source_page && <Tag>文件第 {h.source_page} 页</Tag>}{h.printed_page && <Tag>书内 {h.printed_page}</Tag>}</Space><Typography.Paragraph style={{ whiteSpace: 'pre-wrap', marginTop: 12 }}>{h.text}</Typography.Paragraph><Typography.Text type="secondary">全文排名 {h.fulltext_rank || '未召回'} · 向量排名 {h.vector_rank || '未召回'} · RRF {h.fusion_score.toFixed(5)} · 字符位置 {h.start_rune}–{h.end_rune}</Typography.Text></div></List.Item>} /></>}
    </Card>
    <Modal open={!!publishing} title="校验并发布教材修订" okText="校验并发布" onCancel={() => { if (!publish.isPending) setPublishing(undefined); }} onOk={() => { if (summary.trim()) publish.mutate(); }} confirmLoading={publish.isPending} okButtonProps={{ disabled: !summary.trim() }} maskClosable={false}>
      <Typography.Paragraph>服务端将检查授权、校对、原文校验和全文 / 向量索引。成功后切换到修订 r{r?.revision_no} / 索引 #{publishing?.index.id}，已有答案保持原来的依据。</Typography.Paragraph>
      <Input.TextArea aria-label="发布变更说明" rows={3} maxLength={500} value={summary} onChange={(e) => setSummary(e.target.value)} placeholder="填写本次变更说明" />
      {publish.isError && <Alert style={{ marginTop: 16 }} type="error" title={errorText(publish.error)} description="版本冲突时请关闭窗口并刷新教材详情，再核对发布。" />}
    </Modal>
  </Drawer>;
}

export function TextbookAvailability({ book, edition, onClose }: { book: Book; edition: Edition; onClose: () => void }) {
  const [form] = Form.useForm<components['schemas']['TextbookAvailabilityChange']>();
  const requestKey = useRequestKey(); const qc = useQueryClient();
  const save = useMutation({ mutationFn: (values: components['schemas']['TextbookAvailabilityChange']) => unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/availability', { params: { path: { bookId: book.id, editionId: edition.id } }, body: { ...values, expected_version: edition.version ?? 1, idempotency_key: requestKey(values), license_expires_at: values.license_expires_at ? new Date(values.license_expires_at).toISOString() : null } })), onSuccess: () => { void qc.invalidateQueries({ queryKey: key }); onClose(); } });
  return <Modal open title={`${edition.edition} · 授权与可用性`} onCancel={() => { if (!save.isPending) onClose(); }} onOk={() => form.submit()} okText="确认保存" confirmLoading={save.isPending} maskClosable={false}>
    <Typography.Paragraph type="secondary">下架停止新答案使用；停用历史摘录展示后，APP 的原文摘录不可查看。恢复授权后须重新校验发布。</Typography.Paragraph>
    <Form form={form} layout="vertical" initialValues={{ withdrawn: edition.status === 'withdrawn', authorized: edition.authorized, license_note: edition.license_note, excerpts_enabled: edition.excerpts_enabled ?? true, license_expires_at: edition.license_expires_at ? localDateTime(edition.license_expires_at) : undefined, summary: '' }} onFinish={(v) => save.mutate(v)}>
      <Form.Item name="withdrawn" label="新检索" rules={[{ required: true }]}><Select options={[{ value: false, label: '保留当前状态（不自动恢复发布）' }, { value: true, label: '下架，停止新检索' }]} /></Form.Item>
      <Form.Item name="authorized" label="授权状态"><Select options={[{ value: true, label: '已获授权' }, { value: false, label: '停用授权' }]} /></Form.Item>
      <Form.Item name="license_note" label="授权说明" rules={[{ required: true, whitespace: true }]}><Input.TextArea maxLength={500} /></Form.Item>
      <Form.Item name="license_expires_at" label="授权截止时间（本机时间，留空表示未登记）" rules={[{ validator: (_, v: string) => !v || !Number.isNaN(new Date(v).getTime()) ? Promise.resolve() : Promise.reject(new Error('填写有效时间')) }]}><Input type="datetime-local" /></Form.Item>
      <Form.Item name="excerpts_enabled" label="历史摘录展示"><Select options={[{ value: true, label: '允许查看' }, { value: false, label: '停止展示原文摘录' }]} /></Form.Item>
      <Form.Item name="summary" label="变更原因" rules={[{ required: true, whitespace: true }]}><Input.TextArea maxLength={500} /></Form.Item>
    </Form>
    {save.isError && <Alert type="error" title={errorText(save.error)} />}
  </Modal>;
}
export function TextbookPublicationHistory({ bookId }: { bookId: number }) {
  const q = useQuery({ queryKey: [...key, 'publications', bookId], queryFn: () => unwrap(api.GET('/admin/textbooks/{bookId}/publications', { params: { path: { bookId } } })) });
  return <Card size="small" title="发布与可用性记录"><Loadable loading={q.isPending} error={q.error}><List dataSource={q.data?.items} locale={{ emptyText: '暂无发布记录' }} renderItem={(r) => <List.Item><List.Item.Meta title={r.summary} description={`${dt(r.created_at)} · 操作人 #${r.created_by} · 版次 #${r.edition_id}${r.generation_id ? ` / 索引 #${r.generation_id}` : ''}`} /></List.Item>} /></Loadable></Card>;
}

function localDateTime(value:string){const date=new Date(value);return new Date(date.getTime()-date.getTimezoneOffset()*60000).toISOString().slice(0,16);}
