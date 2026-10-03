import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import TrainTab from '../../app/(tabs)/train';
import PracticeRunner from '../../app/practice/[id]';
import { storage } from '../lib/storage';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({ id: '50' }),
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

const termQ = {
  id: 201, qtype: 'term', stem: '意境', answer: '意境是情景交融、虚实相生、韵味无穷的艺术境界。', source: 'exam', exam_year: 2024, origin_tags: [],
  knowledge_points: [{ id: 3, name: '意境', is_primary: true }], rubric_count: 3, source_ref: { material_id: 1, file_name: '文学理论名词解释.docx', page: 3 },
};
const session = { id: 50, kind: 'type_drill', title: '题型专项 · 名词解释', subject_id: 7, status: 'in_progress', cursor_index: 0, started_at: '2026-10-02T02:00:00Z', done_count: 0, questions: [termQ] };
const grading = (extra = {}) => ({
  grading_id: 9, attempt_id: 1, question_id: 201, status: 'done', score: 3, full_score: 5, counts: { hit: 1, partial: 1, miss: 1 },
  points: [
    { seq: 1, content: '情景交融', score: 2, got: 2, verdict: 'hit', quote: '情感和景物融合在一起' },
    { seq: 2, content: '韵味无穷', score: 1.5, got: 1, verdict: 'partial', quote: '言外之意', reason: '意思对，但没用规范表述' },
    { seq: 3, content: '虚实相生', score: 1.5, got: 0, verdict: 'miss', reason: '没有写到' },
  ],
  rubric_version: 1, rubric_source: 'user_confirmed', rubric_ref: { material_id: 1, file_name: '文学理论名词解释.docx', page: 3 }, structure_ok: true,
  suggestions: ['补上「虚实相生」'], loss: [{ type: 'knowledge', points: 1.5, reason: '「虚实相生」没记住' }], kp_changes: [{ kp_id: 3, name: '意境', from: 'learning', to: 'consolidating', m: 51 }],
  wrong_book: 'added', reference_answer: termQ.answer, trigger: 'submit', disputed: false, quota_charged: true, ...extra,
});
const quota = (used: number) => ({ status: 200, data: { items: [{ quota_type: 'grading', used, limit: 3, period: 'daily' }], membership: { is_member: false } } });

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  storage.clearAll();
  mockResponses = {
    'GET /practice-sessions/{sessionId}': () => ({ status: 200, data: session }),
    'GET /quota': () => quota(2),
    'GET /feature-flags': () => ({ status: 200, data: { flags: {} } }),
  };
});

const answer = '意境是中国古典诗歌中的重要范畴，指诗人的情感和景物融合在一起，读者可以体会言外之意。';

