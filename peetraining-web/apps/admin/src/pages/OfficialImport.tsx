// 7.17 AI 解析导入（PRD v3 增补，设计稿 n_admin_import）：先选官方题库项目；上传资料并标类型（真题卷 / 参考书目录 / 有授权的讲义），
// 或粘贴文字、表格导入（CSV）；文件卡显示解析状态和结果数；核对区三栏：左侧题目列表与置信度和已核对数，中间原文并高亮当前题，
// 右侧解析结果（题型分值、知识点归属、采分点、参考答案及引用来源）；采分点合计不等于分值时标红；逐条「接受、修改、驳回」，
// 接受的提交 7.13 审核。只解析团队上传的资料，与用户端资料完全隔离。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Checkbox, Col, Empty, Form, Input, InputNumber, List, Modal, Row, Select, Space, Tag, Typography, Upload } from 'antd';
import { useState } from 'react';
import { Loadable, PageTitle } from '../components/Page';
import { ProjectPicker } from '../components/ProjectPicker';
import { api, unwrap } from '../lib/api';
import { uploadTextbookFile } from '../lib/textbookUpload';

type ImportFile = Schemas['OfficialImportFile'];
type Item = Schemas['OfficialImportItem'];
type Point = { content: string; score: number };
type QuestionPayload = { qtype: string; stem: string; answer?: string; score?: number; exam_year?: number; kp_names?: string[]; rubric?: Point[] };

const kindNames: Record<ImportFile['kind'], string> = { exam: '真题卷', catalog: '参考书目录', handout: '授权讲义', table: '表格导入' };
const statusText: Record<ImportFile['status'], { text: string; color?: string }> = {
  uploading: { text: '待上传' },
  parsing: { text: '解析中', color: 'gold' },
  parsed: { text: '已解析', color: 'green' },
  failed: { text: '解析失败', color: 'red' },
};
const confidence: Record<Item['confidence'], { text: string; color: string }> = {
  high: { text: '置信 高', color: 'green' },
  medium: { text: '置信 中', color: 'gold' },
  low: { text: '置信 低', color: 'red' },
};
const qtypeNames: Record<string, string> = {
  term: '名词解释', short_answer: '简答', discussion: '论述', essay: '作文', single_choice: '单选', multi_choice: '多选', true_false: '判断', fill_blank: '填空', calculation: '计算', other: '其他',
};

/** 文件格式按扩展名判断。 */
export function formatOf(name: string): ImportFile['format'] | undefined {
  const ext = name.toLowerCase().split('.').pop() ?? '';
  if (ext === 'pdf') return 'pdf';
  if (ext === 'docx') return 'docx';
  if (['jpg', 'jpeg', 'png', 'webp', 'heic'].includes(ext)) return 'image';
  if (ext === 'csv') return 'csv';
  return undefined;
}

/** 一条解析结果的真题年份。 */
function yearOf(it: Item): number | undefined {
  const y = (it.payload as Record<string, unknown>).exam_year;
  return typeof y === 'number' ? y : undefined;
}

/** 一条解析结果的标题。 */
export function itemTitle(it: Item) {
  const p = it.payload as Record<string, unknown>;
  if (it.item_type === 'knowledge_point') return `知识点：${String(p.name ?? '')}`;
  const year = typeof p.exam_year === 'number' ? `${p.exam_year} · ` : '';
  return `${year}${qtypeNames[String(p.qtype)] ?? ''}：${String(p.stem ?? '')}`;
}

export function OfficialImport() {
  const [project, setProject] = useState<number>();
  const [fileId, setFileId] = useState<number>();
  return (
    <>
      <PageTitle code="7.17" title="AI 解析导入" extra={<ProjectPicker value={project} onChange={(id) => { setProject(id); setFileId(undefined); }} />} />
      {project ? <ProjectImports projectId={project} fileId={fileId} onFile={setFileId} /> : <Empty description="先选官方题库项目" />}
      <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>后台只解析团队上传的资料，与用户上传的资料完全隔离。核对完成的内容提交到审核队列。</Typography.Paragraph>
    </>
  );
}

