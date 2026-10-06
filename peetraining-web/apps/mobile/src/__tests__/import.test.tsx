import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import ConfirmScreen from '../../app/import/confirm/[id]';
import JobScreen from '../../app/import/job/[id]';
import { itemNote, itemStatus, rubricTotal, stepIndex, type ImportItem } from '../features/import/api';
import { checkLimits, formatOf, uploadAll, UnsupportedFile, type Picked } from '../features/import/files';
import { RubricEditor } from '../features/import/RubricEditor';

const mockReplace = jest.fn();
const mockPush = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
jest.mock('expo-crypto', () => ({
  randomUUID: () => 'uuid',
  CryptoDigestAlgorithm: { SHA256: 'SHA-256' },
  digest: async () => new Uint8Array([0xab, 0xcd]).buffer,
}));
const mockUpload = jest.fn();
jest.mock('expo-file-system', () => ({
  File: class {
    uri: string;
    constructor(uri: string) {
      this.uri = uri;
    }
    arrayBuffer = async () => new ArrayBuffer(4);
    upload = (...a: unknown[]) => mockUpload(...a);
  },
}));
jest.mock('expo-notifications', () => ({ requestPermissionsAsync: async () => ({ granted: true, canAskAgain: true }) }));

const mockCalls: { method: string; path: string; init?: { body?: unknown; params?: unknown } }[] = [];
let mockResponses: Record<string, () => unknown> = {};
jest.mock('../lib/api', () => {
  const actual = jest.requireActual('@training/api-client');
  const m = (method: string) => (path: string, init?: { body?: unknown }) => {
    mockCalls.push({ method, path, init });
    const r = mockResponses[`${method} ${path}`];
    const { status, data, error } = (r ? r() : { status: 200, data: {} }) as { status: number; data?: unknown; error?: unknown };
    return Promise.resolve({ data, error, response: new Response(null, { status }) });
  };
  return { api: { GET: m('GET'), POST: m('POST'), PUT: m('PUT'), PATCH: m('PATCH'), DELETE: m('DELETE') }, unwrap: actual.unwrap };
});

const clients: QueryClient[] = [];
afterEach(() => {
  // 解析中的任务会轮询，测试结束清掉，避免定时器泄漏。
  clients.forEach((c) => c.clear());
  clients.length = 0;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(qc);
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  );
}

const counts = { questions: 3, knowledge_points: 0, needs_review: 1, subjective: 2, objective: 1, confirmed: 0, essay_items: 0 };
const item = (id: number, extra: Partial<ImportItem> = {}): ImportItem => ({
  id,
  job_id: 7,
  item_type: 'question',
  status: 'pending',
  needs_review: false,
  review_reasons: [],
  question: { qtype: 'term', stem: `题目 ${id}`, score: 5 },
  source: { material_id: 1, file_name: '真题.pdf', page: 2 },
  ...extra,
});
const job = (status: string, materials: unknown[]) => ({ id: 7, subject_id: 1, mode: 'question', status, reserved_pages: 3, counts, created_at: '2026-10-02T00:00:00Z', materials });

beforeEach(() => {
  mockCalls.length = 0;
  mockResponses = {};
  mockParams = {};
  mockReplace.mockReset();
  mockPush.mockReset();
  mockUpload.mockReset();
});

describe('导入工具函数', () => {
  it('按扩展名判断格式，旧版 Office 给出另存提示', () => {
    expect(formatOf('真题.PDF')).toBe('pdf');
    expect(formatOf('a.docx')).toBe('docx');
    expect(formatOf('a.xlsx')).toBe('xlsx');
    expect(formatOf('IMG_1.HEIC')).toBe('image');
    expect(formatOf('noext', 'image/png')).toBe('image');
    expect(() => formatOf('a.doc')).toThrow(UnsupportedFile);
    expect(() => formatOf('a.doc')).toThrow('.docx');
    expect(() => formatOf('a.xls')).toThrow('.xlsx');
    expect(() => formatOf('a.zip')).toThrow('暂不支持');
  });

  it('单次上限', () => {
    const pdf = (n: number) => Array.from({ length: n }, (_, i) => ({ name: `${i}.pdf`, format: 'pdf' as const, size: 1 }));
    const img = (n: number) => Array.from({ length: n }, (_, i) => ({ name: `${i}.jpg`, format: 'image' as const, size: 1 }));
    expect(checkLimits([], pdf(10))).toBeUndefined();
    expect(checkLimits([], pdf(11))).toContain('10 个文件');
    expect(checkLimits([], img(31))).toContain('30 张');
    expect(checkLimits([], [{ name: 'big.pdf', format: 'pdf', size: 51 * 1024 * 1024 }])).toContain('50 MB');
  });

  it('条目状态与说明', () => {
    expect(itemStatus(item(1, { review_reasons: ['duplicate'] })).label).toBe('疑似重复');
    expect(itemStatus(item(1, { review_reasons: ['low_confidence'] })).label).toBe('需核对');
    expect(itemStatus(item(1, { review_reasons: ['missing_answer'] })).label).toBe('缺答案');
    expect(itemStatus(item(1, { review_reasons: ['rubric_unconfirmed'] })).label).toBe('采分点待确认');
    expect(itemStatus(item(1, { status: 'confirmed' })).label).toBe('已确认');
    expect(itemNote(item(1, { review_reasons: ['missing_answer'] }))).toContain('AI 生成');
    expect(rubricTotal([{ content: 'a', score: 1.5 }, { content: 'b', score: 2 }, { content: 'c' }])).toBe(3.5);
    expect(stepIndex('extract')).toBe(0);
    expect(stepIndex('structure')).toBe(1);
    expect(stepIndex('done')).toBe(2);
  });
});