describe('4.4 主观题 → 4.7 批改结果', () => {
  it('作答、字数提示、剩余次数，提交后显示逐采分点批改与失分归因', async () => {
    mockResponses['POST /practice-sessions/{sessionId}/gradings'] = () => ({ status: 200, data: grading() });
    await wrap(<PracticeRunner />);
    expect(await screen.findByText('今日免费批改还剩 1 次')).toBeTruthy();
    expect(screen.getByText('出自 文学理论名词解释.docx · 按你资料里的 3 个采分点批改')).toBeTruthy();
    expect(screen.queryByText('语音')).toBeNull(); // 语音作答开关默认关闭，不显示入口
    await fireEvent.changeText(screen.getByLabelText('你的答案'), answer);
    expect(screen.getByText(/字 · 建议 80–150/)).toBeTruthy();
    await fireEvent.press(screen.getByText('提交批改'));
    expect(await screen.findByText('/ 5 分')).toBeTruthy();
    expect(screen.getByText('命中 1 · 部分命中 1 · 遗漏 1')).toBeTruthy();
    expect(screen.getByText('你写的是「情感和景物融合在一起」')).toBeTruthy();
    expect(screen.getByText('知识没掌握 −1.5')).toBeTruthy();
    expect(screen.getByText('已加入错题本')).toBeTruthy();
    expect(screen.getByText(/批改依据：你确认过的采分点 · 出自 文学理论名词解释.docx 第 3 页/)).toBeTruthy();
    const body = (mockCalls.find((c) => c.path === '/practice-sessions/{sessionId}/gradings')?.init as { body: Record<string, unknown> }).body;
    expect(body).toMatchObject({ question_id: 201, answer_text: answer, answer_mode: 'typed', timed: false });
    await fireEvent.press(screen.getByText('学知识点'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/bank/kp/[id]', params: { id: '3' } });
  });

  it('次数用完（4.9）：答案已保存，可以明天再批改', async () => {
    mockResponses['GET /quota'] = () => quota(3);
    mockResponses['POST /practice-sessions/{sessionId}/gradings'] = () => ({ status: 200, data: { ...grading({ status: 'queued_quota', points: [], loss: [], suggestions: [], kp_changes: [], wrong_book: 'none' }), score: undefined, counts: undefined } });
    await wrap(<PracticeRunner />);
    await fireEvent.changeText(await screen.findByLabelText('你的答案'), answer);
    await fireEvent.press(screen.getByText('提交批改'));
    expect(await screen.findByText('今天的免费批改次数用完了')).toBeTruthy();
    await fireEvent.press(screen.getByText('明天再批改'));
    expect(await screen.findByText('答案已保存为待批改，明天 0 点后在训练页一键提交')).toBeTruthy();
  });

  it('批改失败：未扣除次数，重试成功', async () => {
    let fail = true;
    mockResponses['POST /practice-sessions/{sessionId}/gradings'] = () =>
      fail ? { status: 503, error: { code: 'AI_FAILED', message: 'AI 生成失败' } } : { status: 200, data: grading() };
    await wrap(<PracticeRunner />);
    await fireEvent.changeText(await screen.findByLabelText('你的答案'), answer);
    await fireEvent.press(screen.getByText('提交批改'));
    expect(await screen.findByText('生成失败，未扣除次数')).toBeTruthy();
    fail = false;
    await fireEvent.press(screen.getByText('重试'));
    expect(await screen.findByText('/ 5 分')).toBeTruthy();
    const keys = mockCalls.filter((c) => c.method === 'POST').map((c) => (c.init as { body: { idempotency_key: string } }).body.idempotency_key);
    expect(new Set(keys).size).toBe(2);
  });

  it('批改异议（4.8）：复核重批一次，显示新结果', async () => {
    mockResponses['POST /practice-sessions/{sessionId}/gradings'] = () => ({ status: 200, data: grading() });
    mockResponses['POST /gradings/{gradingId}/disputes'] = () => ({ status: 200, data: grading({ grading_id: 10, score: 4, trigger: 'dispute_recheck', disputed: true, quota_charged: false, parent_grading_id: 9 }) });
    await wrap(<PracticeRunner />);
    await fireEvent.changeText(await screen.findByLabelText('你的答案'), answer);
    await fireEvent.press(screen.getByText('提交批改'));
    await fireEvent.press(await screen.findByText('有异议'));
    await fireEvent.press(screen.getByLabelText('采分点本身不对'));
    expect(screen.getByText('去修改 ›')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('我其实答到了某个采分点'));
    await fireEvent.press(screen.getByLabelText('允许后台查看这道题和我的答案，用于排查批改问题'));
    await fireEvent.press(screen.getByText('提交'));
    expect(await screen.findByText('复核重批的结果，未消耗批改次数')).toBeTruthy();
    expect(screen.getByText('已复核')).toBeTruthy();
    expect(mockCalls.find((c) => c.path === '/gradings/{gradingId}/disputes')?.init).toEqual({
      params: { path: { gradingId: 9 } },
      body: { reason: 'hit_missed', note: undefined, allow_access: true },
    });
  });

  it('草稿在离开时保存', async () => {
    const r = await wrap(<PracticeRunner />);
    await fireEvent.changeText(await screen.findByLabelText('你的答案'), '意境是情景交融');
    await r.unmount();
    expect(storage.getString('draft:practice:50:201')).toBe(JSON.stringify('意境是情景交融'));
  });
});

describe('待批改', () => {
  it('训练页一键提交', async () => {
    mockResponses['GET /subjects'] = () => ({ status: 200, data: { items: [{ id: 7, name: '语言文学基础', full_score: 150, is_essay: false, bank_id: 1, question_count: 3, kp_count: 2, material_count: 1 }], max_subjects: 3, can_add: true } });
    mockResponses['GET /subjects/{subjectId}/practice'] = () => ({ status: 200, data: { subject_id: 7, stage: 'strengthen', total_questions: 3, recite_due: 0, paper_first: false, qtype_counts: [], wrong_book: { total: 0, due: 0 } } });
    mockResponses['GET /gradings/pending'] = () => ({ status: 200, data: { remaining_today: 3, items: [{ grading_id: 9, question_id: 201, qtype: 'term', stem: '意境', saved_at: '2026-10-01T10:00:00Z' }] } });
    mockResponses['POST /gradings/pending/submit'] = () => ({ status: 200, data: { graded: 1, remaining_pending: 0, results: [grading({ trigger: 'pending_resubmit' })] } });
    await wrap(<TrainTab />);
    expect(await screen.findByText('1 道主观题待批改')).toBeTruthy();
    await fireEvent.press(screen.getByText('一键提交'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith({ pathname: '/practice/grading/[id]', params: { id: '9' } }));
  });
});
