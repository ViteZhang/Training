import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import PaperModePage from '../../app/paper/[id]';
import PaperHistoryPage from '../../app/paper/history';
import { PaperTimer } from '../features/paper/timer';
import PaperList from '../../app/paper/list';
import PaperSessionPage from '../../app/paper/session/[id]';
import { clock } from '../features/paper/api';
import { PaperPhotoQueue, paperPhotoQueue, type PaperHandwriting } from '../features/paper/photos';
import { getJSON, setJSON, storage } from '../lib/storage';

const mockPush = jest.fn();
const mockReplace = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
  useFocusEffect: (effect: () => void) => jest.requireActual('react').useEffect(effect, [effect]),
}));
let mockUuid = 0;
jest.mock('expo-crypto', () => ({ randomUUID: () => `uuid-${++mockUuid}` }));

const mockUpload = jest.fn(async () => ({ status: 200 }));
let mockCameraGranted = true;
jest.mock('expo-file-system', () => ({
  Paths: { document: { uri: 'file:///documents/' } },
  Directory: class { uri = 'file:///documents/paper-handwriting/'; create() {} },
  File: class {
    size = 2048;
    exists = true;
    uri: string;
    constructor(base: string | { uri: string }, name?: string) { this.uri = typeof base === 'string' ? base : `${base.uri}${name ?? ''}`; }
    copy() {}
    delete() {}
    upload = (...a: unknown[]) => mockUpload(...(a as []));
  },
}));
jest.mock('expo-image-picker', () => ({
  requestCameraPermissionsAsync: async () => ({ granted: mockCameraGranted, canAskAgain: true }),
  launchCameraAsync: async () => ({ canceled: false, assets: [{ uri: 'file:///paper-answer.jpg', mimeType: 'image/jpeg', fileSize: 2048 }] }),
}));
let queryClient: QueryClient;
const mockCalls: { method: string; path: string; init?: unknown }[] = [];
let mockResponses: Record<string, (init?: unknown) => unknown> = {};
async function mockRespond(method: string, path: string, init?: unknown) {
  mockCalls.push({ method, path, init });
  const r = mockResponses[`${method} ${path}`];
  const v = r ? r(init) : { status: 200, data: {} };
  const { status, data, error } = await v as { status: number; data?: unknown; error?: unknown };
  return Promise.resolve({ data, error, response: new Response(null, { status }) });
}
jest.mock('../lib/api', () => {
  const actual = jest.requireActual('@training/api-client');
  const m = (method: string) => (path: string, init?: unknown) => mockRespond(method, path, init);
  return { api: { GET: m('GET'), POST: m('POST'), PUT: m('PUT'), PATCH: m('PATCH'), DELETE: m('DELETE') }, unwrap: actual.unwrap };
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  queryClient = qc;
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  );
}

const brief = (over: object = {}) => ({
  id: 5, kind: 'real_exam', title: '2024 年真题', exam_year: 2024, question_count: 3, full_score: 150, actual_score: 150, duration_minutes: 180,
  ai_filled: 0, status: 'not_started', ...over,
});
const detail = { ...brief(), sections: [{ qtype: 'term', count: 2, score_each: 10, total: 20, suggested_minutes: 25 }, { qtype: 'discussion', count: 1, score_each: 130, total: 130, suggested_minutes: 140 }],
  check_minutes: 15, recommended_mode: 'mock', counts_for_estimate: true };
const item = (seq: number, over: object = {}) => ({
  seq, section: seq < 3 ? '名词解释' : '论述', qtype: seq < 3 ? 'term' : 'discussion', score: seq < 3 ? 10 : 130, question_id: 100 + seq, stem: `第${seq}题题干`,
  marked: false, answered: false, time_spent_seconds: 0, ...over,
});
const serverNow = new Date();
const session = (over: object = {}) => ({
  id: 90, paper_id: 5, subject_id: 7, title: '2024 年真题', kind: 'real_exam', mode: 'mock', status: 'in_progress',
  started_at: serverNow.toISOString(), deadline_at: new Date(serverNow.getTime() + 3600_000).toISOString(), server_now: serverNow.toISOString(),
  elapsed_seconds: 0, total_minutes: 180, check_minutes: 15, remind_left_minutes: 15, resume_available: true, full_score: 150, counts_for_estimate: true,
  reminders: [{ qtype: 'term', at_minutes: 25, suggested_minutes: 25, text: '名词解释的建议用时（25 分钟）到了，建议进入论述' }],
  items: [item(1, { draft_text: '建安风骨是…', answered: true }), item(2), item(3)],
  ...over,
});

beforeEach(() => {
  mockCalls.length = 0;
  mockCameraGranted = true;
  mockUpload.mockReset();
  mockUpload.mockResolvedValue({ status: 200 });
  for (const photo of Object.values(paperPhotoQueue(90).snapshot())) paperPhotoQueue(90).discardLocal(photo.key);
  mockPush.mockReset();
  mockReplace.mockReset();
  storage.clearAll();
  mockParams = {};
  mockResponses = { 'GET /feature-flags': () => ({ status: 200, data: { flags: {} } }) };
});

