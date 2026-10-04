import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import TrainTab from '../../app/(tabs)/train';
import CustomPractice from '../../app/practice/custom';
import PracticeRunner from '../../app/practice/[id]';
import WrongBookPage from '../../app/practice/wrong';
import { judge } from '../features/practice/judge';
import { cacheSession, flush, usePending } from '../features/practice/offline';
import { storage } from '../lib/storage';

const mockPush = jest.fn();
const mockReplace = jest.fn();
const mockBack = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: () => mockBack() },
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
  if (v === 'network') return Promise.reject(new TypeError('Network request failed'));
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

const subjects = { items: [{ id: 7, name: '语言文学基础', code: '654', full_score: 150, is_essay: false, bank_id: 1, question_count: 30, kp_count: 10, material_count: 2 }], max_subjects: 3, can_add: true };
const choiceQ = (id: number, extra = {}) => ({
  id, qtype: 'single_choice', stem: `第 ${id} 题：下列属于唐传奇的是`, options: [{ key: 'A', text: '莺莺传' }, { key: 'B', text: '搜神记' }], answer: 'A', analysis: '《莺莺传》是元稹的唐传奇。',
  source: 'exam', exam_year: 2024, origin_tags: [], knowledge_points: [{ id: 3, name: '唐传奇', is_primary: true }], rubric_count: 0, ...extra,
});
const session = (extra = {}) => ({
  id: 50, kind: 'type_drill', title: '题型专项 · 单选', subject_id: 7, status: 'in_progress', cursor_index: 0, started_at: '2026-10-02T02:00:00Z', done_count: 0,
  questions: [choiceQ(101, { plan_group: 'review' }), choiceQ(102, { origin_tags: ['ai_generated'], source: 'ai_generated', source_ref: { material_id: 1, file_name: '讲义.pdf', page: 4 } })],
  ...extra,
});

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockReplace.mockReset();
  mockBack.mockReset();
  mockParams = {};
  storage.clearAll();
  usePending.getState().set(0);
  mockResponses = { 'GET /subjects': () => ({ status: 200, data: subjects }) };
});

describe('客观题离线判分', () => {
  it('与服务端规则一致', () => {
    expect(judge('single_choice', undefined, 'A', ['A'], '')).toBe(true);
    expect(judge('multi_choice', undefined, 'A、C', ['C', 'A'], '')).toBe(true);
    expect(judge('multi_choice', undefined, 'AC', ['A'], '')).toBe(false);
    expect(judge('true_false', [{ key: 'A', text: '正确' }, { key: 'B', text: '错误' }], '对', ['A'], '')).toBe(true);
    expect(judge('fill_blank', undefined, '刘勰；文心雕龙', [], '刘勰;《文心雕龙》')).toBe(true);
    expect(judge('single_choice', undefined, '', ['A'], '')).toBeNull();
  });
});

