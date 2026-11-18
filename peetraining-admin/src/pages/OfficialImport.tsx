// 7.17 AI 解析导入（PRD v3 增补，设计稿 n_admin_import）。T54 起不再单独成页，放进项目工作台的「导入真题与核对」与「知识框架」两步；
// 上传资料并标类型（真题卷 / 表格导入；参考书目录在知识框架一步，讲义放官方资料库），
// 或粘贴文字、表格导入（CSV）；文件卡显示解析状态和结果数；核对区三栏：左侧题目列表与置信度和已核对数，中间原文并高亮当前题，
// 右侧解析结果（题型分值、知识点归属、采分点、参考答案、解析、出处及引用来源；T56 标知识点对不上、同名知识点修订）；采分点合计不等于分值时标红；逐条「接受、修改、驳回」，
// 接受的提交 7.13 审核。只解析团队上传的资料，与用户端资料完全隔离。
// T49：缺答案、采分点的题解析后由编辑点「生成答案 / 提取采分点」或整份「一键补全」，上传时可勾「缺答案时由 AI 生成」；
// AI 生成的答案标「AI 生成」（发布后用户端显示「AI 参考答案」），参考同题型填的分值标「分值待核对」，引用显示教材原文与页码。
// 左侧列表可勾选多题「批量接受」或「批量重新生成」；不能直接接受的题（缺采分点、采分点合计不对、重复）批量接受时跳过。
import { GroupFields, groupInit, groupOut, GroupTag, type QuestionGroup } from '../components/QuestionGroup';
import type { Schemas } from '@/api-client';
import { ApiError } from '@/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, App as AntApp, Button, Card, Checkbox, Col, Empty, Form, Input, InputNumber, List, Modal, Row, Select, Space, Tag, Typography, Upload } from 'antd';
import { useEffect, useRef, useState } from 'react';
import { Loadable } from '../components/Page';
import { api, unwrap } from '../lib/api';
import { uploadTextbookFile } from '../lib/textbookUpload';

type ImportFile = Schemas['OfficialImportFile'];
type Item = Schemas['OfficialImportItem'];
type Point = { content: string; score: number };
type QuestionPayload = {
  qtype: string;
  stem: string;
  answer?: string;
  score?: number;
  exam_year?: number;
  kp_names?: string[];
  rubric?: Point[];
  /** 参考答案由 AI 生成（发布后用户端显示「AI 参考答案」） */
  answer_ai?: boolean;
  /** 原文没有分值，参考同项目同题型的给分填上的 */
  score_inferred?: boolean;
  /** 限选题（按专业分组）的组名与适用专业；没有是必答题（T51） */
  group?: QuestionGroup;
  /** 解析（T56） */
  analysis?: string;
  /** 出处，发布后展示给用户（T56） */
  reference?: string;
};

/** 知识点名称：多个用「；」隔开，第一个是主知识点（与后端表格导入一致；名称里可能有顿号，不按顿号拆，T56）。 */
export function splitKPNames(v: string) {
  return [...new Set(v.split(/[；;\n]/).map((s) => s.trim()).filter(Boolean))];
}
type Citation = NonNullable<Item['citations']>[number];

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
  term: '名词解释', short_answer: '简答', discrimination: '辨析', discussion: '论述', analytical_discussion: '分析论述',
  material_analysis: '材料分析', essay: '作文', single_choice: '单选', multi_choice: '多选', true_false: '判断', fill_blank: '填空', calculation: '计算', other: '其他',
};

/** 主观题（与后端 ai.Subjective 一致）：要有采分点才能接受。 */
export function isSubjective(qtype: string) {
  return ['term', 'short_answer', 'discussion', 'essay', 'discrimination', 'material_analysis', 'analytical_discussion'].includes(qtype);
}

/** 补全按钮的文字：缺答案的生成答案（主观题连同采分点），有答案缺采分点的提取采分点。 */
export function fillLabel(p: { answer?: string }) {
  return p.answer?.trim() ? '提取采分点' : '生成答案';
}

/** 一键补全的进度文字。 */
export function fillProgress(f: Pick<ImportFile, 'fill'>) {
  const fill = f.fill;
  if (!fill || fill.status !== 'running') return undefined;
  return `补全中 ${fill.done}/${fill.total}${fill.failed ? `，失败 ${fill.failed}` : ''}`;
}

/** 引用的出处：书名 · 章节 · 页码。 */
/** 引用出处；考纲、经验总结在前面标明类型（T55）。 */
export function citationSource(c: Citation) {
  const page = c.printed_page ? `第 ${c.printed_page} 页` : c.page ? `第 ${c.page} 页` : '';
  const kind = c.material === 'syllabus' ? '对应考纲' : c.material === 'experience' ? '经验总结' : '';
  return [kind, c.title, c.heading, page].filter(Boolean).join(' · ');
}

/** 文件格式按扩展名判断。 */
export function formatOf(name: string): ImportFile['format'] | undefined {
  const ext = name.toLowerCase().split('.').pop() ?? '';
  if (ext === 'pdf') return 'pdf';
  if (ext === 'docx') return 'docx';
  if (['jpg', 'jpeg', 'png', 'webp', 'heic'].includes(ext)) return 'image';
  if (ext === 'csv') return 'csv';
  if (ext === 'md' || ext === 'markdown') return 'md';
  return undefined;
}