describe('4.18 整卷列表', () => {
  it('真题卷显示进行中与得分，AI 组卷后进入选择模式', async () => {
    mockParams = { subjectId: '7' };
    mockResponses['GET /subjects/{subjectId}/papers'] = () => ({
      status: 200,
      data: {
        real_exam: [
          brief({ session: { id: 90, mode: 'practice', status: 'in_progress', answered: 1, total: 3 } }),
          brief({ id: 6, title: '2023 年真题', status: 'done', missing_note: '资料里缺 1 道论述题，按 120 分计分', last: { session_id: 80, mode: 'mock', finished_at: '2026-09-20T08:00:00Z', score: 98, full_score: 150, status: 'graded' } }),
        ],
        ai_papers: [],
        weekly_remaining: 1,
      },
    });
    mockResponses['POST /subjects/{subjectId}/papers'] = () => ({ status: 201, data: { ...detail, id: 11, kind: 'ai_standard' } });
    const r = await wrap(<PaperList />);
    expect(await screen.findByText('进行中 · 已答 1 / 3 · 练习模式')).toBeTruthy();
    expect(screen.getByText(/^98/)).toBeTruthy();
    expect(screen.getByText('资料里缺 1 道论述题，按 120 分计分')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('2024 年真题'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/paper/session/[id]', params: { id: '90' } });
    await fireEvent.press(screen.getByText('标准卷'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith({ pathname: '/paper/[id]', params: { id: '11' } }));
    expect(mockCalls.find((c) => c.method === 'POST')?.init).toMatchObject({ body: { kind: 'ai_standard' } });
    await r.unmount();
  });
});

describe('4.19 选择作答模式', () => {
  it('默认推荐模式，切到练习模式后开始，带幂等键', async () => {
    mockParams = { id: '5' };
    mockResponses['GET /papers/{paperId}'] = () => ({ status: 200, data: detail });
    mockResponses['POST /papers/{paperId}/sessions'] = () => ({ status: 201, data: session({ mode: 'practice' }) });
    const r = await wrap(<PaperModePage />);
    expect(await screen.findByText('开始模拟考试')).toBeTruthy();
    expect(screen.getByText('140′')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('练习模式'));
    await fireEvent.press(screen.getByText('开始练习'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith({ pathname: '/paper/session/[id]', params: { id: '90' } }));
    expect(mockCalls.find((c) => c.method === 'POST')?.init).toMatchObject({ body: { mode: 'practice', idempotency_key: expect.stringMatching(/^uuid-/) } });
    await r.unmount();
  });

  it('额度用完弹出额度提示', async () => {
    mockParams = { id: '5' };
    mockResponses['GET /papers/{paperId}'] = () => ({ status: 200, data: detail });
    mockResponses['POST /papers/{paperId}/sessions'] = () => ({ status: 402, error: { code: 'QUOTA_EXCEEDED', message: '额度不足' } });
    const r = await wrap(<PaperModePage />);
    await await fireEvent.press(await screen.findByText('开始模拟考试'));
    expect(await screen.findByText('本周的整卷批改次数用完了')).toBeTruthy();
    await r.unmount();
  });
});

describe('4.20–4.23 作答', () => {
  beforeEach(() => {
    mockParams = { id: '90' };
  });

  it('模拟考试倒计时以服务端截止时间为准，本地草稿优先（杀掉 App 再打开还在）', async () => {
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session() });
    setJSON('draft:paper:90', { 2: '本地还没同步的草稿' });
    const r = await wrap(<PaperSessionPage />);
    expect(await screen.findByLabelText('剩余时间')).toBeTruthy();
    expect(screen.getByLabelText('剩余时间').props.children).toMatch(/^00:59:5\d$|^01:00:00$/);
    expect(screen.getByDisplayValue('本地还没同步的草稿')).toBeTruthy();
    expect(screen.queryByText('考试被中断了')).toBeNull();
    await r.unmount();
  });

  it('作答后打开答题卡交卷：先同步草稿，确认框写明未答与标记数，交卷后进入批改中', async () => {
    let status = 'in_progress';
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session({ status }) });
    mockResponses['PUT /paper-sessions/{sessionId}/items/{seq}'] = () => ({ status: 200, data: item(2, { marked: true }) });
    mockResponses['POST /paper-sessions/{sessionId}/submit'] = () => {
      status = 'grading';
      return { status: 200, data: session({ status: 'grading' }) };
    };
    const r = await wrap(<PaperSessionPage />);
    expect(await screen.findByText(/第2题题干/)).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('你的答案'), '玄学清谈');
    await fireEvent.press(screen.getByText('标记'));
    await waitFor(() => expect(screen.getByText('已标记')).toBeTruthy());
    await fireEvent.press(screen.getByText('答题卡'));
    expect(screen.getByText('已答 2')).toBeTruthy();
    expect(screen.getByText('未答 1')).toBeTruthy();
    await fireEvent.press(screen.getAllByText('交卷')[0]!);
    expect(await screen.findByText('还有 1 题未作答，1 题已标记。交卷后开始整卷批改，大约需要 2 分钟。按你资料里的采分点逐题批改。')).toBeTruthy();
    await fireEvent.press(screen.getAllByText('交卷').at(-1)!);
    expect(await screen.findByText('正在整卷批改')).toBeTruthy();
    const put = mockCalls.find((c) => c.method === 'PUT' && (c.init as { body: { draft_text?: string } }).body.draft_text === '玄学清谈');
    expect(put?.init).toMatchObject({ params: { path: { sessionId: 90, seq: 2 } } });
    expect(getJSON('draft:paper:90')).toBeUndefined();
    await r.unmount();
  });

  it('模拟考试被中断后重新打开，提示恢复并带上中断时间', async () => {
    const at = Date.now() - 5 * 60_000;
    setJSON('paper:active:90', at);
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session() });
    mockResponses['POST /paper-sessions/{sessionId}/resume'] = () => ({ status: 200, data: session({ resume_available: false }) });
    const r = await wrap(<PaperSessionPage />);
    expect(await screen.findByText('考试被中断了')).toBeTruthy();
    await fireEvent.press(screen.getByText('恢复考试'));
    await waitFor(() => expect(mockCalls.some((c) => c.path === '/paper-sessions/{sessionId}/resume')).toBe(true));
    expect(mockCalls.find((c) => c.path === '/paper-sessions/{sessionId}/resume')?.init).toMatchObject({ body: { interrupted_at: new Date(at).toISOString() } });
    await r.unmount();
  });

  it('时间到自动交卷', async () => {
    const past = new Date(serverNow.getTime() - 1000).toISOString();
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session({ deadline_at: past }) });
    mockResponses['POST /paper-sessions/{sessionId}/submit'] = () => ({ status: 200, data: session({ status: 'grading' }) });
    const r = await wrap(<PaperSessionPage />);
    expect(await screen.findByText('正在整卷批改')).toBeTruthy();
    await r.unmount();
  });

  it('练习模式可暂停，暂停后显示继续作答', async () => {
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session({ mode: 'practice', deadline_at: undefined, resume_available: false }) });
    mockResponses['PUT /paper-sessions/{sessionId}/items/{seq}'] = () => ({ status: 200, data: item(2) });
    mockResponses['POST /paper-sessions/{sessionId}/pause'] = () => ({ status: 200, data: session({ mode: 'practice', status: 'paused', deadline_at: undefined }) });
    const r = await wrap(<PaperSessionPage />);
    expect(await screen.findByLabelText('已用时间')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('退出'));
    await fireEvent.press(screen.getByText('暂停并离开'));
    expect(await screen.findByText('已暂停')).toBeTruthy();
    await r.unmount();
  });

  it('批改完成显示得分与每题得分', async () => {
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({
      status: 200,
      data: session({ status: 'graded', score: 112, items: [item(1, { answered: true, got: 8, grading_id: 501 }), item(2, { got: 6 }), item(3, { got: 98 })] }),
    });
    const r = await wrap(<PaperSessionPage />);
    expect(await screen.findByText(/^112/)).toBeTruthy();
    expect(screen.getByText('这套真题卷会用来更新预估分')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('第 1 题'));
    await fireEvent.press(screen.getByText('查看逐题批改'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/practice/grading/[id]', params: { id: '501' } });
    await r.unmount();
  });
});