function ProjectImports({ projectId, fileId, onFile }: { projectId: number; fileId?: number; onFile: (id: number) => void }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [adding, setAdding] = useState(false);
  const files = useQuery({
    queryKey: ['admin', 'official-import', projectId],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/imports', { params: { path: { projectId } } })),
    refetchInterval: (q) => (q.state.data?.items.some((f) => f.status === 'parsing') ? 5000 : false),
  });
  const confirm = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/official/projects/{projectId}/imports/{fileId}/confirm', { params: { path: { projectId, fileId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] }),
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  const list = files.data?.items ?? [];
  return (
    <Loadable loading={files.isPending} error={files.error}>
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        {list.map((f) => (
          <Col span={6} key={f.id}>
            <Card size="small" hoverable onClick={() => f.status === 'parsed' && onFile(f.id)} style={fileId === f.id ? { borderColor: '#1A1A1A' } : undefined}>
              <Space direction="vertical" size={2}>
                <Typography.Text strong ellipsis style={{ maxWidth: 220 }}>{f.file_name}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {kindNames[f.kind]} · <Tag color={statusText[f.status].color}>{statusText[f.status].text}</Tag>
                  {f.status === 'parsed' ? (f.kind === 'catalog' ? `知识点 ${f.kp_count} 个` : `${f.question_count} 题`) : null}
                </Typography.Text>
                {f.status === 'parsed' ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>已核对 {f.reviewed_count}</Typography.Text> : null}
                {f.fail_reason ? <Typography.Text type="danger" style={{ fontSize: 12 }}>{f.fail_reason}</Typography.Text> : null}
                {f.status === 'failed' ? <Button size="small" onClick={(e) => { e.stopPropagation(); confirm.mutate(f.id); }}>重新解析</Button> : null}
              </Space>
            </Card>
          </Col>
        ))}
        <Col span={6}>
          <Card size="small" hoverable onClick={() => setAdding(true)} style={{ borderStyle: 'dashed' }}>
            <Typography.Text strong>+ 上传资料</Typography.Text>
            <br />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>真题卷、参考书目录、有授权的讲义；解析不理想时可改用表格导入</Typography.Text>
          </Card>
        </Col>
      </Row>
      {fileId ? <Review projectId={projectId} fileId={fileId} /> : <Empty description="点一份已解析的资料开始核对" />}
      <AddFile projectId={projectId} open={adding} onClose={() => setAdding(false)} />
    </Loadable>
  );
}

/** 文件名里只有一个年份时（「2024真题.pdf」）用它当这份真题的年份；「1997—2026」这类多年合集返回 undefined，按文件里的年份标题逐题识别。 */
export function yearFromName(name: string): number | undefined {
  const years = new Set(name.match(/(?<!\d)(?:19|20)\d{2}(?!\d)/g) ?? []);
  return years.size === 1 ? Number([...years][0]) : undefined;
}