describe('4.1 训练首页', () => {
  it('今日训练、按题型练、错题本；点题型开始专项', async () => {
    mockResponses['GET /subjects/{subjectId}/practice'] = () => ({
      status: 200,
      data: {
        subject_id: 7, stage: 'strengthen', total_questions: 30, recite_due: 10, paper_first: false,
        today: { total: 12, done: 0, minutes: 44 }, type_drill: { qtype: 'term', done_this_week: 2, weekly_target: 3 },
        qtype_counts: [{ qtype: 'term', count: 18 }, { qtype: 'single_choice', count: 12 }], wrong_book: { total: 23, due: 8 },
      },
    });
    mockResponses['POST /practice-sessions'] = () => ({ status: 201, data: session() });
    await wrap(<TrainTab />);
    expect(await screen.findByText('0 / 12 · 约 44 分钟')).toBeTruthy();
    expect(screen.getByText('本周题型专项 · 名词解释')).toBeTruthy();
    expect(screen.getByText('23 题 · 8 题到了复习日')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('练单选'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith({ pathname: '/practice/[id]', params: { id: '50' } }));
    expect(mockCalls.find((c) => c.path === '/practice-sessions')?.init).toEqual({ body: { subject_id: 7, kind: 'type_drill', qtype: 'single_choice', ai_fill: false } });
  });
});

describe('4.3 客观题', () => {
  it('选中即判分，显示服务端结果；做完进入本组总结', async () => {
    mockParams = { id: '50' };
    mockResponses['GET /practice-sessions/{sessionId}'] = () => ({ status: 200, data: session() });
    mockResponses['POST /practice-sessions/{sessionId}/attempts'] = () => ({
      status: 200, data: { attempt_id: 1, is_correct: false, correct_answer: 'A', analysis: '《莺莺传》是元稹的唐传奇。', wrong_book: 'added', kp_changes: [] },
    });
    await wrap(<PracticeRunner />);
    expect(await screen.findByText('题型专项 · 单选 · 1 / 2')).toBeTruthy();
    expect(screen.getByText('到期复习')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('B. 搜神记'));
    expect(await screen.findByText('回答错误')).toBeTruthy();
    expect(await screen.findByText('已加入错题本')).toBeTruthy();
    const body = (mockCalls.find((c) => c.method === 'POST')?.init as { body: Record<string, unknown> }).body;
    expect(body).toMatchObject({ question_id: 101, selected: ['B'], revealed: false, offline: false });
    await fireEvent.press(screen.getByText('下一题'));
    expect(await screen.findByText('AI 出题')).toBeTruthy();
    await fireEvent.press(screen.getByText('不会，看答案'));
    expect(await screen.findByText('先看答案，记下来')).toBeTruthy();
    expect(screen.getByText(/AI 按你的知识点「唐传奇」出题 · 依据 讲义.pdf 第 4 页/)).toBeTruthy();
    await fireEvent.press(screen.getByText('完成本组'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith({ pathname: '/practice/summary/[id]', params: { id: '50' } }));
  });

  it('飞行模式：本地判分、进队列，联网后补交（offline=true）', async () => {
    mockParams = { id: '50' };
    mockResponses['GET /practice-sessions/{sessionId}'] = () => ({ status: 200, data: session() });
    mockResponses['POST /practice-sessions/{sessionId}/attempts'] = () => 'network';
    await wrap(<PracticeRunner />);
    await fireEvent.press(await screen.findByLabelText('A. 莺莺传'));
    expect(await screen.findByText('回答正确')).toBeTruthy();
    expect(screen.getByText('离线作答，联网后自动提交并复核')).toBeTruthy();
    expect(usePending.getState().count).toBe(1);

    mockResponses['POST /practice-sessions/{sessionId}/attempts'] = () => ({ status: 200, data: { attempt_id: 9, is_correct: true, wrong_book: 'none', kp_changes: [] } });
    expect(await flush()).toBe(1);
    expect(usePending.getState().count).toBe(0);
    const posts = mockCalls.filter((c) => c.method === 'POST');
    const last = (posts[posts.length - 1]!.init as { body: Record<string, unknown> }).body;
    expect(last).toMatchObject({ question_id: 101, selected: ['A'], offline: true });
    expect(typeof last.answered_at).toBe('string');
  });

  it('断网时用本地缓存的会话继续做', async () => {
    mockParams = { id: '50' };
    // 之前联网时拉过这组题（含答案），已缓存在本地。
    cacheSession(session() as never);
    mockResponses['GET /practice-sessions/{sessionId}'] = () => 'network';
    await wrap(<PracticeRunner />);
    expect(await screen.findByText('题型专项 · 单选 · 1 / 2')).toBeTruthy();
  });

  it('退出训练确认（4.10）：保存断点', async () => {
    mockParams = { id: '50' };
    mockResponses['GET /practice-sessions/{sessionId}'] = () => ({ status: 200, data: session({ questions: [choiceQ(101, { answered: { is_correct: true, revealed: false } }), choiceQ(102)] }) });
    mockResponses['PUT /practice-sessions/{sessionId}/progress'] = () => ({ status: 204 });
    await wrap(<PracticeRunner />);
    expect(await screen.findByText('题型专项 · 单选 · 2 / 2')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('退出训练'));
    expect(screen.getByText('题型专项 · 单选已完成 1 / 2 题，进度已保存，下次从第 2 题继续。')).toBeTruthy();
    await fireEvent.press(screen.getByText('退出'));
    await waitFor(() => expect(mockBack).toHaveBeenCalled());
    expect(mockCalls.find((c) => c.method === 'PUT')?.init).toMatchObject({ body: { cursor_index: 1 } });
  });
});

describe('4.2 自定义练习', () => {
  it('实时显示题数与用时，按条件开始', async () => {
    mockParams = { subjectId: '7' };
    mockResponses['GET /subjects/{subjectId}/practice'] = () => ({
      status: 200,
      data: { subject_id: 7, stage: 'strengthen', total_questions: 30, recite_due: 0, paper_first: false, qtype_counts: [{ qtype: 'single_choice', count: 2 }], wrong_book: { total: 0, due: 0 } },
    });
    mockResponses['GET /subjects/{subjectId}/knowledge-tree'] = () => ({ status: 200, data: { sections: [{ id: 1, name: '文学理论' }], needs_review_count: 0 } });
    mockResponses['POST /subjects/{subjectId}/practice/preview'] = () => ({ status: 200, data: { available: 2, count: 2, ai_fill: 3, minutes: 7.5 } });
    mockResponses['POST /practice-sessions'] = () => ({ status: 201, data: session({ kind: 'custom' }) });
    await wrap(<CustomPractice />);
    await fireEvent.press(await screen.findByLabelText('单选 2'));
    await fireEvent(screen.getByLabelText('题库不够时 AI 出变式题'), 'valueChange', true);
    expect(await screen.findByText('符合条件 2 题 · 本组 5 题（AI 补 3 题） · 约 8 分钟')).toBeTruthy();
    await fireEvent.press(screen.getByText('开始练习'));
    await waitFor(() =>
      expect(mockCalls.find((c) => c.path === '/practice-sessions')?.init).toEqual({
        body: { subject_id: 7, kind: 'custom', config: { section_ids: [], qtypes: ['single_choice'], count: 10, only_unmastered: false, ai_fill: true }, ai_fill: false },
      }),
    );
  });
});

describe('4.12 错题本', () => {
  it('统计、分组与重做', async () => {
    mockParams = { subjectId: '7' };
    mockResponses['GET /subjects/{subjectId}/wrong-book'] = () => ({
      status: 200,
      data: {
        total: 2, to_redo: 1, week_new: 2, eliminated: 5,
        items: [
          { question_id: 1, qtype: 'single_choice', stem: '关于意境最准确的表述', source: 'exam', exam_year: 2024, wrong_count: 1, added_reason: 'wrong', kp: { id: 3, name: '意境' } },
          { question_id: 2, qtype: 'term', stem: '意境', source: 'exercise', wrong_count: 2, added_reason: 'partial', last_score_rate: 0.6, loss_type: 'norm', kp: { id: 3, name: '意境' } },
        ],
      },
    });
    mockResponses['POST /practice-sessions'] = () => ({ status: 201, data: session({ kind: 'wrong_redo' }) });
    await wrap(<WrongBookPage />);
    expect(await screen.findByText('意境 · 2 题')).toBeTruthy();
    expect(screen.getByText('5')).toBeTruthy();
    expect(screen.getByText('习题 · 最近得分率 60%')).toBeTruthy();
    await fireEvent.press(screen.getByText('按失分原因'));
    expect(screen.getByText('答题不规范 · 1 题')).toBeTruthy();
    await fireEvent.press(screen.getByText('重做全部 2 题'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/practice-sessions')?.init).toEqual({ body: { subject_id: 7, kind: 'wrong_redo', ai_fill: false } }));
  });
});