describe('clock', () => {
  it('格式化为时:分:秒，负数按 0', () => {
    expect(clock(3725)).toBe('01:02:05');
    expect(clock(-3)).toBe('00:00:00');
  });
});


describe('模拟考试回归', () => {
  beforeEach(() => { mockParams = { id: '90' }; });
  it('拍照上传、核对修改后填回当前题并保留已有答案，交卷前同步识别文字', async () => {
    storage.set('permission_explained_camera', true);
    let draft = '原有要点';
    let photos: PaperHandwriting[] = [];
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session({ items: [item(1, { draft_text: draft, handwriting: photos })] }) });
    mockResponses['POST /handwriting/upload-requests'] = () => ({ status: 200, data: { items: [{ object_key: 'u/1/hw/paper.jpg', upload_url: 'https://oss/paper', upload_headers: {} }] } });
    mockResponses['POST /paper-sessions/{sessionId}/items/{seq}/handwriting'] = (init) => {
      const body = (init as { body: { idempotency_key: string } }).body;
      photos = [{ id: 51, seq: 1, idempotency_key: body.idempotency_key, object_keys: ['u/1/hw/paper.jpg'], status: 'ready', text: '识别的要点', low_confidence: [] }];
      return { status: 200, data: photos[0] };
    };
    mockResponses['PUT /paper-sessions/{sessionId}/items/{seq}/handwriting/{handwritingId}'] = (init) => {
      draft = `原有要点\n${(init as { body: { text: string } }).body.text}`;
      photos = photos.map((p) => ({ ...p, status: 'confirmed' }));
      return { status: 200, data: item(1, { draft_text: draft, handwriting: photos }) };
    };
    mockResponses['POST /paper-sessions/{sessionId}/submit'] = () => ({ status: 200, data: session({ status: 'grading' }) });
    const r = await wrap(<PaperSessionPage />);
    await fireEvent.press(await screen.findByText('拍手写稿'));
    await fireEvent.press(screen.getByText('拍手写稿'));
    expect(await screen.findByDisplayValue('识别的要点')).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('识别文字'), '核对后的要点');
    await fireEvent.press(screen.getByText('确认并填入答案'));
    expect(screen.getByDisplayValue('原有要点\n核对后的要点')).toBeTruthy();
    expect(mockUpload).toHaveBeenCalledWith('https://oss/paper', { httpMethod: 'PUT', headers: {} });
    expect(mockCalls.some((c) => c.path.includes('/submit'))).toBe(false);
    await fireEvent.press(screen.getByText('交卷'));
    await fireEvent.press(screen.getAllByText('交卷').at(-1)!);
    expect(await screen.findByText('正在整卷批改')).toBeTruthy();
    expect(mockCalls.find((c) => c.path.endsWith('/handwriting/{handwritingId}'))?.init).toMatchObject({ body: { text: '核对后的要点' } });
    expect(mockCalls.some((c) => c.path === '/handwriting/recognize')).toBe(false);
    await r.unmount();
  });

  it('相机权限被拒时能回到打字，原草稿保留', async () => {
    storage.set('permission_explained_camera', true);
    mockCameraGranted = false;
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session() });
    const r = await wrap(<PaperSessionPage />);
    await fireEvent.changeText(await screen.findByLabelText('你的答案'), '不能丢的草稿');
    await fireEvent.press(screen.getByText('拍手写稿'));
    await fireEvent.press(screen.getByText('拍手写稿'));
    expect(await screen.findByText('相机权限未开启')).toBeTruthy();
    await fireEvent.press(screen.getAllByText('改为打字作答').at(-1)!);
    expect(screen.getByDisplayValue('不能丢的草稿')).toBeTruthy();
    await r.unmount();
  });

  it('批改进度轮询失败保留原答卷；能查看提交的答案', async () => {
    let fail = false;
    mockResponses['GET /paper-sessions/{sessionId}'] = () => fail ? { status: 503, error: { code: 'SERVICE_UNAVAILABLE', message: '暂时不可用' } } : { status: 200, data: session({ status: 'grading' }) };
    const r = await wrap(<PaperSessionPage />);
    expect(await screen.findByText('正在整卷批改')).toBeTruthy();
    fail = true;
    await act(async () => { await queryClient.invalidateQueries({ queryKey: ['paper-session', 90] }); });
    expect(await screen.findByText('暂时无法刷新批改进度，已提交的答卷已保存。')).toBeTruthy();
    expect(screen.getByText('正在整卷批改')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('第 1 题'));
    expect(await screen.findByText('建安风骨是…')).toBeTruthy();
    await r.unmount();
  });

  it('任务异常时恢复原答卷批改，不新建考试', async () => {
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session({ status: 'grading', grading_error: '任务重试耗尽，答卷已保存' }) });
    mockResponses['POST /paper-sessions/{sessionId}/submit'] = () => ({ status: 200, data: session({ status: 'grading' }) });
    const r = await wrap(<PaperSessionPage />);
    await fireEvent.press(await screen.findByText('恢复批改'));
    expect(await screen.findByText('正在整卷批改')).toBeTruthy();
    expect(mockCalls.find((c) => c.method === 'POST')?.init).toMatchObject({ params: { path: { sessionId: 90 } } });
    expect(mockCalls.some((c) => c.path === '/papers/{paperId}/sessions')).toBe(false);
    await r.unmount();
  });

  it('本题用时每秒更新，切题后各题独立累计', async () => {
    jest.useFakeTimers();
    try {
      mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session() });
      const r = await wrap(<PaperSessionPage />);
      expect(await screen.findByText('本题已用 00:00:00')).toBeTruthy();
      await act(async () => { await jest.advanceTimersByTimeAsync(3000); });
      expect(screen.getByText('本题已用 00:00:03')).toBeTruthy();
      await fireEvent.press(screen.getByText('下一题'));
      expect(screen.getByText('本题已用 00:00:00')).toBeTruthy();
      await act(async () => { await jest.advanceTimersByTimeAsync(2000); });
      expect(screen.getByText('本题已用 00:00:02')).toBeTruthy();
      await fireEvent.press(screen.getByText('上一题'));
      expect(screen.getByText('本题已用 00:00:03')).toBeTruthy();
      await r.unmount();
    } finally { jest.useRealTimers(); }
  });
});