function AddFile({ projectId, open, onClose }: { projectId: number; open: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const { message, modal } = AntApp.useApp();
  const [form] = Form.useForm();
  const kind = Form.useWatch('kind', form) as ImportFile['kind'] | undefined;
  const [files, setFiles] = useState<File[]>([]);
  const [progress, setProgress] = useState<string>();
  const close = () => {
    setFiles([]);
    onClose();
  };
  // 一个文件一条解析记录，逐个上传；某个文件失败不影响其他文件，最后汇总失败的。
  const uploadOne = async (v: FormValues, file?: File) => {
    const format = v.kind === 'table' ? 'csv' : file ? formatOf(file.name) : 'text';
    if (!format) throw new Error('只支持 PDF、Word（.docx）、图片或 CSV');
    const text = file && (format === 'csv' || format === 'text') ? await file.text() : v.text;
    const examYear = v.exam_year ?? (file && files.length > 1 ? yearFromName(file.name) : undefined);
    const created = await unwrap(
      api.POST('/admin/official/projects/{projectId}/imports', {
        params: { path: { projectId } },
        body: { kind: v.kind, format, file_name: file?.name, size: file?.size, text, exam_year: examYear, is_recollection: v.is_recollection },
      }),
    );
    if (created.upload_url && file) {
      await uploadTextbookFile(file, { upload_url: created.upload_url, upload_headers: created.upload_headers ?? {} });
      await unwrap(api.POST('/admin/official/projects/{projectId}/imports/{fileId}/confirm', { params: { path: { projectId, fileId: created.id } } }));
    }
  };
  const submit = async (v: FormValues) => {
    const bad = files.filter((f) => v.kind !== 'table' && !formatOf(f.name));
    if (bad.length) {
      message.error(`不支持的格式：${bad.map((f) => f.name).join('、')}。只支持 PDF、Word（.docx）、图片或 CSV`);
      return;
    }
    if (!files.length && !v.text?.trim()) {
      message.error('先选文件，或粘贴文字');
      return;
    }
    const queue: (File | undefined)[] = files.length ? files : [undefined];
    const failed: string[] = [];
    for (const [i, file] of queue.entries()) {
      if (queue.length > 1) setProgress(`上传中 ${i + 1}/${queue.length}`);
      else setProgress('上传中');
      try {
        await uploadOne(v, file);
      } catch (e) {
        failed.push(`${file?.name ?? '粘贴的文字'}：${e instanceof Error ? e.message : '上传失败'}`);
      }
    }
    setProgress(undefined);
    void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] });
    const ok = queue.length - failed.length;
    if (!failed.length) {
      message.success(queue.length > 1 ? `${ok} 个文件已开始解析` : '已开始解析');
      close();
      return;
    }
    if (ok > 0) close();
    modal.error({ title: ok > 0 ? `${ok} 个已开始解析，${failed.length} 个上传失败` : '上传失败', content: failed.map((f) => <div key={f}>{f}</div>) });
  };
  const multi = files.length > 1;
  return (
    <Modal open={open} title="上传资料" onCancel={close} onOk={() => form.submit()} confirmLoading={!!progress} okText={progress ?? '确定'} destroyOnClose>
      <Form form={form} layout="vertical" preserve={false} initialValues={{ kind: 'exam' }} onFinish={(v: FormValues) => void submit(v)}>
        <Form.Item name="kind" label="资料类型">
          <Select options={Object.entries(kindNames).map(([value, label]) => ({ value, label }))} />
        </Form.Item>
        {kind === 'exam' ? (
          <>
            <Space>
              <Form.Item name="exam_year" label="真题年份（选填）"><InputNumber min={1978} max={2100} /></Form.Item>
              <Form.Item name="is_recollection" valuePropName="checked" label=" "><Checkbox>回忆版</Checkbox></Form.Item>
            </Space>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: -12 }}>
              一个文件里有多个年份时留空，按文件里「1997年」「2005年硕士研究生入学考试试题」这样的年份标题逐题识别；只有整份都是同一年、文件里又没写年份时才填。
              {multi ? '选了多个文件时，留空则按文件名里的年份识别（如「2024真题.pdf」）。' : ''}
            </Typography.Paragraph>
          </>
        ) : null}
        <Form.Item label={kind === 'table' ? 'CSV 文件（Excel 另存为 CSV UTF-8）' : '文件（PDF、Word、照片，可多选）'}>
          <Upload
            multiple
            fileList={files.map((f, i) => ({ uid: `${i}-${f.name}`, name: f.name, status: 'done' as const }))}
            beforeUpload={(f) => {
              setFiles((prev) => (prev.some((p) => p.name === f.name && p.size === f.size) ? prev : [...prev, f]));
              return false;
            }}
            onRemove={(u) => setFiles((prev) => prev.filter((f, i) => `${i}-${f.name}` !== u.uid))}
            accept={kind === 'table' ? '.csv' : '.pdf,.docx,.jpg,.jpeg,.png,.webp'}
          >
            <Button>选择文件</Button>
          </Upload>
          {multi ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>已选 {files.length} 个文件，每个文件单独解析、单独核对</Typography.Text> : null}
        </Form.Item>
        {kind === 'table' ? (
          <Alert type="info" message="表头：年份、题号、题型、分值、题干、参考答案、采分点（每行一条：要点 + 分值）、知识点、出处、来源类型。题型、分值、题干必填，真题年份必填；缺答案、采分点时由 AI 补全。" />
        ) : files.length ? null : (
          <Form.Item name="text" label="或者直接粘贴文字">
            <Input.TextArea rows={6} placeholder="没有文件时可以粘贴真题或讲义原文" />
          </Form.Item>
        )}
      </Form>
    </Modal>
  );
}