const supported = '只支持 PDF、Word（.docx）、Markdown（.md）、图片或 CSV';

/** 已有同一年份真题卷的文件（解析失败的不算）：上传前提醒，可能是重复上传（T52）。 */
export function sameYearExams(existing: ImportFile[], years: (number | undefined)[]) {
  const want = new Set(years.filter((y): y is number => !!y));
  return existing.filter((f) => f.kind === 'exam' && f.status !== 'failed' && f.exam_year && want.has(f.exam_year));
}

/** 一条解析结果的真题年份。 */
function yearOf(it: Item): number | undefined {
  const y = (it.payload as Record<string, unknown>).exam_year;
  return typeof y === 'number' ? y : undefined;
}

/** 能不能直接批量接受：还没核对；主观题要有采分点、合计等于分值；标为重复、还缺答案的不批量接受。 */
export function canBatchAccept(it: Item) {
  // 知识点对不上的不批量接受，逐条看（T56：不处理的话发布前检查不通过）
  if (it.status !== 'pending' || it.rubric_mismatch || it.duplicate_of || it.needs_fill || it.filling || it.missing_kps?.length) return false;
  const p = it.payload as QuestionPayload;
  return !(it.item_type === 'question' && isSubjective(p.qtype) && !(p.rubric ?? []).length);
}

/** 一条解析结果的标题。 */
export function itemTitle(it: Item) {
  const p = it.payload as Record<string, unknown>;
  if (it.item_type === 'knowledge_point') return `知识点：${String(p.name ?? '')}`;
  const year = typeof p.exam_year === 'number' ? `${p.exam_year} · ` : '';
  return `${year}${qtypeNames[String(p.qtype)] ?? ''}：${String(p.stem ?? '')}`;
}

/** 上传框可选的资料类型（T54：工作台里真题与表格在「导入真题与核对」，参考书目录在「知识框架」）。 */
export type ImportKind = ImportFile['kind'];

/** 项目的资料列表与核对区（工作台「导入真题与核对」一步）。fileId 不传时自己记当前打开的资料。 */
export function ImportSection({ projectId, fileId, onFile, kinds = ['exam', 'table'] }: { projectId: number; fileId?: number; onFile?: (id: number) => void; kinds?: ImportKind[] }) {
  const [own, setOwn] = useState<number>();
  return (
    <>
      <ProjectImports projectId={projectId} fileId={fileId ?? own} onFile={onFile ?? setOwn} kinds={kinds} />
      <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>后台只解析团队上传的资料，与用户上传的资料完全隔离。核对完成的内容提交到审核队列。</Typography.Paragraph>
    </>
  );
}

/** 上传资料弹窗（知识框架一步用来上传参考书目录）。 */
export function UploadMaterial(props: { projectId: number; open: boolean; onClose: () => void; kinds: ImportKind[] }) {
  const files = useQuery({
    queryKey: ['admin', 'official-import', props.projectId],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/imports', { params: { path: { projectId: props.projectId } } })),
  });
  return <AddFile {...props} existing={files.data?.items ?? []} />;
}