describe('逐次历史答卷', () => {
  it('同一套卷多次作答分别保留，批改中可进入原 session，支持分页', async () => {
    mockParams = { subjectId: '7' };
    let page = 0;
    const historyItem = { id: 90, title: '2024 年真题', kind: 'real_exam', mode: 'mock', status: 'grading', started_at: serverNow.toISOString(), full_score: 150, answered: 2, total: 3 };
    mockResponses['GET /subjects/{subjectId}/paper-sessions'] = () => ({ status: 200, data: ++page === 1 ? { items: [historyItem], next_before: 90 } : { items: [{ ...historyItem, id: 80, status: 'graded', score: 98 }] } });
    const r = await wrap(<PaperHistoryPage />);
    await fireEvent.press(await screen.findByLabelText('2024 年真题，批改中'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/paper/session/[id]', params: { id: '90' } });
    await fireEvent.press(screen.getByText('加载更多'));
    expect(await screen.findByText('98 / 150 分 · 查看答卷及报告')).toBeTruthy();
    expect(screen.getAllByText('2024 年真题')).toHaveLength(2);
    expect(mockCalls.at(-1)?.init).toMatchObject({ params: { query: { before: 90 } } });
    await r.unmount();
  });
});

describe('计时增量在失败、暂停和切题后保留', () => {
  it('同步失败不丢时间，成功才确认增量；暂停不计时', () => {
    let at = 0;
    const t = new PaperTimer([{ seq: 1, time_spent_seconds: 4 }, { seq: 2, time_spent_seconds: 0 }], 1, true, () => at);
    at = 3000; expect(t.seconds(1)).toBe(7); expect(t.pending(1)).toBe(3);
    at = 5000; expect(t.pending(1)).toBe(5);
    t.acknowledge(1, 3); expect(t.pending(1)).toBe(2);
    t.select(2); at = 8000; expect(t.seconds(1)).toBe(9); expect(t.seconds(2)).toBe(3);
    t.setRunning(false); at = 18000; expect(t.seconds(2)).toBe(3);
    t.setRunning(true); at = 20000; expect(t.seconds(2)).toBe(5);
  });
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function backgroundPhotos(options: { oneItem?: boolean } = {}) {
  let jobs: PaperHandwriting[] = [];
  const drafts: Record<number, string> = { 1: '第一题已有答案' };
  const view = () => session({ items: (options.oneItem ? [1] : [1, 2, 3]).map((seq) => item(seq, { draft_text: drafts[seq] ?? '', handwriting: jobs.filter((job) => job.seq === seq) })) });
  mockParams = { id: '90' };
  storage.set('permission_explained_camera', true);
  mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: view() });
  let uploads = 0;
  mockResponses['POST /handwriting/upload-requests'] = () => ({ status: 200, data: { items: [{ object_key: `u/1/hw/${++uploads}.jpg`, upload_url: `https://oss/${uploads}`, upload_headers: {} }] } });
  mockResponses['POST /paper-sessions/{sessionId}/items/{seq}/handwriting'] = (init) => {
    const { params, body } = init as { params: { path: { seq: number } }; body: { object_keys: string[]; idempotency_key: string; capture_order?: number; input_kind?: 'photo' | 'voice' } };
    let job = jobs.find((j) => j.idempotency_key === body.idempotency_key);
    if (!job) {
      job = { id: jobs.length + 100, seq: params.path.seq, capture_order: body.capture_order, input_kind: body.input_kind, idempotency_key: body.idempotency_key, object_keys: body.object_keys, status: 'pending', text: '', low_confidence: [] };
      jobs = [...jobs, job];
    }
    return { status: 200, data: job };
  };
  mockResponses['PUT /paper-sessions/{sessionId}/items/{seq}'] = (init) => {
    const { params, body } = init as { params: { path: { seq: number } }; body: { draft_text?: string } };
    if (body.draft_text !== undefined) drafts[params.path.seq] = body.draft_text;
    return { status: 200, data: item(params.path.seq, { draft_text: drafts[params.path.seq] }) };
  };
  mockResponses['PUT /paper-sessions/{sessionId}/items/{seq}/handwriting/{handwritingId}'] = (init) => {
    const { params, body } = init as { params: { path: { seq: number; handwritingId: number } }; body: { text: string; confirm?: boolean } };
    const job = jobs.find((j) => j.id === params.path.handwritingId)!;
    if (job.status !== 'confirmed') {
      if (body.confirm !== false) {
        const previous = drafts[job.seq] ?? '';
        drafts[job.seq] = previous ? `${previous}\n${body.text}` : body.text;
      }
      jobs = jobs.map((j) => j.id === job.id ? { ...j, status: body.confirm === false ? 'ready' : 'confirmed', text: body.text } : j);
    }
    return { status: 200, data: view().items.find((it) => it.seq === params.path.seq) };
  };
  mockResponses['POST /paper-sessions/{sessionId}/submit'] = () => {
    for (const job of [...jobs].sort((a, b) => (a.capture_order ?? a.id) - (b.capture_order ?? b.id))) if (job.status === 'ready') {
      drafts[job.seq] = drafts[job.seq] ? `${drafts[job.seq]}\n${job.text}` : job.text;
      jobs = jobs.map((j) => j.id === job.id ? { ...j, status: 'confirmed' } : j);
    }
    return { status: 200, data: { ...view(), status: 'grading' } };
  };
  return {
    jobs: () => jobs,
    drafts,
    ready: (seq: number, text: string) => { jobs = jobs.map((j) => j.seq === seq ? { ...j, status: 'ready', text } : j); },
    readyJob: (id: number, text: string) => { jobs = jobs.map((j) => j.id === id ? { ...j, status: 'ready', text } : j); },
    failed: (seq: number) => { jobs = jobs.map((j) => j.seq === seq ? { ...j, status: 'failed', error: '照片已保存，请重试' } : j); },
    refresh: () => act(async () => { await queryClient.invalidateQueries({ queryKey: ['paper-session', 90] }); }),
  };
}
async function takePaperPhoto() {
  await fireEvent.press(await screen.findByText('拍手写稿'));
  await fireEvent.press(screen.getByText('拍手写稿'));
}
async function submitFromCard() {
  await fireEvent.press(screen.getByText('答题卡'));
  await fireEvent.press(screen.getAllByText('交卷')[0]!);
  await fireEvent.press(screen.getAllByText('交卷').at(-1)!);
}

describe('模拟考试手写稿后台识别', () => {
  it('上传未完成即可切题；完成后仍归原题，核对中的编辑跨题保留', async () => {
    const fixture = backgroundPhotos();
    const upload = deferred<{ status: number }>();
    mockUpload.mockReturnValueOnce(upload.promise);
    const r = await wrap(<PaperSessionPage />);
    await takePaperPhoto();
    expect(await screen.findByText('正在后台上传照片')).toBeTruthy();
    expect(Object.values(paperPhotoQueue(90).snapshot())[0]?.seq).toBe(1);
    await fireEvent.press(screen.getByText('下一题'));
    await fireEvent.changeText(screen.getByLabelText('你的答案'), '第二题自己的答案');
    await act(async () => { upload.resolve({ status: 200 }); await paperPhotoQueue(90).waitForUploads(); });
    expect(fixture.jobs()[0]?.seq).toBe(1);
    fixture.ready(1, '第一题识别内容');
    await fixture.refresh();
    expect(screen.getByDisplayValue('第二题自己的答案')).toBeTruthy();
    expect(screen.queryByLabelText('识别文字')).toBeNull();
    await fireEvent.press(screen.getByText('答题卡'));
    await fireEvent.press(screen.getByLabelText('第 1 题，已答，待核对'));
    expect(await screen.findByDisplayValue('第一题识别内容')).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('识别文字'), '第一题核对后内容');
    await fireEvent.press(screen.getByText('下一题'));
    await fireEvent.press(screen.getByText('上一题'));
    expect(screen.getByDisplayValue('第一题核对后内容')).toBeTruthy();
    await fireEvent.press(screen.getByText('确认并填入答案'));
    expect(await screen.findByDisplayValue('第一题已有答案\n第一题核对后内容')).toBeTruthy();
    expect(fixture.drafts[2]).toBe('第二题自己的答案');
    expect(mockCalls.some((c) => c.path === '/handwriting/recognize')).toBe(false);
    await r.unmount();
  });

  it('手动交卷等待上传登记，识别中的照片随答卷交给后台处理', async () => {
    const fixture = backgroundPhotos({ oneItem: true });
    const upload = deferred<{ status: number }>();
    mockUpload.mockReturnValueOnce(upload.promise);
    const r = await wrap(<PaperSessionPage />);
    await takePaperPhoto();
    await submitFromCard();
    expect(mockCalls.filter((c) => c.path.endsWith('/submit'))).toHaveLength(0);
    expect(await screen.findByText(/正在保存答卷，等待照片和录音上传/)).toBeTruthy();
    await act(async () => { upload.resolve({ status: 200 }); await paperPhotoQueue(90).waitForUploads(); });
    expect(await screen.findByText('正在识别答案并整卷批改')).toBeTruthy();
    expect(fixture.jobs()[0]?.status).toBe('pending');
    expect(mockCalls.findIndex((c) => c.path.endsWith('/handwriting'))).toBeLessThan(mockCalls.findIndex((c) => c.path.endsWith('/submit')));
    expect(getJSON('paper:photos:90')).toBeUndefined();
    await r.unmount();
  });

  it('上传失败保留本机照片，切题回来可以重试而无需重新拍摄', async () => {
    backgroundPhotos();
    mockUpload.mockRejectedValueOnce(new Error('断网了'));
    const r = await wrap(<PaperSessionPage />);
    await takePaperPhoto();
    expect(await screen.findByText('文件保存失败')).toBeTruthy();
    const saved = Object.values(paperPhotoQueue(90).snapshot())[0]!;
    expect(saved.shots[0]?.uri).toContain('/documents/paper-handwriting/');
    await fireEvent.press(screen.getByText('下一题'));
    await fireEvent.press(screen.getByText('上一题'));
    expect(screen.getByText('重试上传')).toBeTruthy();
    await fireEvent.press(screen.getByText('重试上传'));
    expect(await screen.findByText('手写稿正在后台识别')).toBeTruthy();
    expect(mockUpload).toHaveBeenCalledTimes(2);
    expect(Object.values(paperPhotoQueue(90).snapshot())[0]?.key).toBe(saved.key);
    await r.unmount();
  });

  it('未点确认的核对编辑在手动交卷时自动保存，不重复追加到答案', async () => {
    const fixture = backgroundPhotos({ oneItem: true });
    const r = await wrap(<PaperSessionPage />);
    await takePaperPhoto();
    await waitFor(() => expect(fixture.jobs()).toHaveLength(1));
    fixture.ready(1, '原始识别'); await fixture.refresh();
    await fireEvent.changeText(await screen.findByLabelText('识别文字'), '用户改过的识别结果');
    await submitFromCard();
    expect(await screen.findByText('正在整卷批改')).toBeTruthy();
    expect(fixture.drafts[1]).toBe('第一题已有答案\n用户改过的识别结果');
    expect(mockCalls.filter((c) => c.path.endsWith('/handwriting/{handwritingId}'))).toHaveLength(1);
    await r.unmount();
  });

  it('已经登记但未识别的照片在截止时间自动交卷时仍交给后台', async () => {
    const fixture = backgroundPhotos({ oneItem: true });
    const r = await wrap(<PaperSessionPage />);
    await takePaperPhoto();
    await waitFor(() => expect(fixture.jobs()).toHaveLength(1));
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session({ deadline_at: new Date(serverNow.getTime() - 1000).toISOString(), items: [item(1, { handwriting: fixture.jobs() })] }) });
    await fixture.refresh();
    expect(await screen.findByText('正在识别答案并整卷批改')).toBeTruthy();
    expect(mockCalls.filter((c) => c.path.endsWith('/submit'))).toHaveLength(1);
    expect(fixture.jobs()).toHaveLength(1);
    await r.unmount();
  });

  it('识别失败不会被当作空白答案交卷，保留照片并提示重试', async () => {
    const fixture = backgroundPhotos({ oneItem: true });
    const r = await wrap(<PaperSessionPage />);
    await takePaperPhoto();
    await waitFor(() => expect(fixture.jobs()).toHaveLength(1));
    fixture.failed(1); await fixture.refresh();
    expect(await screen.findByText('识别失败')).toBeTruthy();
    await submitFromCard();
    expect(await screen.findByText('第 1 题识别失败，请重试或弃用文件后交卷')).toBeTruthy();
    expect(mockCalls.some((c) => c.path.endsWith('/submit'))).toBe(false);
    expect(Object.values(paperPhotoQueue(90).snapshot())[0]?.shots).toHaveLength(1);
    await r.unmount();
  });
});