describe('上传', () => {
  it('算哈希 → 申请直传 → PUT → 回调；重复文件跳过上传', async () => {
    mockResponses['POST /materials/upload-requests'] = () => ({
      status: 200,
      data: { items: [{ index: 0, material_id: 11, upload_url: 'https://oss/put', upload_headers: { 'Content-Type': 'application/pdf' } }, { index: 1, material_id: 12, duplicate: true }] },
    });
    mockUpload.mockImplementation(async (_url: string, opts: { onProgress: (p: { bytesSent: number; totalBytes: number }) => void }) => {
      opts.onProgress({ bytesSent: 5, totalBytes: 10 });
      return { status: 200, body: '', headers: {} };
    });
    const files: Picked[] = [
      { key: 'a', name: 'a.pdf', format: 'pdf', uri: 'file://a', size: 10, status: 'pending', progress: 0 },
      { key: 'b', name: 'b.pdf', format: 'pdf', uri: 'file://b', size: 10, status: 'pending', progress: 0 },
      { key: 'c', name: '粘贴', format: 'text', size: 5, materialId: 9, status: 'uploaded', progress: 1 },
    ];
    const updates: [string, Partial<Picked>][] = [];
    const ids = await uploadAll(1, 'question', files, (k, p) => updates.push([k, p]));
    expect(ids).toEqual([11, 12, 9]);
    const req = mockCalls.find((c) => c.path === '/materials/upload-requests')!.init!.body as { files: { sha256: string }[]; right_confirmed: boolean };
    expect(req.right_confirmed).toBe(true);
    expect(req.files).toHaveLength(2);
    expect(req.files[0]!.sha256).toBe('abcd');
    expect(mockUpload).toHaveBeenCalledTimes(1);
    expect(mockUpload.mock.calls[0]![1]).toMatchObject({ httpMethod: 'PUT', headers: { 'Content-Type': 'application/pdf' } });
    expect(mockCalls.some((c) => c.path === '/materials/{materialId}/uploaded')).toBe(true);
    expect(updates).toContainEqual(['a', { progress: 0.5 }]);
    expect(updates).toContainEqual(['b', expect.objectContaining({ duplicate: true, status: 'uploaded' })]);
  });

  it('上传失败标出这个文件并抛错', async () => {
    mockResponses['POST /materials/upload-requests'] = () => ({ status: 200, data: { items: [{ index: 0, material_id: 11, upload_url: 'https://oss/put' }] } });
    mockUpload.mockResolvedValue({ status: 403, body: '', headers: {} });
    const updates: [string, Partial<Picked>][] = [];
    await expect(uploadAll(1, undefined, [{ key: 'a', name: 'a.pdf', format: 'pdf', uri: 'file://a', size: 10, status: 'pending', progress: 0 }], (k, p) => updates.push([k, p]))).rejects.toThrow();
    expect(updates.at(-1)).toEqual(['a', expect.objectContaining({ status: 'failed' })]);
  });
});

describe('1.7b 采分点', () => {
  it('合计不等于分值时标红提示', async () => {
    const onChange = jest.fn();
    await render(<RubricEditor points={[{ content: '情景交融', score: 2 }]} score={5} onChange={onChange} />);
    expect(screen.getByText('采分点分值合计要等于题目分值 5 分')).toBeTruthy();
    await fireEvent.press(screen.getByText('＋ 添加采分点'));
    expect(onChange).toHaveBeenCalledWith([{ content: '情景交融', score: 2 }, { content: '', score: 0 }]);
    await fireEvent.changeText(screen.getByLabelText('第 1 个采分点的分值'), '5');
    expect(onChange).toHaveBeenLastCalledWith([{ content: '情景交融', score: 5 }]);
  });
});