type FormValues = { kind: ImportFile['kind']; text?: string; exam_year?: number; is_recollection?: boolean };

function Review({ projectId, fileId }: { projectId: number; fileId: number }) {
  const qc = useQueryClient();
  const { message } = AntApp.useApp();
  const [current, setCurrent] = useState<number>();
  const [editing, setEditing] = useState(false);
  const [year, setYear] = useState<number>();
  const detail = useQuery({
    queryKey: ['admin', 'official-import', projectId, fileId],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/imports/{fileId}', { params: { path: { projectId, fileId } } })),
  });
  const review = useMutation({
    mutationFn: (v: { itemId: number; decision: 'accept' | 'reject'; payload?: Record<string, unknown> }) =>
      unwrap(api.POST('/admin/official/projects/{projectId}/import-items/{itemId}/review', { params: { path: { projectId, itemId: v.itemId } }, body: { decision: v.decision, payload: v.payload } })),
    onSuccess: (_, v) => {
      message.success(v.decision === 'accept' ? '已接受，提交审核' : '已驳回');
      setEditing(false);
      void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] });
      const rest = (detail.data?.items ?? []).filter((i) => !year || yearOf(i) === year);
      const next = rest.find((i) => i.status === 'pending' && i.id !== v.itemId);
      setCurrent(next?.id);
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  const all = detail.data?.items ?? [];
  // 多年真题合集一份就有几百道题，按年份筛着核对
  const years = [...new Set(all.map(yearOf).filter((y): y is number => y !== undefined))].sort((a, b) => a - b);
  const items = year ? all.filter((i) => yearOf(i) === year) : all;
  const it = items.find((i) => i.id === current) ?? items.find((i) => i.status === 'pending') ?? items[0];
  const p = (it?.payload ?? {}) as QuestionPayload & Record<string, unknown>;
  const text = detail.data?.text ?? '';
  const src = it?.source_text ?? '';
  const at = src ? text.indexOf(src.slice(0, 40)) : -1;
  return (
    <Loadable loading={detail.isPending} error={detail.error}>
      <Row gutter={16}>
        <Col span={7}>
          <Card size="small" title={`${detail.data?.file.file_name ?? ''} · ${items.length} 条`} extra={<Typography.Text type="secondary">已核对 {items.filter((i) => i.status !== 'pending').length}</Typography.Text>}>
            {years.length > 1 ? (
              <Select
                allowClear
                placeholder={`全部年份（${years.length} 年）`}
                value={year}
                onChange={(y?: number) => { setYear(y); setCurrent(undefined); setEditing(false); }}
                options={years.map((y) => ({ value: y, label: `${y} 年 · ${all.filter((i) => yearOf(i) === y).length} 条` }))}
                style={{ width: '100%', marginBottom: 8 }}
              />
            ) : null}
            <List
              size="small"
              dataSource={items}
              style={{ maxHeight: 560, overflow: 'auto' }}
              renderItem={(i) => (
                <List.Item onClick={() => { setCurrent(i.id); setEditing(false); }} style={{ cursor: 'pointer', background: i.id === it?.id ? '#F7F7F5' : undefined, padding: '8px 10px' }}>
                  <Typography.Text ellipsis style={{ maxWidth: 190 }} delete={i.status === 'rejected'}>{i.seq}. {itemTitle(i)}</Typography.Text>
                  {i.status === 'pending' ? <Tag color={confidence[i.confidence].color}>{confidence[i.confidence].text}</Tag> : <Tag>{i.status === 'accepted' ? '已接受' : '已驳回'}</Tag>}
                </List.Item>
              )}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card size="small" title="原文" extra={it?.page ? <Typography.Text type="secondary">第 {it.page} 页</Typography.Text> : null}>
            <div style={{ maxHeight: 560, overflow: 'auto', whiteSpace: 'pre-wrap', lineHeight: 1.9, background: '#F7F7F5', padding: 16, borderRadius: 12 }}>
              {at >= 0 ? (
                <>
                  {text.slice(Math.max(0, at - 400), at)}
                  <mark style={{ background: '#FFE9A8' }}>{text.slice(at, at + src.length)}</mark>
                  {text.slice(at + src.length, at + src.length + 600)}
                </>
              ) : (
                src || text.slice(0, 2000)
              )}
            </div>
          </Card>
        </Col>
        <Col span={9}>
          {it ? (
            <Card
              size="small"
              title="AI 解析结果"
              extra={it.rubric_mismatch ? <Tag color="gold">采分点合计需核对</Tag> : <Tag color={confidence[it.confidence].color}>{confidence[it.confidence].text}</Tag>}
            >
              {editing ? (
                <PayloadEditor item={it} onCancel={() => setEditing(false)} onSave={(payload) => review.mutate({ itemId: it.id, decision: 'accept', payload })} loading={review.isPending} />
              ) : (
                <Space direction="vertical" style={{ width: '100%' }}>
                  {it.item_type === 'question' ? (
                    <>
                      <Typography.Text type="secondary">题型 · 分值</Typography.Text>
                      <Typography.Text>{qtypeNames[p.qtype] ?? p.qtype} · {p.score ?? '—'} 分{p.exam_year ? ` · ${p.exam_year} 真题` : ''}</Typography.Text>
                      <Typography.Text type="secondary">知识点</Typography.Text>
                      <Typography.Text>{p.kp_names?.join('、') || '（未归类）'}</Typography.Text>
                      <Typography.Text type="secondary">采分点</Typography.Text>
                      {(p.rubric ?? []).map((r, i) => <Typography.Text key={i}>{i + 1}. {r.content}（{r.score} 分）</Typography.Text>)}
                      {it.rubric_mismatch ? (
                        <Typography.Text type="danger">合计 {(p.rubric ?? []).reduce((s, r) => s + r.score, 0)}，分值 {p.score}，要先修改</Typography.Text>
                      ) : null}
                      <Typography.Text type="secondary">参考答案</Typography.Text>
                      <Typography.Paragraph>{p.answer || '—'}</Typography.Paragraph>
                      {it.citations?.length ? <Typography.Text type="secondary">引用：{it.citations.map((c) => `${c.title ?? ''}${c.heading ? ` ${c.heading}` : ''}`).join('；')}</Typography.Text> : null}
                    </>
                  ) : (
                    <Typography.Paragraph>
                      {String(p.section ?? '')} / {String(p.chapter ?? '')} / <b>{String(p.name ?? '')}</b>
                      <br />
                      {String(p.original_text ?? '')}
                    </Typography.Paragraph>
                  )}
                  {it.status === 'pending' ? (
                    <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
                      <Button onClick={() => review.mutate({ itemId: it.id, decision: 'reject' })} loading={review.isPending}>驳回</Button>
                      <Button onClick={() => setEditing(true)}>修改</Button>
                      <Button type="primary" disabled={it.rubric_mismatch} onClick={() => review.mutate({ itemId: it.id, decision: 'accept' })} loading={review.isPending}>接受，下一题</Button>
                    </Space>
                  ) : (
                    <Tag>{it.status === 'accepted' ? `已接受 · 草稿 ${it.draft_id ?? ''}` : '已驳回'}</Tag>
                  )}
                </Space>
              )}
            </Card>
          ) : (
            <Empty description="没有解析结果" />
          )}
        </Col>
      </Row>
    </Loadable>
  );
}