it('同题两页上传乱序完成，显示和交卷仍按拍照顺序，未确认的修改保留', async () => {
  const fixture = backgroundPhotos({ oneItem: true });
  const firstUpload = deferred<{ status: number }>();
  mockUpload.mockReturnValueOnce(firstUpload.promise);
  const r = await wrap(<PaperSessionPage />);
  await takePaperPhoto();
  await takePaperPhoto();
  await waitFor(() => expect(fixture.jobs()).toHaveLength(1));
  const later = fixture.jobs()[0]!;
  await act(async () => { firstUpload.resolve({ status: 200 }); await paperPhotoQueue(90).waitForUploads(); });
  const earlier = fixture.jobs()[1]!;
  expect(earlier.capture_order).toBeLessThan(later.capture_order!);
  fixture.readyJob(earlier.id, '第一页识别文字');
  fixture.readyJob(later.id, '第二页识别文字');
  await fixture.refresh();
  const editors = await screen.findAllByLabelText('识别文字');
  expect(editors[0]?.props.value).toBe('第一页识别文字');
  expect(editors[1]?.props.value).toBe('第二页识别文字');
  await fireEvent.press(screen.getAllByText('确认并填入答案')[1]!);
  expect(mockCalls.filter((c) => c.path.endsWith('/handwriting/{handwritingId}'))).toHaveLength(0);
  await fireEvent.changeText(editors[1]!, '第二页修改后');
  await submitFromCard();
  expect(await screen.findByText('正在整卷批改')).toBeTruthy();
  expect(fixture.drafts[1]).toBe('第一题已有答案\n第一页识别文字\n第二页修改后');
  expect(mockCalls.find((c) => c.path.endsWith('/handwriting/{handwritingId}'))?.init).toMatchObject({ body: { text: '第二页修改后', confirm: false } });
  await r.unmount();
});

