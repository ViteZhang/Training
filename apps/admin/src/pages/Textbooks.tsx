// 7.16 官方教材库：独立教材身份、出版版本和原文修订，设计稿 B01/B02/B04。
import { ApiError, type components } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Checkbox, Descriptions, Drawer, Empty, Form, Input, InputNumber, List, Modal, Select, Space, Table, Tag, Typography } from 'antd';
import { useRef, useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { api, dt, unwrap } from '../lib/api';
import { TextbookImports } from './TextbookImports';
import { TextbookIndexes, TextbookRetrievalStatus, TextbookAvailability, TextbookPublicationHistory } from './TextbookIndexes';

type Book = components['schemas']['Textbook'];
type Metadata = components['schemas']['TextbookMetadata'];
type BookFormValues = Metadata & EditionInput & { edition_scopes?: components['schemas']['TextbookEditionScopeUpdate'][] };
type EditionInput = components['schemas']['TextbookEditionInput'];
type Revision = components['schemas']['TextbookRevision'];
type Section = components['schemas']['TextbookRevisionSection'];
const kinds = [{ value: 'textbook', label: '教材' }, { value: 'handout', label: '讲义' }];
const statusNames = { draft: '待校对', reviewed: '已校对', published: '已发布' };
const indexNames = { missing: '未构建', stale: '待构建', ready: '可检索' };
const catalogKey = ['admin', 'textbooks'];
class SaveVerificationError extends Error {}
const errorText = (e: unknown) => e instanceof ApiError || e instanceof SaveVerificationError ? e.message : '操作失败，请重试';

export function Textbooks() {
  const [filters, setFilters] = useState({ q: '', kind: '', discipline: '', page: 1, page_size: 20 });
  const [search, setSearch] = useState('');
  const [bookId, setBookId] = useState<number>();
  const [creating, setCreating] = useState(false);
  const q = useQuery({ queryKey: [...catalogKey, 'list', filters], queryFn: () => unwrap(api.GET('/admin/textbooks', { params: { query: filters } })) });
  return <>
    <PageTitle code="7.16" title="官方教材库" extra={<Button type="primary" onClick={() => setCreating(true)}>新建教材</Button>} />
    <Typography.Paragraph type="secondary">统一维护教材与讲义原文，可跨专业课、跨官方题库项目复用。</Typography.Paragraph>
    <TextbookRetrievalStatus /><Space wrap style={{ marginBottom: 20 }}>
      <Input.Search aria-label="搜索教材" placeholder="搜索书名、作者、别名" value={search} onChange={(e) => setSearch(e.target.value)} onSearch={(q) => setFilters((f) => ({ ...f, q, page: 1 }))} allowClear style={{ width: 300 }} />
      <Select aria-label="素材类型" value={filters.kind} onChange={(kind) => setFilters((f) => ({ ...f, kind, page: 1 }))} style={{ width: 130 }} options={[{ value: '', label: '全部类型' }, ...kinds]} />
      <Input aria-label="学科筛选" placeholder="适用学科" value={filters.discipline} onChange={(e) => setFilters((f) => ({ ...f, discipline: e.target.value, page: 1 }))} style={{ width: 160 }} />
      <Button onClick={() => { setSearch(''); setFilters({ q: '', kind: '', discipline: '', page: 1, page_size: 20 }); }}>重置</Button>
    </Space>
    <Loadable loading={q.isPending} error={q.error}>
      <Table<Book> rowKey="id" dataSource={q.data?.items ?? []} locale={{ emptyText: <Empty description="还没有教材，先新建一本教材或讲义" /> }} pagination={{ current: filters.page, pageSize: filters.page_size, total: q.data?.total ?? 0, showSizeChanger: false, onChange: (page) => setFilters((f) => ({ ...f, page })) }} columns={[
        { title: '教材 / 讲义', render: (_, b) => <><Button type="link" style={{ padding: 0 }} onClick={() => setBookId(b.id)}>{b.title}</Button><div><Typography.Text type="secondary">{b.author}</Typography.Text></div></> },
        { title: '类型', render: (_, b) => b.kind === 'textbook' ? '教材' : '讲义' },
        { title: '适用学科', dataIndex: 'discipline' },
        { title: '使用范围', render: (_, b) => b.editions.length ? <Space orientation="vertical" size={4} style={{ maxWidth: 300, overflowWrap: 'anywhere' }}>{b.editions.map((e) => <div key={e.id}><Typography.Text type="secondary">{e.edition}：</Typography.Text>{e.scope_note || '未填写'}</div>)}</Space> : '—' },
        { title: '出版版本与内容状态', render: (_, b) => <Space orientation="vertical" size={4}>{b.editions.map((e) => <div key={e.id}><span>{e.edition} </span><Tag color={e.status === 'published' ? 'green' : 'default'}>{e.status === 'published' ? '线上已发布' : e.status === 'withdrawn' ? '已下架' : '未发布'}</Tag>{e.revisions[0] ? <Typography.Text type="secondary">最新 r{e.revisions[0].revision_no} · {statusNames[e.revisions[0].status]} · 索引{indexNames[e.revisions[0].index_status]}</Typography.Text> : <Typography.Text type="secondary">尚无原文</Typography.Text>}</div>)}</Space> },
        { title: '更新时间', render: (_, b) => dt(b.updated_at) },
        { title: '操作', render: (_, b) => <Button onClick={() => setBookId(b.id)}>管理</Button> },
      ]} />
    </Loadable>
    {creating && <BookForm onClose={() => setCreating(false)} onDone={(book) => { setCreating(false); setBookId(book.id); }} />}
    {bookId && <BookDetail bookId={bookId} onClose={() => setBookId(undefined)} />}
  </>;
}

function MetadataFields() {
  return <>
    <Form.Item name="title" label="标准书名" rules={[{ required: true, whitespace: true, message: '填写标准书名' }]}><Input maxLength={128} /></Form.Item>
    <Space align="start" style={{ width: '100%' }}>
      <Form.Item name="author" label="作者 / 编者" rules={[{ required: true, whitespace: true }]}><Input maxLength={128} /></Form.Item>
      <Form.Item name="kind" label="类型" rules={[{ required: true }]}><Select options={kinds} style={{ width: 140 }} /></Form.Item>
    </Space>
    <Form.Item name="discipline" label="适用学科" rules={[{ required: true, whitespace: true }]}><Input maxLength={128} /></Form.Item>
    <Form.Item name="aliases" label="简称与别名" extra="回车添加，最多 20 个；别名可重复存在于不同教材中。"><Select mode="tags" maxCount={20} tokenSeparators={['，', ',']} /></Form.Item>
  </>;
}
function EditionFields() {
  return <>
    <Form.Item name="edition" label="出版 / 讲义版本" rules={[{ required: true, whitespace: true }]}><Input placeholder="如：第五版、2027 春季讲义" maxLength={64} /></Form.Item>
    <Space align="start">
      <Form.Item name="publisher" label="出版社"><Input maxLength={128} /></Form.Item>
      <Form.Item name="publication_year" label="出版年份"><InputNumber min={1800} max={2200} precision={0} /></Form.Item>
    </Space>
    <Form.Item name="isbn" label="ISBN（选填）"><Input maxLength={32} /></Form.Item>
    <Form.Item name="scope_note" label="授权 / 适用范围"><Input maxLength={255} placeholder="如：适用院校、课程及使用范围" /></Form.Item>
    <Form.Item name="license_note" label="授权说明" rules={[{ required: true, whitespace: true }]}><Input.TextArea rows={2} maxLength={500} placeholder="填写授权方及授权依据" /></Form.Item>
    <Form.Item name="authorized" valuePropName="checked" rules={[{ validator: (_, v) => v ? Promise.resolve() : Promise.reject(new Error('请确认素材使用授权')) }]}><Checkbox>确认可用于应用内检索及相关摘录展示</Checkbox></Form.Item>
  </>;
}
function BookForm({ book: initialBook, onClose, onDone }: { book?: Book; onClose: () => void; onDone: (book: Book) => void }) {
  // 保留打开表单时的版本快照，后台刷新不能让旧输入绕过冲突检查。
  const [book] = useState(initialBook);
  const [form] = Form.useForm<BookFormValues>();
  const [key] = useState(() => crypto.randomUUID());
  const qc = useQueryClient();
  const { modal, message } = AntApp.useApp();
  const mutation = useMutation({ mutationFn: async (v: BookFormValues) => {
    if (!book) return unwrap(api.POST('/admin/textbooks', { body: { title: v.title, author: v.author, kind: v.kind, discipline: v.discipline, aliases: v.aliases ?? [], idempotency_key: key, initial_edition: { edition: v.edition, publisher: v.publisher, publication_year: v.publication_year ?? undefined, isbn: v.isbn, license_note: v.license_note, authorized: v.authorized, scope_note: v.scope_note } } }));
    const editionScopes = v.edition_scopes?.filter((scope) => scope.scope_note !== book.editions.find((edition) => edition.id === scope.edition_id)?.scope_note) ?? [];
    const params = { path: { bookId: book.id } };
    await unwrap(api.PUT('/admin/textbooks/{bookId}', { params, body: { title: v.title, author: v.author, kind: v.kind, discipline: v.discipline, aliases: v.aliases ?? [], expected_version: book.version, edition_scopes: editionScopes } }));
    // 保存后重新读取；旧服务忽略新增字段或数据未落库时，不能仅凭 HTTP 200 关闭表单。
    let saved: Book;
    try { saved = await unwrap(api.GET('/admin/textbooks/{bookId}', { params, cache: 'no-store' })); }
    catch { throw new SaveVerificationError('暂时无法确认保存结果，当前输入已保留，请重试'); }
    const aliases = [...new Set((v.aliases ?? []).map((alias) => alias.trim()))];
    if (saved.title !== v.title.trim() || saved.author !== v.author.trim() || saved.kind !== v.kind || saved.discipline !== v.discipline.trim() || JSON.stringify(saved.aliases) !== JSON.stringify(aliases)) {
      throw new SaveVerificationError('教材信息未保存成功，当前输入已保留，请重试');
    }
    if (editionScopes.some((scope) => saved.editions.find((edition) => edition.id === scope.edition_id)?.scope_note !== scope.scope_note.trim())) {
      throw new SaveVerificationError('使用范围未保存成功，当前输入已保留，请重试');
    }
    return saved;
  },
    onSuccess: (b) => { qc.setQueryData([...catalogKey, b.id], b); void qc.invalidateQueries({ queryKey: catalogKey }); void message.success(book ? '教材信息已保存' : '教材已创建'); onDone(b); },
  });
  const close = () => { if (mutation.isPending) return; if (form.isFieldsTouched()) modal.confirm({ title: '放弃未保存的修改？', onOk: onClose }); else onClose(); };
  return <Modal open title={book ? '编辑教材信息' : '新建教材 / 讲义'} width={660} okText="保存" onOk={() => form.submit()} confirmLoading={mutation.isPending} onCancel={close} maskClosable={false}>
    {mutation.isError && <Alert type="error" showIcon title={errorText(mutation.error)} style={{ marginBottom: 16 }} />}
    <Form form={form} layout="vertical" initialValues={book ? { ...book, edition_scopes: book.editions.map((e) => ({ edition_id: e.id, expected_version: e.version ?? 1, scope_note: e.scope_note })) } : { kind: 'textbook', aliases: [], authorized: false }} onFinish={(v) => mutation.mutate(v)}>
      <MetadataFields />{book && <>
        <Typography.Title level={5}>各出版版本的使用范围</Typography.Title>
        <Form.List name="edition_scopes">{(fields) => fields.map((field) => <div key={field.key}>
          <Form.Item name={[field.name, 'edition_id']} hidden><InputNumber /></Form.Item>
          <Form.Item name={[field.name, 'expected_version']} hidden><InputNumber /></Form.Item>
          <Form.Item name={[field.name, 'scope_note']} label={`${book.editions[field.name]?.edition} · 使用范围`} rules={[{ max: 255, message: '使用范围最多 255 字' }]}>
            <Input.TextArea rows={2} maxLength={255} showCount placeholder="如：适用院校、课程及使用范围；留空可清除" />
          </Form.Item>
        </div>)}</Form.List>
      </>}{!book && <><Typography.Title level={5}>首个出版版本</Typography.Title><EditionFields /></>}
    </Form>
  </Modal>;
}

function BookDetail({ bookId, onClose }: { bookId: number; onClose: () => void }) {
  const qc = useQueryClient();
  const { message,modal } = AntApp.useApp();
  const q = useQuery({ queryKey: [...catalogKey, bookId], queryFn: () => unwrap(api.GET('/admin/textbooks/{bookId}', { params: { path: { bookId } } })) });
  const [editing, setEditing] = useState(false);
  const [addingEdition, setAddingEdition] = useState(false);
  const [revision, setRevision] = useState<{ editionId: number; revisionId: number }>();
  const [imports, setImports] = useState<{ editionId: number; revisionId: number }>();
 const [indexes,setIndexes]=useState<{editionId:number;revisionId:number}>();
 const [availability,setAvailability]=useState<components['schemas']['TextbookEdition']>();
  // 保留失败请求的 key；成功之后才为下一次“新建草稿”分配 key。
  const revisionKeys = useRef(new Map<number, string>());
  const createRevision = useMutation({ mutationFn: (editionId: number) => {
    let key = revisionKeys.current.get(editionId); if (!key) { key = crypto.randomUUID(); revisionKeys.current.set(editionId, key); }
    return unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/revisions', { params: { path: { bookId, editionId } }, body: { idempotency_key: key } }));
  }, onSuccess: (r) => { revisionKeys.current.delete(r.edition_id); void qc.invalidateQueries({ queryKey: catalogKey }); setRevision({ editionId: r.edition_id, revisionId: r.id }); }, onError: (e) => message.error(errorText(e)) });
  const defaultKeys = useRef(new Map<string,string>());
  const setDefault = useMutation({ mutationFn: (e: components['schemas']['TextbookEdition']) => {
    if (!q.data) throw new Error('请刷新教材');
    const enabled=q.data.default_edition_id!==e.id;
    const value=JSON.stringify([e.id,q.data.version,e.version,enabled]);let k=defaultKeys.current.get(value);if(!k){k=crypto.randomUUID();defaultKeys.current.set(value,k);}
    return unwrap(api.POST('/admin/textbooks/{bookId}/editions/{editionId}/default',{params:{path:{bookId,editionId:e.id}},body:{idempotency_key:k,expected_book_version:q.data.version,expected_edition_version:e.version??1,enabled}}));
  },onSuccess:()=>{void qc.invalidateQueries({queryKey:catalogKey});},onError:(e)=>message.error(errorText(e))});
  const b = q.data;
  return <Drawer open width={880} title={b?.title ?? '教材详情'} onClose={onClose}>
    <Loadable loading={q.isPending} error={q.error}>{b && <>
      <Descriptions size="small" column={2} items={[{ key: 'author', label: '作者 / 编者', children: b.author }, { key: 'kind', label: '类型', children: b.kind === 'textbook' ? '教材' : '讲义' }, { key: 'discipline', label: '适用学科', children: b.discipline }, { key: 'aliases', label: '别名', children: b.aliases.join('、') || '—' }]} />
      <Space style={{ margin: '16px 0 24px' }}><Button onClick={() => setEditing(true)}>编辑教材信息</Button><Button onClick={() => setAddingEdition(true)}>新增出版版本</Button></Space>
      {b.editions.map((e) => <Card key={e.id} title={e.edition} size="small" style={{ marginBottom: 20 }} extra={<Button onClick={() => createRevision.mutate(e.id)} loading={createRevision.isPending && createRevision.variables === e.id} disabled={createRevision.isPending}>新建原文草稿</Button>}>
        <Space wrap style={{marginBottom:16}}>{b.default_edition_id===e.id&&<Tag color="green">默认版次</Tag>}<Button onClick={()=>setAvailability(e)}>授权与下架</Button><Button disabled={setDefault.isPending||(b.default_edition_id!==e.id&&e.status!=='published')} onClick={()=>modal.confirm({title:b.default_edition_id===e.id?'取消默认版次？':'设为默认版次？',content:'影响之后未指定版次的书名匹配，已有答案保持不变。',onOk:()=>setDefault.mutateAsync(e).then(()=>undefined)})}>{b.default_edition_id===e.id?'取消默认':'设为默认'}</Button></Space><Descriptions column={2} size="small" items={[{ key: 'state', label: '线上状态', children: e.status === 'published' ? '已发布' : e.status === 'withdrawn' ? '已下架' : '未发布' }, { key: 'pub', label: '出版社 / 年份', children: [e.publisher, e.publication_year].filter(Boolean).join(' · ') || '—' }, { key: 'isbn', label: 'ISBN', children: e.isbn || '—' }, { key: 'license', label: '授权说明', children: e.license_note }, { key: 'scope', label: '适用范围', children: e.scope_note || '—' }]} />
        <List size="small" locale={{ emptyText: '尚无原文，创建草稿后粘贴教材章节。' }} dataSource={e.revisions} renderItem={(r) => <List.Item actions={[<Button key="indexes" onClick={()=>setIndexes({editionId:e.id,revisionId:r.id})}>索引与发布</Button>,...(r.status !== 'published' ? [<Button key="import" onClick={() => setImports({ editionId: e.id, revisionId: r.id })}>导入文件</Button>] : []), <Button key="edit" type="link" onClick={() => setRevision({ editionId: e.id, revisionId: r.id })}>{r.status === 'published' ? '查看原文' : '校对原文'}</Button>]}><Space wrap><Typography.Text strong>r{r.revision_no}</Typography.Text><Tag>{statusNames[r.status]}</Tag><Typography.Text type="secondary">索引{indexNames[r.index_status]} · {dt(r.updated_at)}</Typography.Text></Space></List.Item>} />
      </Card>)}
      <TextbookPublicationHistory bookId={bookId} />
    </>}</Loadable>
    {b && editing && <BookForm book={b} onClose={() => setEditing(false)} onDone={() => setEditing(false)} />}
    {addingEdition && <AddEdition bookId={bookId} onClose={() => setAddingEdition(false)} />}
    {imports && <TextbookImports path={{ bookId, ...imports }} onClose={() => setImports(undefined)} />}
    {b && indexes && b.editions.find(e=>e.id===indexes.editionId) && <TextbookIndexes path={{bookId,...indexes}} book={b} edition={b.editions.find(e=>e.id===indexes.editionId)!} onClose={()=>setIndexes(undefined)} />}
    {b && availability && <TextbookAvailability book={b} edition={availability} onClose={()=>setAvailability(undefined)} />}
    {revision && <RevisionEditor bookId={bookId} {...revision} onClose={() => setRevision(undefined)} />}
  </Drawer>;
}
function AddEdition({ bookId, onClose }: { bookId: number; onClose: () => void }) {
  const [form] = Form.useForm<EditionInput>(); const [key] = useState(() => crypto.randomUUID());
  const qc = useQueryClient(); const { modal } = AntApp.useApp();
  const save = useMutation({ mutationFn: (v: EditionInput) => unwrap(api.POST('/admin/textbooks/{bookId}/editions', { params: { path: { bookId } }, body: { ...v, publication_year: v.publication_year ?? undefined, idempotency_key: key } })), onSuccess: () => { void qc.invalidateQueries({ queryKey: catalogKey }); onClose(); } });
  const close = () => { if (save.isPending) return; if (form.isFieldsTouched()) modal.confirm({ title: '放弃未保存的版本？', onOk: onClose }); else onClose(); };
  return <Modal open title="新增出版版本" onOk={() => form.submit()} okText="保存版本" confirmLoading={save.isPending} onCancel={close} maskClosable={false}>
    {save.isError && <Alert type="error" title={errorText(save.error)} style={{ marginBottom: 16 }} />}
    <Form form={form} layout="vertical" initialValues={{ authorized: false }} onFinish={(v) => save.mutate(v)}><EditionFields /></Form>
  </Modal>;
}
function RevisionEditor({ bookId, editionId, revisionId, onClose }: { bookId: number; editionId: number; revisionId: number; onClose: () => void }) {
  const path = { bookId, editionId, revisionId };
  const q = useQuery({ queryKey: [...catalogKey, bookId, editionId, revisionId], queryFn: () => unwrap(api.GET('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}', { params: { path } })), refetchOnWindowFocus: false });
  return <Loadable loading={q.isPending} error={q.error}>{q.data && <RevisionForm key={q.data.id} revision={q.data} path={path} onClose={onClose} />}</Loadable>;
}
function RevisionForm({ revision, path, onClose }: { revision: Revision; path: { bookId: number; editionId: number; revisionId: number }; onClose: () => void }) {
  const [form] = Form.useForm<{ sections: Section[] }>();
  const [current, setCurrent] = useState(revision);
  const [dirty, setDirty] = useState(false);
  const qc = useQueryClient(); const { message, modal } = AntApp.useApp();
  const readonly = current.status === 'published';
  const save = useMutation({ mutationFn: ({ sections, reviewed }: { sections: Section[]; reviewed: boolean }) => unwrap(api.PUT('/admin/textbooks/{bookId}/editions/{editionId}/revisions/{revisionId}', { params: { path }, body: { expected_version: current.version, sections, mark_reviewed: reviewed } })), onSuccess: (r) => { setCurrent(r); setDirty(false); form.setFieldsValue({ sections: r.sections }); void qc.invalidateQueries({ queryKey: catalogKey }); message.success(r.status === 'reviewed' ? '已确认校对，等待构建索引' : '草稿已保存'); } });
  const submit = async (reviewed: boolean) => { const v = await form.validateFields().catch(() => undefined); if (!v) return; if (reviewed) modal.confirm({ title: '确认已逐项核对原文？', content: '请核对正文与章节、页码。后续修改正文需重新校对。', okText: '确认校对', onOk: () => save.mutateAsync({ sections: v.sections, reviewed }).then(() => undefined) }); else save.mutate({ sections: v.sections, reviewed }); };
  const close = () => { if (save.isPending) return; if (dirty) modal.confirm({ title: '放弃未保存的原文？', onOk: onClose }); else onClose(); };
  return <Drawer open width={760} title={`原文修订 r${current.revision_no} · ${statusNames[current.status]}`} onClose={close} maskClosable={false} extra={!readonly && <Space><Button disabled={save.isPending} onClick={() => void submit(false)}>保存草稿</Button><Button type="primary" loading={save.isPending} onClick={() => void submit(true)}>确认校对</Button></Space>}>
    <Typography.Paragraph type="secondary">按章节录入忠实原文，保留原始页码。导入时标记不确定的单元，请核对后取消“仍需核对”。保存后需构建检索索引，正式发布后才供 APP 生成答案使用。</Typography.Paragraph>
    {save.isError && <Alert type="error" title={errorText(save.error)} description="当前编辑内容已保留。若发生版本冲突，请复制未保存原文后关闭并重新打开。" style={{ marginBottom: 16 }} />}
    <Form form={form} layout="vertical" disabled={readonly || save.isPending} initialValues={{ sections: current.sections.length ? current.sections : [{ heading: '', text: '' }] }} onValuesChange={() => setDirty(true)}>
      <Form.List name="sections" rules={[{ validator: (_, v: Section[]) => v?.length ? Promise.resolve() : Promise.reject(new Error('至少录入一个原文单元')) }]}>{(fields, { add, remove }, { errors }) => <>
        {fields.map((f, index) => <Card key={f.key} size="small" title={`原文单元 ${index + 1}`} style={{ marginBottom: 16 }} extra={!readonly && <Button danger type="link" onClick={() => { remove(f.name); setDirty(true); }}>移除</Button>}>
          <Form.Item name={[f.name, 'source_import_id']} hidden><InputNumber /></Form.Item>
          <Form.Item name={[f.name, 'heading']} label="章节标题"><Input maxLength={512} /></Form.Item>
          <Space align="start"><Form.Item name={[f.name, 'source_page']} label="文件页序"><InputNumber min={1} precision={0} /></Form.Item><Form.Item name={[f.name, 'printed_page']} label="书内页码"><Input maxLength={32} /></Form.Item></Space>
          <Form.Item name={[f.name, 'needs_review']} valuePropName="checked"><Checkbox>此单元仍需核对</Checkbox></Form.Item>
          <Form.Item name={[f.name, 'text']} label="原文" rules={[{ required: true, whitespace: true, max: 100000, message: '填写原文，每单元最多 10 万字' }]}><Input.TextArea rows={9} /></Form.Item>
        </Card>)}
        <Form.ErrorList errors={errors} />{!readonly && <Button block onClick={() => { add({ heading: '', text: '' }); setDirty(true); }}>添加原文单元</Button>}
      </>}</Form.List>
    </Form>
  </Drawer>;
}