function ProjectImports({ projectId, fileId, onFile, kinds }: { projectId: number; fileId?: number; onFile: (id: number) => void; kinds: ImportKind[] }) {
  const qc = useQueryClient();
  const { message, modal } = AntApp.useApp();
  const [adding, setAdding] = useState(false);
  const files = useQuery({
    queryKey: ['admin', 'official-import', projectId],
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/imports', { params: { path: { projectId } } })),
    refetchInterval: (q) => (q.state.data?.items.some((f) => f.status === 'parsing' || f.fill?.status === 'running') ? 5000 : false),
  });
  const confirm = useMutation({
    mutationFn: (id: number) => unwrap(api.POST('/admin/official/projects/{projectId}/imports/{fileId}/confirm', { params: { path: { projectId, fileId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] }),
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  const remove = useMutation({
    mutationFn: (id: number) => unwrap(api.DELETE('/admin/official/projects/{projectId}/imports/{fileId}', { params: { path: { projectId, fileId: id } } })),
    onSuccess: () => {
      message.success('已删除');
      // 草稿、审核队列里随之删掉的条目也要刷新。
      void qc.invalidateQueries({ queryKey: ['admin'] });
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '删除失败'),
  });
  const askRemove = (f: ImportFile) =>
    modal.confirm({
      title: `删除「${f.file_name}」？`,
      content: '这份资料解析出的题会一起删除，已经接受、进了审核队列的题也会从审核队列里删掉，不能恢复。已有题发布到题库的不能删。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: () => remove.mutateAsync(f.id).catch(() => undefined),
    });
  const list = files.data?.items ?? [];
  // 打开的资料被删掉后不再显示它的核对区。
  const current = list.some((f) => f.id === fileId) ? fileId : undefined;
  return (
    <Loadable loading={files.isPending} error={files.error}>
      <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
        {list.map((f) => (
          <Col span={6} key={f.id}>
            <Card size="small" hoverable onClick={() => f.status === 'parsed' && onFile(f.id)} style={current === f.id ? { borderColor: '#1A1A1A' } : undefined}>
              <Space direction="vertical" size={2}>
                <Typography.Text strong ellipsis style={{ maxWidth: 220 }}>{f.file_name}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {kindNames[f.kind]} · <Tag color={statusText[f.status].color}>{statusText[f.status].text}</Tag>
                  {f.status === 'parsed' ? (f.kind === 'catalog' ? `知识点 ${f.kp_count} 个` : `${f.question_count} 题`) : null}
                </Typography.Text>
                {f.status === 'parsed' ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>已核对 {f.reviewed_count}</Typography.Text> : null}
                {fillProgress(f) ? <Tag color="blue">{fillProgress(f)}</Tag> : null}
                {f.auto_answer ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>解析时已由 AI 补答案</Typography.Text> : null}
                {f.fail_reason ? <Typography.Text type="danger" style={{ fontSize: 12 }}>{f.fail_reason}</Typography.Text> : null}
                <Space size={4}>
                  {f.status === 'failed' ? <Button size="small" onClick={(e) => { e.stopPropagation(); confirm.mutate(f.id); }}>重新解析</Button> : null}
                  <Button size="small" danger type="text" aria-label={`删除${f.file_name}`} onClick={(e) => { e.stopPropagation(); askRemove(f); }}>删除</Button>
                </Space>
              </Space>
            </Card>
          </Col>
        ))}
        <Col span={6}>
          <Card size="small" hoverable onClick={() => setAdding(true)} style={{ borderStyle: 'dashed' }}>
            <Typography.Text strong>+ 上传资料</Typography.Text>
            <br />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>{kinds.map((k) => kindNames[k]).join('、')}；解析不理想时可改用表格导入</Typography.Text>
          </Card>
        </Col>
      </Row>
      {current ? <Review projectId={projectId} fileId={current} /> : <Empty description="点一份已解析的资料开始核对" />}
      <AddFile projectId={projectId} open={adding} onClose={() => setAdding(false)} existing={list} kinds={kinds} />
    </Loadable>
  );
}

/** 文件名里只有一个年份时（「2024真题.pdf」）用它当这份真题的年份；「1997—2026」这类多年合集返回 undefined，按文件里的年份标题逐题识别。 */
/** 项目关联的教材里有没有能检索的（已发布、授权有效、有索引，T53）。 */
export function hasUsableTextbook(items: { available: boolean }[]) {
  return items.some((b) => b.available);
}

export function yearFromName(name: string): number | undefined {
  const years = new Set(name.match(/(?<!\d)(?:19|20)\d{2}(?!\d)/g) ?? []);
  return years.size === 1 ? Number([...years][0]) : undefined;
}

function AddFile({ projectId, open, onClose, existing, kinds }: { projectId: number; open: boolean; onClose: () => void; existing: ImportFile[]; kinds: ImportKind[] }) {
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
  const yearFor = (v: FormValues, file?: File) => v.exam_year ?? (file && files.length > 1 ? yearFromName(file.name) : undefined);
  // 一个文件一条解析记录，逐个上传；某个文件失败不影响其他文件，最后汇总失败的。
  const uploadOne = async (v: FormValues, file?: File) => {
    const format = v.kind === 'table' ? 'csv' : file ? formatOf(file.name) : 'text';
    if (!format) throw new Error(supported);
    // CSV 与 Markdown 是纯文字，直接带内容提交，不用传到 OSS（T52）
    const inline = !file || format === 'csv' || format === 'text' || format === 'md';
    const text = file && inline ? await file.text() : v.text;
    const examYear = yearFor(v, file);
    const created = await unwrap(
      api.POST('/admin/official/projects/{projectId}/imports', {
        params: { path: { projectId } },
        body: { kind: v.kind, format, file_name: file?.name, size: inline ? undefined : file?.size, text, exam_year: examYear, is_recollection: v.is_recollection, auto_answer: !!v.auto_answer },
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
      message.error(`不支持的格式：${bad.map((f) => f.name).join('、')}。${supported}`);
      return;
    }
    if (!files.length && !v.text?.trim()) {
      message.error('先选文件，或粘贴文字');
      return;
    }
    const queue: (File | undefined)[] = files.length ? files : [undefined];
    // T53 勾了 AI 补答案、项目却没有能检索的关联教材时先提醒：答案只能按通用知识生成。
    if (v.auto_answer && v.kind !== 'catalog') {
      const books = await qc
        .fetchQuery({ queryKey: ['admin', 'project-textbooks', projectId], queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/textbooks', { params: { path: { projectId } } })) })
        .catch(() => undefined);
      if (books && !hasUsableTextbook(books.items)) {
        const go = await modal.confirm({
          title: books.items.length ? '关联的教材都还不能检索' : '本项目还没有关联教材',
          content: '现在上传的话，AI 只能按通用知识生成答案。可以先到「官方题库」打开项目，在「关联教材」里关联已发布的教材；之后也可以在核对区「按关联教材重新生成」。',
          okText: '仍然上传',
          cancelText: '先去关联',
        });
        if (!go) return;
      }
    }
    if (v.kind === 'exam') {
      const dup = sameYearExams(existing, queue.map((f) => yearFor(v, f)));
      if (dup.length) {
        const go = await modal.confirm({
          title: '这一年的真题已经传过',
          content: (
            <>
              {dup.map((f) => <div key={f.id}>{f.exam_year} 年：《{f.file_name}》（{f.question_count} 题）</div>)}
              <div style={{ marginTop: 8 }}>继续上传的话，解析出的重复题会在核对区标「重复」，可以一键驳回。</div>
            </>
          ),
          okText: '继续上传',
          cancelText: '取消',
        });
        if (!go) return;
      }
    }
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
      <Form form={form} layout="vertical" preserve={false} initialValues={{ kind: kinds[0] }} onFinish={(v: FormValues) => void submit(v)}>
        <Form.Item name="kind" label="资料类型" hidden={kinds.length === 1}>
          <Select options={kinds.map((value) => ({ value, label: kindNames[value] }))} />
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
        <Form.Item label={kind === 'table' ? 'CSV 文件（Excel 另存为 CSV UTF-8）' : '文件（PDF、Word、Markdown、照片，可多选）'}>
          <Upload
            multiple
            fileList={files.map((f, i) => ({ uid: `${i}-${f.name}`, name: f.name, status: 'done' as const }))}
            beforeUpload={(f) => {
              setFiles((prev) => (prev.some((p) => p.name === f.name && p.size === f.size) ? prev : [...prev, f]));
              return false;
            }}
            onRemove={(u) => setFiles((prev) => prev.filter((f, i) => `${i}-${f.name}` !== u.uid))}
            accept={kind === 'table' ? '.csv' : '.pdf,.docx,.md,.markdown,.jpg,.jpeg,.png,.webp'}
          >
            <Button>选择文件</Button>
          </Upload>
          {multi ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>已选 {files.length} 个文件，每个文件单独解析、单独核对</Typography.Text> : null}
        </Form.Item>
        {kind === 'table' ? (
          <Alert
            type="info"
            message="题目表表头：年份、题号、题型、分值、题干、参考答案、解析、采分点（每行一条：要点 + 分值）、知识点（多个用「；」隔开）、出处、答案来源（AI生成 / 团队）、来源类型、限选分组、适用专业。题型、分值、题干必填，真题年份必填。"
            description="知识点表表头：板块、章节、知识点、原文表述、采分点、出处，一行一个知识点。建议先导知识点表，题目才能对上知识点；出处发布后展示给用户。"
          />
        ) : files.length ? null : (
          <Form.Item name="text" label="或者直接粘贴文字">
            <Input.TextArea rows={6} placeholder="没有文件时可以粘贴真题或讲义原文（Markdown 也可以，排版标记会自动去掉）" />
          </Form.Item>
        )}
        {kind === 'catalog' ? null : (
          <Form.Item name="auto_answer" valuePropName="checked" style={{ marginTop: 12, marginBottom: 0 }}>
            <Checkbox>缺答案时由 AI 生成</Checkbox>
          </Form.Item>
        )}
        {kind === 'catalog' ? null : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            勾选后解析时就补答案和采分点，解析会慢一些；不勾时解析完在核对区逐题「生成答案 / 提取采分点」，或点「一键补全」
          </Typography.Text>
        )}
      </Form>
    </Modal>
  );
}

type FormValues = { kind: ImportFile['kind']; text?: string; exam_year?: number; is_recollection?: boolean; auto_answer?: boolean };

function Review({ projectId, fileId }: { projectId: number; fileId: number }) {
  const qc = useQueryClient();
  const { message, modal } = AntApp.useApp();
  const [current, setCurrent] = useState<number>();
  const [editing, setEditing] = useState(false);
  const [year, setYear] = useState<number>();
  // 左侧勾选的题与批量操作的进度
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [batch, setBatch] = useState<{ label: string; done: number; total: number }>();
  const detailKey = ['admin', 'official-import', projectId, fileId];
  const detail = useQuery({
    queryKey: detailKey,
    queryFn: () => unwrap(api.GET('/admin/official/projects/{projectId}/imports/{fileId}', { params: { path: { projectId, fileId } } })),
    // 一键补全、单题生成都在后台排队执行，进行中每 5 秒刷新一次
    refetchInterval: (q) => (q.state.data?.file.fill?.status === 'running' || q.state.data?.items.some((i) => i.filling) ? 5000 : false),
  });
  // 单题生成完（或失败）时提示一下，编辑可能已经切到别的题了
  const fillingIds = useRef<Set<number>>(new Set());
  useEffect(() => {
    const items = detail.data?.items;
    if (!items) return;
    for (const i of items) {
      if (!fillingIds.current.has(i.id) || i.filling) continue;
      if (i.fill_error) message.error(`第 ${i.seq} 题没生成出来：${i.fill_error}`);
      else message.success(`第 ${i.seq} 题已生成，请核对`);
    }
    fillingIds.current = new Set(items.filter((i) => i.filling).map((i) => i.id));
  }, [detail.data, message]);
  const queuedMsg = '已开始生成，大约要一两分钟，可以先核对别的题，生成完会自动显示';
  const fillItem = useMutation({
    mutationFn: (itemId: number) => unwrap(api.POST('/admin/official/projects/{projectId}/import-items/{itemId}/fill', { params: { path: { projectId, itemId } } })),
    onSuccess: (saved) => {
      qc.setQueryData(detailKey, (old: typeof detail.data) => (old ? { ...old, items: old.items.map((i) => (i.id === saved.id ? saved : i)) } : old));
      if (saved.filling) message.info(queuedMsg);
      else message.success((saved.payload as QuestionPayload).answer_ai ? '已生成答案和采分点，请核对' : '已提取采分点，请核对');
    },
    onError: (e) => {
      message.error(e instanceof ApiError ? e.message : '生成失败，请稍后再试');
      void qc.invalidateQueries({ queryKey: detailKey }); // 失败原因记在条目上
    },
  });
  const rejectDup = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/official/projects/{projectId}/imports/{fileId}/reject-duplicates', { params: { path: { projectId, fileId } } })),
    onSuccess: (r) => {
      message.success(`已驳回 ${r.rejected} 道重复题`);
      setCurrent(undefined);
      void qc.invalidateQueries({ queryKey: detailKey });
      void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] });
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  const regenItem = useMutation({
    mutationFn: (itemId: number) => unwrap(api.POST('/admin/official/projects/{projectId}/import-items/{itemId}/regenerate', { params: { path: { projectId, itemId } } })),
    onSuccess: (saved) => {
      qc.setQueryData(detailKey, (old: typeof detail.data) => (old ? { ...old, items: old.items.map((i) => (i.id === saved.id ? saved : i)) } : old));
      if (saved.filling) message.info(queuedMsg);
      else message.success(saved.citations?.length ? '已重新生成，答案引用了关联教材，请核对' : '已重新生成，仍没有引用教材，原因见答案下方');
    },
    onError: (e) => {
      message.error(e instanceof ApiError ? e.message : '生成失败，请稍后再试');
      void qc.invalidateQueries({ queryKey: detailKey });
    },
  });
  const regenAll = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/official/projects/{projectId}/imports/{fileId}/regenerate', { params: { path: { projectId, fileId } } })),
    onSuccess: () => {
      message.success('已开始重新生成，可以先核对别的题');
      void qc.invalidateQueries({ queryKey: detailKey });
      void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] });
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
  });
  const fillAll = useMutation({
    mutationFn: () => unwrap(api.POST('/admin/official/projects/{projectId}/imports/{fileId}/fill', { params: { path: { projectId, fileId } } })),
    onSuccess: () => {
      message.success('已开始一键补全，可以先核对别的题');
      void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] });
    },
    onError: (e) => message.error(e instanceof ApiError ? e.message : '操作失败'),
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
  // 批量操作逐题调用单题接口（每题各自一个事务），一题失败不影响其余，最后汇总。
  const runBatch = async (label: string, ids: number[], call: (id: number) => Promise<unknown>, doneText = '都已完成') => {
    const failed: string[] = [];
    setBatch({ label, done: 0, total: ids.length });
    for (const [i, id] of ids.entries()) {
      try {
        await call(id);
        setSelected((s) => { const n = new Set(s); n.delete(id); return n; });
      } catch (e) {
        const seq = all.find((x) => x.id === id)?.seq;
        failed.push(`第 ${seq ?? id} 题：${e instanceof ApiError ? e.message : '操作失败'}`);
      }
      setBatch({ label, done: i + 1, total: ids.length });
    }
    setBatch(undefined);
    setCurrent(undefined);
    setEditing(false);
    void qc.invalidateQueries({ queryKey: detailKey });
    void qc.invalidateQueries({ queryKey: ['admin', 'official-import', projectId] });
    if (failed.length) {
      modal.warning({ title: `${label}：${ids.length - failed.length} 题成功，${failed.length} 题失败`, content: failed.map((f) => <div key={f}>{f}</div>) });
    } else {
      message.success(`${label}：${ids.length} 题${doneText}`);
    }
  };
  const all = detail.data?.items ?? [];
  // 多年真题合集一份就有几百道题，按年份筛着核对
  const years = [...new Set(all.map(yearOf).filter((y): y is number => y !== undefined))].sort((a, b) => a - b);
  const items = year ? all.filter((i) => yearOf(i) === year) : all;
  const it = items.find((i) => i.id === current) ?? items.find((i) => i.status === 'pending') ?? items[0];
  const p = (it?.payload ?? {}) as QuestionPayload & Record<string, unknown>;
  const file = detail.data?.file;
  const filling = file?.fill?.status === 'running';
  const toFill = all.filter((i) => i.status === 'pending' && i.needs_fill).length;
  const dups = all.filter((i) => i.status === 'pending' && i.duplicate_of).length;
  const toRegen = all.filter((i) => i.status === 'pending' && i.answer_ai).length;
  const pendingHere = items.filter((i) => i.status === 'pending');
  const picked = pendingHere.filter((i) => selected.has(i.id));
  const pickedAccept = picked.filter(canBatchAccept);
  const pickedRegen = picked.filter((i) => i.answer_ai && !i.filling);
  const busy = !!batch;
  const batchAccept = () => {
    const skipped = picked.length - pickedAccept.length;
    const noTextbook = pickedAccept.filter((i) => i.answer_basis).length;
    modal.confirm({
      title: `接受 ${pickedAccept.length} 道题并提交审核？`,
      content: (
        <>
          {skipped ? <div>另有 {skipped} 道缺采分点、采分点合计不对或标为重复，不能直接接受，已跳过。</div> : null}
          {noTextbook ? <div>其中 {noTextbook} 道的 AI 答案没有引用教材，建议先「批量重新生成」再接受。</div> : null}
        </>
      ),
      okText: '批量接受',
      onOk: () => {
        void runBatch('批量接受', pickedAccept.map((i) => i.id), (id) =>
          unwrap(api.POST('/admin/official/projects/{projectId}/import-items/{itemId}/review', { params: { path: { projectId, itemId: id } }, body: { decision: 'accept' } })),
        );
      },
    });
  };
  const batchRegen = () =>
    modal.confirm({
      title: `按关联教材重新生成 ${pickedRegen.length} 道题的 AI 答案？`,
      content: '在后台逐题生成，每题一两分钟，生成完自动显示。只换 AI 生成的答案和采分点；某道题生成失败时保留原来的答案。',
      okText: '重新生成',
      onOk: () => {
        void runBatch('批量重新生成', pickedRegen.map((i) => i.id), (id) =>
          unwrap(api.POST('/admin/official/projects/{projectId}/import-items/{itemId}/regenerate', { params: { path: { projectId, itemId: id } } })),
          '已开始生成，生成完会自动显示',
        );
      },
    });
  // 主观题没有采分点时不能接受，先生成
  const blocked = it?.item_type === 'question' && isSubjective(p.qtype) && !(p.rubric ?? []).length;
  const text = detail.data?.text ?? '';
  const src = it?.source_text ?? '';
  const at = src ? text.indexOf(src.slice(0, 40)) : -1;
  return (
    <Loadable loading={detail.isPending} error={detail.error}>
      <Row gutter={16}>
        <Col span={7}>
          <Card size="small" title={`${detail.data?.file.file_name ?? ''} · ${items.length} 条`} extra={<Typography.Text type="secondary">已核对 {items.filter((i) => i.status !== 'pending').length}</Typography.Text>}>
            {filling ? (
              <Alert type="info" showIcon style={{ marginBottom: 8 }} message={fillProgress(file!)} description="在后台补答案和采分点，可以先核对别的题" />
            ) : toFill > 0 ? (
              <Button block style={{ marginBottom: 8 }} loading={fillAll.isPending} onClick={() => fillAll.mutate()}>
                一键补全（{toFill} 题缺答案或采分点）
              </Button>
            ) : null}
            {!filling && toRegen > 0 ? (
              <Button
                block
                style={{ marginBottom: 8 }}
                loading={regenAll.isPending}
                onClick={() =>
                  modal.confirm({
                    title: `按关联教材重新生成 ${toRegen} 道题的 AI 答案？`,
                    content: '只换 AI 生成的答案和采分点；原文给的答案不动，原文给的采分点保留。某道题生成失败时保留原来的答案。',
                    okText: '重新生成',
                    onOk: () => regenAll.mutateAsync().then(() => undefined),
                  })
                }
              >
                按关联教材重新生成（{toRegen} 题 AI 答案）
              </Button>
            ) : null}
            {dups > 0 ? (
              <Button block danger style={{ marginBottom: 8 }} loading={rejectDup.isPending} onClick={() => rejectDup.mutate()}>
                驳回重复题（{dups} 题）
              </Button>
            ) : null}
            {years.length > 1 ? (
              <Select
                allowClear
                placeholder={`全部年份（${years.length} 年）`}
                value={year}
                onChange={(y?: number) => { setYear(y); setCurrent(undefined); setEditing(false); setSelected(new Set()); }}
                options={years.map((y) => ({ value: y, label: `${y} 年 · ${all.filter((i) => yearOf(i) === y).length} 条` }))}
                style={{ width: '100%', marginBottom: 8 }}
              />
            ) : null}
            {batch ? (
              <Alert type="info" showIcon style={{ marginBottom: 8 }} message={`${batch.label}中 ${batch.done}/${batch.total}`} description="逐题处理，请不要关闭页面" />
            ) : pendingHere.length ? (
              <Space wrap style={{ marginBottom: 8 }}>
                <Checkbox
                  checked={picked.length > 0 && picked.length === pendingHere.length}
                  indeterminate={picked.length > 0 && picked.length < pendingHere.length}
                  onChange={(e) => setSelected(new Set(e.target.checked ? pendingHere.map((i) => i.id) : []))}
                >
                  全选待核对（已选 {picked.length}）
                </Checkbox>
                <Button size="small" type="primary" disabled={!pickedAccept.length} onClick={batchAccept}>
                  批量接受（{pickedAccept.length}）
                </Button>
                <Button size="small" disabled={!pickedRegen.length || filling} onClick={batchRegen}>
                  批量重新生成（{pickedRegen.length}）
                </Button>
              </Space>
            ) : null}
            <List
              size="small"
              dataSource={items}
              style={{ maxHeight: 560, overflow: 'auto' }}
              renderItem={(i) => (
                <List.Item onClick={() => { setCurrent(i.id); setEditing(false); }} style={{ cursor: 'pointer', background: i.id === it?.id ? '#F7F7F5' : undefined, padding: '8px 10px' }}>
                  <Space size={6}>
                    {i.status === 'pending' ? (
                      <Checkbox
                        aria-label={`选择第 ${i.seq} 题`}
                        checked={selected.has(i.id)}
                        disabled={busy}
                        onClick={(e) => e.stopPropagation()}
                        onChange={(e) => setSelected((s) => { const n = new Set(s); if (e.target.checked) n.add(i.id); else n.delete(i.id); return n; })}
                      />
                    ) : null}
                    <Typography.Text ellipsis style={{ maxWidth: 170 }} delete={i.status === 'rejected'}>{i.seq}. {itemTitle(i)}</Typography.Text>
                  </Space>
                  {i.status === 'pending' ? (
                    <Space size={2}>
                      <GroupTag group={(i.payload as QuestionPayload).group} />
                      {i.duplicate_of ? <Tag color="red">重复</Tag> : null}
                      {i.missing_kps?.length ? <Tag color="red">知识点缺</Tag> : null}
                      {i.existing_kp ? <Tag color="blue">修订</Tag> : null}
                      {i.filling ? <Tag color="processing">生成中</Tag> : i.needs_fill ? <Tag>待补</Tag> : null}
                      <Tag color={confidence[i.confidence].color}>{confidence[i.confidence].text}</Tag>
                    </Space>
                  ) : (
                    <Tag>{i.status === 'accepted' ? '已接受' : '已驳回'}</Tag>
                  )}
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
                  {it.status === 'pending' && it.duplicate_of ? (
                    <Alert type="error" showIcon message={`重复：${it.duplicate_of}`} description="同一年份的同一道题，一般直接驳回" />
                  ) : null}
                  {it.status === 'pending' && it.missing_kps?.length ? (
                    <Alert
                      type="warning"
                      showIcon
                      message={`知识点「${it.missing_kps.join('」「')}」项目里还没有`}
                      description="先导入知识点表，或在「修改」里改成已有的知识点；不处理的话发布前检查不通过"
                    />
                  ) : null}
                  {it.status === 'pending' && it.existing_kp ? (
                    <Alert type="info" showIcon message="题库里已有同名知识点" description="接受后修订原知识点（不重复新建），已添加题库的用户收到更新" />
                  ) : null}
                  {it.seen_years?.length ? (
                    <Alert type="info" showIcon message={`${it.seen_years.join('、')} 年也考过这道题`} description="不同年份重考不算重复，照常核对" />
                  ) : null}
                  {it.item_type === 'question' ? (
                    <>
                      <Typography.Text type="secondary">题型 · 分值</Typography.Text>
                      <Typography.Text>
                        {qtypeNames[p.qtype] ?? p.qtype} · {p.score ?? '—'} 分{p.exam_year ? ` · ${p.exam_year} 真题` : ''}
                        {p.score_inferred ? <Tag color="gold" style={{ marginLeft: 8 }}>分值待核对</Tag> : null}
                      </Typography.Text>
                      {p.score_inferred ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>原文没有分值，参考同项目同题型的给分填上的</Typography.Text> : null}
                      <Typography.Text type="secondary">必答 / 限选</Typography.Text>
                      <Typography.Text>{p.group ? `限选 · ${p.group.label}${p.group.majors ? `（${p.group.majors}专业的考生作答）` : ''}` : '必答'}</Typography.Text>
                      <Typography.Text type="secondary">知识点</Typography.Text>
                      <Typography.Text>{p.kp_names?.join('；') || '（未归类）'}</Typography.Text>
                      <Typography.Text type="secondary">采分点</Typography.Text>
                      {(p.rubric ?? []).map((r, i) => <Typography.Text key={i}>{i + 1}. {r.content}（{r.score} 分）</Typography.Text>)}
                      {it.rubric_mismatch ? (
                        <Typography.Text type="danger">合计 {(p.rubric ?? []).reduce((s, r) => s + r.score, 0)}，分值 {p.score}，要先修改</Typography.Text>
                      ) : null}
                      <Typography.Text type="secondary">
                        参考答案
                        {p.answer_ai ? <Tag color="purple" style={{ marginLeft: 8 }}>AI 生成</Tag> : p.answer ? <Tag style={{ marginLeft: 8 }}>团队答案</Tag> : null}
                      </Typography.Text>
                      <Typography.Paragraph>{p.answer || '—'}</Typography.Paragraph>
                      {p.analysis ? (
                        <>
                          <Typography.Text type="secondary">解析</Typography.Text>
                          <Typography.Paragraph>{p.analysis}</Typography.Paragraph>
                        </>
                      ) : null}
                      <Typography.Text type="secondary">出处（展示给用户）</Typography.Text>
                      <Typography.Paragraph>{p.reference || '—'}</Typography.Paragraph>
                      {it.answer_basis ? (
                        <Alert
                          type="warning"
                          showIcon
                          message={`没有引用教材：${it.answer_basis.message}`}
                          description={it.answer_basis.details?.length ? it.answer_basis.details.map((d) => <div key={d}>{d}</div>) : undefined}
                        />
                      ) : p.answer_ai && !it.citations?.length ? (
                        <Typography.Text type="secondary" style={{ fontSize: 12 }}>没有引用到项目关联的教材，按学科通行观点生成，请重点核对</Typography.Text>
                      ) : null}
                      {it.status === 'pending' && it.filling ? (
                        <Alert type="info" showIcon message="正在生成答案" description="在后台检索教材并生成，大约要一两分钟，可以先核对别的题，生成完会自动显示" />
                      ) : null}
                      {it.status === 'pending' && it.answer_ai && !it.filling ? (
                        <Button size="small" loading={regenItem.isPending} disabled={filling} onClick={() => regenItem.mutate(it.id)}>
                          按关联教材重新生成
                        </Button>
                      ) : null}
                      {it.citations?.length ? <Citations items={it.citations} /> : null}
                      {it.status === 'pending' && it.needs_fill && !it.filling ? (
                        <Space direction="vertical" style={{ width: '100%' }}>
                          {it.fill_error ? <Typography.Text type="danger">上次没补上：{it.fill_error}</Typography.Text> : null}
                          <Button type="dashed" block loading={fillItem.isPending} disabled={filling} onClick={() => fillItem.mutate(it.id)}>
                            {fillLabel(p)}
                          </Button>
                        </Space>
                      ) : null}
                    </>
                  ) : (
                    <>
                      <Typography.Paragraph>
                        {String(p.section ?? '')} / {String(p.chapter ?? '')} / <b>{String(p.name ?? '')}</b>
                        <br />
                        {String(p.original_text ?? '')}
                      </Typography.Paragraph>
                      <Typography.Text type="secondary">采分点</Typography.Text>
                      {((p.rubric ?? []) as Point[]).length ? (
                        ((p.rubric ?? []) as Point[]).map((r, i) => <Typography.Text key={i}>{i + 1}. {r.content}（{r.score} 分）</Typography.Text>)
                      ) : (
                        <Typography.Text type="danger">没有采分点，接受前在「修改」里补上（发布前检查要求知识点有采分点）</Typography.Text>
                      )}
                      <Typography.Text type="secondary">出处（展示给用户）</Typography.Text>
                      <Typography.Paragraph>{String(p.reference ?? '') || '—'}</Typography.Paragraph>
                    </>
                  )}
                  {it.status === 'pending' ? (
                    <Space style={{ justifyContent: 'flex-end', width: '100%' }}>
                      <Button onClick={() => review.mutate({ itemId: it.id, decision: 'reject' })} loading={review.isPending}>驳回</Button>
                      <Button onClick={() => setEditing(true)} disabled={it.filling}>修改</Button>
                      <Button
                        type="primary"
                        disabled={it.rubric_mismatch || blocked || it.filling}
                        title={it.filling ? '正在生成答案，生成完再核对' : blocked ? '还没有采分点，先生成或在「修改」里填写' : undefined}
                        onClick={() => review.mutate({ itemId: it.id, decision: 'accept' })}
                        loading={review.isPending}
                      >
                        接受，下一题
                      </Button>
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

/** 引用：书名 · 章节 · 页码，下面是引用的原文段落。 */
function Citations({ items }: { items: Citation[] }) {
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <Typography.Text type="secondary">引用的资料段落</Typography.Text>
      {items.map((c, i) => (
        <div key={i} style={{ background: '#F7F7F5', borderRadius: 8, padding: '8px 12px' }}>
          <Typography.Text strong style={{ fontSize: 12 }}>{citationSource(c)}</Typography.Text>
          {c.quote ? <Typography.Paragraph style={{ margin: '4px 0 0', fontSize: 13 }}>「{c.quote}」</Typography.Paragraph> : null}
          {c.claim ? <Typography.Text type="secondary" style={{ fontSize: 12 }}>支撑：{c.claim}</Typography.Text> : null}
        </div>
      ))}
    </Space>
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
      initialValues={{ ...init, ...groupInit(init), kp_names: (init.kp_names as string[] | undefined)?.join('；') }}
      onFinish={({ elective, group, ...v }) =>
        onSave({
          ...init,
          ...v,
          ...(q ? groupOut({ elective, group }) : {}),
          // 编辑核对过分值，不再标「分值待核对」
          ...(q ? { kp_names: splitKPNames(String(v.kp_names ?? '')), score_inferred: false, answer_ai: !!v.answer_ai } : {}),
        })
      }
    >
      {q ? (
        <>
          <Space>
            <Form.Item name="qtype" label="题型"><Select style={{ width: 120 }} options={Object.entries(qtypeNames).map(([value, label]) => ({ value, label }))} /></Form.Item>
            <Form.Item name="score" label="分值"><InputNumber min={0} max={300} /></Form.Item>
            <Form.Item name="exam_year" label="年份"><InputNumber min={1978} max={2100} /></Form.Item>
          </Space>
          <GroupFields />
          <Form.Item name="stem" label="题干" rules={[{ required: true }]}><Input.TextArea rows={2} /></Form.Item>
          <Form.Item name="kp_names" label="知识点（多个用「；」隔开，第一个是主知识点）"><Input /></Form.Item>
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
          <Form.Item name="answer_ai" valuePropName="checked" extra="勾选时发布后在用户端显示「AI 参考答案」；改成团队给的答案时取消勾选，用户端显示「官方答案」">
            <Checkbox>答案由 AI 生成</Checkbox>
          </Form.Item>
          <Form.Item name="analysis" label="解析"><Input.TextArea rows={3} /></Form.Item>
          <Form.Item name="reference" label="出处（发布后展示给用户）"><Input /></Form.Item>
        </>
      ) : (
        <>
          <Form.Item name="section" label="板块"><Input /></Form.Item>
          <Form.Item name="chapter" label="章节"><Input /></Form.Item>
          <Form.Item name="name" label="知识点"><Input /></Form.Item>
          <Form.Item name="original_text" label="原文表述"><Input.TextArea rows={4} /></Form.Item>
          <Typography.Text type="secondary">采分点</Typography.Text>
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
          <Form.Item name="reference" label="出处（发布后展示给用户）" style={{ marginTop: 12 }}><Input /></Form.Item>
        </>
      )}
      <Space>
        <Button onClick={onCancel}>取消</Button>
        <Button type="primary" htmlType="submit" loading={loading}>修改后接受</Button>
      </Space>
    </Form>
  );
}