it('登记响应丢失时通过服务端状态找回同一张稿件，不重复上传', async () => {
  const fixture = backgroundPhotos({ oneItem: true });
  const register = mockResponses['POST /paper-sessions/{sessionId}/items/{seq}/handwriting']!;
  mockResponses['POST /paper-sessions/{sessionId}/items/{seq}/handwriting'] = (init) => { register(init); throw new Error('响应丢失'); };
  const r = await wrap(<PaperSessionPage />);
  await takePaperPhoto();
  await waitFor(() => expect(Object.values(paperPhotoQueue(90).snapshot())[0]?.status).toBe('registered'));
  expect(await screen.findByText('手写稿正在后台识别')).toBeTruthy();
  expect(fixture.jobs()).toHaveLength(1);
  expect(mockUpload).toHaveBeenCalledTimes(1);
  await submitFromCard();
  expect(await screen.findByText('正在识别答案并整卷批改')).toBeTruthy();
  await r.unmount();
});

it('进程重建后恢复已经上传但未登记的照片，沿用照片键与拍照顺序', async () => {
  backgroundPhotos();
  setJSON('paper:photos:91', { restored: { key: 'restored', order: 123, seq: 2, shots: [{ uri: 'file:///documents/paper-handwriting/restored.jpg', mimeType: 'image/jpeg', size: 2048 }], keys: ['u/1/hw/restored.jpg'], status: 'uploading' } });
  const queue = new PaperPhotoQueue(91);
  queue.resume();
  await queue.waitForUploads();
  expect(queue.snapshot().restored?.status).toBe('registered');
  expect(mockUpload).not.toHaveBeenCalled();
  expect(mockCalls.find((c) => c.path.endsWith('/handwriting'))?.init).toMatchObject({ params: { path: { sessionId: 91, seq: 2 } }, body: { object_keys: ['u/1/hw/restored.jpg'], idempotency_key: 'restored', capture_order: 123 } });
});

