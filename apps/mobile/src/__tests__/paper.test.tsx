import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import PaperModePage from '../../app/paper/[id]';
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
}));
let mockUuid = 0;
jest.mock('expo-crypto', () => ({ randomUUID: () => `uuid-${++mockUuid}` }));

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
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
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
  mockPush.mockReset();
  mockReplace.mockReset();
  storage.clearAll();
  mockParams = {};
  mockResponses = {};
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
    expect(await screen.findByText('第2题题干')).toBeTruthy();
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
    await fireEvent.press(screen.getByText('退出'));
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