/** 修改后接受：题目改题型、分值、题干、答案、知识点与采分点；知识点改板块、章节、名称、原文。 */
function PayloadEditor({ item, onCancel, onSave, loading }: { item: Item; onCancel: () => void; onSave: (p: Record<string, unknown>) => void; loading: boolean }) {
  const [form] = Form.useForm();
  const init = item.payload as Record<string, unknown>;
  const score = Form.useWatch('score', form) as number | undefined;
  const rubric = (Form.useWatch('rubric', form) as Point[] | undefined) ?? [];
  const sum = rubric.reduce((s, r) => s + (r?.score ?? 0), 0);
  const q = item.item_type === 'question';
  return (
    <Form
      form={form}
      layout="vertical"
      initialValues={{ ...init, kp_names: (init.kp_names as string[] | undefined)?.join('、') }}
      onFinish={(v) => onSave({ ...init, ...v, ...(q ? { kp_names: String(v.kp_names ?? '').split(/[、,，]/).map((s: string) => s.trim()).filter(Boolean) } : {}) })}
    >
      {q ? (
        <>
          <Space>
            <Form.Item name="qtype" label="题型"><Select style={{ width: 120 }} options={Object.entries(qtypeNames).map(([value, label]) => ({ value, label }))} /></Form.Item>
            <Form.Item name="score" label="分值"><InputNumber min={0} max={300} /></Form.Item>
            <Form.Item name="exam_year" label="年份"><InputNumber min={1978} max={2100} /></Form.Item>
          </Space>
          <Form.Item name="stem" label="题干" rules={[{ required: true }]}><Input.TextArea rows={2} /></Form.Item>
          <Form.Item name="kp_names" label="知识点（多个用顿号分隔）"><Input /></Form.Item>
          <Typography.Text type={score !== undefined && Math.abs(sum - score) > 0.01 ? 'danger' : 'secondary'}>采分点合计 {sum}{score !== undefined ? `，分值 ${score}` : ''}</Typography.Text>
          <Form.List name="rubric">
            {(fields, { add, remove }) => (
              <>
                {fields.map((f) => (
                  <Space key={f.key} style={{ display: 'flex', marginTop: 6 }}>
                    <Form.Item name={[f.name, 'content']} noStyle><Input style={{ width: 240 }} /></Form.Item>
                    <Form.Item name={[f.name, 'score']} noStyle><InputNumber min={0} step={0.5} /></Form.Item>
                    <Button size="small" onClick={() => remove(f.name)}>删</Button>
                  </Space>
                ))}
                <Button size="small" style={{ marginTop: 6 }} onClick={() => add({ content: '', score: 0 })}>+ 采分点</Button>
              </>
            )}
          </Form.List>
          <Form.Item name="answer" label="参考答案" style={{ marginTop: 12 }}><Input.TextArea rows={4} /></Form.Item>
        </>
      ) : (
        <>
          <Form.Item name="section" label="板块"><Input /></Form.Item>
          <Form.Item name="chapter" label="章节"><Input /></Form.Item>
          <Form.Item name="name" label="知识点"><Input /></Form.Item>
          <Form.Item name="original_text" label="原文表述"><Input.TextArea rows={4} /></Form.Item>
        </>
      )}
      <Space>
        <Button onClick={onCancel}>取消</Button>
        <Button type="primary" htmlType="submit" loading={loading}>修改后接受</Button>
      </Space>
    </Form>
  );
}