it('确认成功但响应丢失时先恢复确认结果，旧草稿不会覆盖已追加文字', async () => {
  const fixture = backgroundPhotos();
  const confirm = mockResponses['PUT /paper-sessions/{sessionId}/items/{seq}/handwriting/{handwritingId}']!;
  let online = false;
  let processed = false;
  mockResponses['PUT /paper-sessions/{sessionId}/items/{seq}/handwriting/{handwritingId}'] = (init) => {
    if (!processed) { processed = true; confirm(init); }
    if (!online) throw new Error('确认响应丢失');
    return confirm(init);
  };
  const r = await wrap(<PaperSessionPage />);
  await takePaperPhoto();
  await waitFor(() => expect(fixture.jobs()).toHaveLength(1));
  fixture.ready(1, '已在服务端确认的文字'); await fixture.refresh();
  await fireEvent.press(await screen.findByText('确认并填入答案'));
  await fixture.refresh();
  expect(fixture.drafts[1]).toBe('第一题已有答案\n已在服务端确认的文字');
  expect(screen.getByLabelText('你的答案').props.editable).toBe(false);
  expect(Object.values(paperPhotoQueue(90).snapshot())[0]?.confirmingText).toBe('已在服务端确认的文字');
  await fireEvent.press(screen.getByText('下一题'));
  await fireEvent.changeText(screen.getByLabelText('你的答案'), '第二题继续写');
  await fireEvent.press(screen.getByText('上一题'));
  online = true;
  await fireEvent.press(screen.getByText('重试保存识别结果'));
  expect(await screen.findByDisplayValue('第一题已有答案\n已在服务端确认的文字')).toBeTruthy();
  expect(fixture.drafts[1]).toBe('第一题已有答案\n已在服务端确认的文字');
  expect(getJSON<Record<number, string>>('draft:paper:90')?.[2]).toBe('第二题继续写');
  expect(Object.values(paperPhotoQueue(90).snapshot())[0]?.confirmingText).toBeUndefined();
  await r.unmount();
});


