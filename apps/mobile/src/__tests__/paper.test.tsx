import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import PaperModePage from '../../app/paper/[id]';
import PaperHistoryPage from '../../app/paper/history';
import { PaperTimer } from '../features/paper/timer';
import PaperList from '../../app/paper/list';
import PaperSessionPage from '../../app/paper/session/[id]';
import { clock } from '../features/paper/api';
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
jest.mock('expo-file-system', () => ({ File: class { size = 2048; upload = (...a: unknown[]) => mockUpload(...(a as [])); } }));
jest.mock('expo-image-picker', () => ({
  requestCameraPermissionsAsync: async () => ({ granted: mockCameraGranted, canAskAgain: true }),
  launchCameraAsync: async () => ({ canceled: false, assets: [{ uri: 'file:///paper-answer.jpg', mimeType: 'image/jpeg', fileSize: 2048 }] }),
}));
let queryClient: QueryClient;
const mockCalls: { method: string; path: string; init?: unknown }[] = [];
let mockResponses: Record<string, () => unknown> = {};
function mockRespond(method: string, path: string, init?: unknown) {
  mockCalls.push({ method, path, init });
  const r = mockResponses[`${method} ${path}`];
  const v = r ? r() : { status: 200, data: {} };
  const { status, data, error } = v as { status: number; data?: unknown; error?: unknown };
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
  mockUpload.mockClear();
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
    mockResponses['GET /paper-sessions/{sessionId}'] = () => ({ status: 200, data: session({ items: [item(1, { draft_text: '原有要点' })] }) });
    mockResponses['POST /handwriting/upload-requests'] = () => ({ status: 200, data: { items: [{ object_key: 'u/1/hw/paper.jpg', upload_url: 'https://oss/paper', upload_headers: {} }] } });
    mockResponses['POST /handwriting/recognize'] = () => ({ status: 200, data: { text: '识别的要点', low_confidence: [], uncertain_count: 0, pages: [] } });
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
    expect(mockCalls.find((c) => c.method === 'PUT')?.init).toMatchObject({ body: { draft_text: '原有要点\n核对后的要点' } });
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