describe('1.6 解析中与 1.6b 部分失败', () => {
  it('解析中显示每个文件的进度，可以先核对已识别的', async () => {
    mockParams = { id: '7' };
    mockResponses['GET /import-jobs/{jobId}'] = () => ({
      status: 200,
      data: job('running', [
        { material_id: 1, file_name: '真题.pdf', step: 'done', status: 'done', recognized_count: 3 },
        { material_id: 2, file_name: '讲义.docx', step: 'structure', status: 'running' },
        { material_id: 3, file_name: '照片.jpg', step: 'queued', status: 'pending' },
      ]),
    });
    mockResponses['GET /profile'] = () => ({ status: 200, data: { exam_year: 2027, stage: 'strengthen', stage_manual: false, daily_minutes: 60, reminder_times: [] } });
    await wrap(<JobScreen />);
    expect(await screen.findByText('正在整理你的题库')).toBeTruthy();
    expect(screen.getByText('排队中')).toBeTruthy();
    expect(screen.getByText('✓ 识别文字')).toBeTruthy();
    expect(await screen.findByText('趁这会儿，设置学习提醒')).toBeTruthy();
    await fireEvent.press(screen.getByText('先核对已识别的 3 条'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/import/confirm/[id]', params: { id: '7' } });
  });

  it('失败的文件说明原因，可重新识别', async () => {
    mockParams = { id: '7' };
    const failed = job('reviewing', [
      { material_id: 1, file_name: '真题.pdf', step: 'done', status: 'done', recognized_count: 3 },
      { material_id: 3, file_name: '照片.jpg', step: 'extract', status: 'failed', fail_reason: '照片里没有识别到文字' },
    ]);
    mockResponses['GET /import-jobs/{jobId}'] = () => ({ status: 200, data: failed });
    mockResponses['POST /import-jobs/{jobId}/materials/{materialId}/retry'] = () => ({ status: 200, data: job('running', []) });
    await wrap(<JobScreen />);
    expect(await screen.findByText('有 1 项没能识别好')).toBeTruthy();
    expect(screen.getByText('照片里没有识别到文字')).toBeTruthy();
    await fireEvent.press(screen.getByText('重新识别'));
    await waitFor(() => expect(mockCalls.some((c) => c.path === '/import-jobs/{jobId}/materials/{materialId}/retry')).toBe(true));
  });
});

describe('1.7 确认导入结果', () => {
  it('显示汇总与条目，确认入库后进入 1.8', async () => {
    mockParams = { id: '7' };
    mockResponses['GET /import-jobs/{jobId}'] = () => ({ status: 200, data: job('reviewing', [{ material_id: 1, file_name: '真题.pdf', step: 'done', status: 'done' }]) });
    mockResponses['GET /import-jobs/{jobId}/items'] = () => ({
      status: 200,
      data: { counts, items: [item(1), item(2, { review_reasons: ['low_confidence'], needs_review: true }), item(3, { review_reasons: ['missing_answer'] })] },
    });
    mockResponses['POST /import-jobs/{jobId}/confirm'] = () => ({ status: 200, data: { question_count: 3, kp_count: 2, needs_review_count: 1, paper_count: 1, plan_ready: false, subjects_without_import: [2] } });
    await wrap(<ConfirmScreen />);
    expect(await screen.findByText('题目 2')).toBeTruthy();
    expect(screen.getByText('需核对')).toBeTruthy();
    expect(screen.getByText('缺答案')).toBeTruthy();
    await fireEvent.press(screen.getByText('确认入库 3 题'));
    await waitFor(() =>
      expect(mockReplace).toHaveBeenCalledWith({ pathname: '/import/done/[id]', params: expect.objectContaining({ id: '7', questions: '3', review: '1', without: '2' }) }),
    );
  });

  it('题目导入额度不够时弹出开会员', async () => {
    mockParams = { id: '7' };
    mockResponses['GET /import-jobs/{jobId}'] = () => ({ status: 200, data: job('reviewing', []) });
    mockResponses['GET /import-jobs/{jobId}/items'] = () => ({ status: 200, data: { counts, items: [item(1)] } });
    mockResponses['POST /import-jobs/{jobId}/confirm'] = () => ({ status: 402, error: { code: 'QUOTA_EXCEEDED', message: '额度不足' } });
    await wrap(<ConfirmScreen />);
    await fireEvent.press(await screen.findByText('确认入库 3 题'));
    expect(await screen.findByText('题目导入额度不够了')).toBeTruthy();
  });
});