it('整卷录音停止后可切题，识别重试保留语音类型与原题', async () => {
  const fixture = backgroundPhotos();
  storage.set('permission_explained_microphone', true);
  mockResponses['GET /feature-flags'] = () => ({ status: 200, data: { flags: { voice_answer: true } } });
  mockResponses['POST /voice/upload-requests'] = () => ({ status: 200, data: { object_key: 'u/1/voice/exam.m4a', upload_url: 'https://oss/voice', upload_headers: {} } });
  const r = await wrap(<PaperSessionPage />);
  await fireEvent.press(await screen.findByText('语音输入'));
  await fireEvent.press(await screen.findByText('结束录音 0:00'));
  await waitFor(() => expect(fixture.jobs()[0]?.input_kind).toBe('voice'));
  await fireEvent.press(screen.getByText('下一题'));
  expect(screen.queryByText('语音正在后台识别')).toBeNull();
  await fireEvent.press(screen.getByText('上一题'));
  expect(await screen.findByText('语音正在后台识别')).toBeTruthy();
  fixture.failed(1); await fixture.refresh();
  await fireEvent.press(await screen.findByText('重试识别'));
  const registers = mockCalls.filter((call) => call.path.endsWith('/handwriting'));
  expect(registers).toHaveLength(2);
  expect(registers[1]?.init).toMatchObject({ params: { path: { sessionId: 90, seq: 1 } }, body: { input_kind: 'voice', object_keys: ['u/1/voice/exam.m4a'] } });
  await r.unmount();
});
