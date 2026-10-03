import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import DashboardPage from '../../app/dashboard';
import PaperReportPage from '../../app/paper/report/[id]';
import TimeReportPage from '../../app/paper/time/[id]';

const mockPush = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));

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
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, refetchInterval: false } } });
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  );
}

const time = {
  total_minutes: 180, used_minutes: 180, used_full: true, unanswered: 2, time_loss: 13,
  sections: [
    { qtype: 'term', count: 6, answered: 6, suggested_minutes: 25, actual_minutes: 38, status: 'overtime', diff_minutes: 13, unfinished: false },
    { qtype: 'discussion', count: 2, answered: 1, suggested_minutes: 80, actual_minutes: 70, status: 'ok', diff_minutes: -10, unfinished: true },
  ],
  check_suggested_minutes: 15, check_actual_minutes: 0, check_status: 'under',
  conclusion: '名词解释超时 13 分钟，论述有 1 题没写', advice: ['论述留足 80 分钟，先写分值高的题，每题先列要点再展开', '最后留 15 分钟检查'],
  trend: [{ session_id: 80, date: '2026-09-20T08:00:00Z', unanswered: 3 }, { session_id: 90, date: '2026-10-02T08:00:00Z', unanswered: 2 }],
};
const report = (over: object = {}) => ({
  session_id: 90, paper_id: 5, subject_id: 7, title: '2023 年真题', kind: 'real_exam', mode: 'mock', score: 98, full_score: 150, counts_for_estimate: true,
  graded_at: '2026-10-02T08:00:00Z', prev_delta: 6, target_score: 115, gap: 17,
  by_qtype: [{ qtype: 'term', got: 22, full: 30 }, { qtype: 'short_answer', got: 36, full: 60 }, { qtype: 'discussion', got: 40, full: 60 }],
  loss: { knowledge: 26, norm: 13, time: 13 }, loss_total: 52, weakest_qtype: 'short_answer', time, ...over,
});
const estimate = { subject_id: 7, name: '语言文学基础', is_essay: false, full_score: 150, target_score: 115, ready: true, low: 98, high: 106, gap: 9,
  main_gap_qtype: 'discussion', basis_papers: 2, basis_questions: 20 };
const dashboard = (over: object = {}) => ({
  estimate, trend: [{ week_start: '2026-09-21', low: 92, high: 100, mid: 96 }, { week_start: '2026-09-28', low: 98, high: 106, mid: 102 }],
  loss_points: { knowledge: 20, norm: 10, time: 10 }, loss_shares: { knowledge: 0.5, norm: 0.25, time: 0.25 }, sections_ready: true,
  sections: [{ id: 1, name: '中国古代文学', share: 0.3, mastery: 38, kp_count: 12 }],
  false_mastery: [{ kp_id: 3, name: '意境' }, { kp_id: 4, name: '典型' }],
  recent_papers: [{ session_id: 90, title: '2023 年真题', kind: 'real_exam', mode: 'mock', score: 98, full_score: 150, counts_for_estimate: true, graded_at: '2026-10-02T08:00:00Z' }],
  ...over,
});
const subjects = { items: [{ id: 7, name: '语言文学基础', code: '654', full_score: 150, target_score: 115, is_essay: false, bank_id: 1, question_count: 246, kp_count: 58, material_count: 2 },
  { id: 8, name: '作文', code: '908', full_score: 150, is_essay: true, bank_id: 2, question_count: 0, kp_count: 0, material_count: 0 }], max_subjects: 3, can_add: true };

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockParams = { id: '90' };
  mockResponses = {};
});

describe('4.24 整卷报告', () => {
  it('分数、较上次、目标差距、题型得分、失分归因、时间分析入口；针对失分练一组按最弱题型出题', async () => {
    mockResponses['GET /paper-sessions/{sessionId}/report'] = () => ({ status: 200, data: report() });
    mockResponses['POST /practice-sessions'] = () => ({ status: 201, data: { id: 300, items: [] } });
    const r = await wrap(<PaperReportPage />);
    expect(await screen.findByText('AI 批改得分 · 仅供参考')).toBeTruthy();
    expect(screen.getByText('较上次 +6')).toBeTruthy();
    expect(screen.getByText('目标 115 · 差 17')).toBeTruthy();
    expect(screen.getByText('36/60')).toBeTruthy();
    expect(screen.getByText('失分归因 · 共失 52 分')).toBeTruthy();
    expect(screen.getByText('按你资料里的采分点批改 · 已计入预估分')).toBeTruthy();
    expect(screen.getByText('名词解释超时 13 分钟，论述有 1 题没写')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('时间分析报告'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/paper/time/[id]', params: { id: '90' } });
    await fireEvent.press(screen.getByText('针对失分练一组'));
    await waitFor(() => expect(mockCalls.some((c) => c.path === '/practice-sessions')).toBe(true));
    expect(mockCalls.find((c) => c.path === '/practice-sessions')?.init).toMatchObject({ body: { subject_id: 7, kind: 'custom', config: { qtypes: ['short_answer'] } } });
    await r.unmount();
  });

  it('AI 组卷与练习模式：标明不计入预估分，没有时间分析', async () => {
    mockResponses['GET /paper-sessions/{sessionId}/report'] = () => ({ status: 200, data: report({ kind: 'ai_standard', mode: 'practice', counts_for_estimate: false, time: undefined, prev_delta: undefined }) });
    const r = await wrap(<PaperReportPage />);
    expect(await screen.findByText('按你资料里的采分点批改 · AI 组卷的成绩只作参考，不计入预估分')).toBeTruthy();
    expect(screen.queryByLabelText('时间分析报告')).toBeNull();
    expect(screen.queryByText(/较上次/)).toBeNull();
    await r.unmount();
  });

  it('还在批改时显示批改中', async () => {
    mockResponses['GET /paper-sessions/{sessionId}/report'] = () => ({ status: 409, error: { code: 'CONFLICT', message: '这套卷还在批改' } });
    const r = await wrap(<PaperReportPage />);
    expect(await screen.findByText('正在整卷批改')).toBeTruthy();
    await r.unmount();
  });
});

describe('4.25 时间分析报告', () => {
  it('用满时间、未答、时间失分；各题型超时与没写完标出；没有留出检查；下次分配建议；未答题趋势', async () => {
    mockResponses['GET /paper-sessions/{sessionId}/report'] = () => ({ status: 200, data: report() });
    const r = await wrap(<TimeReportPage />);
    expect(await screen.findByText('用满时间')).toBeTruthy();
    expect(screen.getByText('2 题')).toBeTruthy();
    expect(screen.getByText('约 13')).toBeTruthy();
    expect(screen.getByText('超时 13′')).toBeTruthy();
    expect(screen.getByText('1 题未完')).toBeTruthy();
    expect(screen.getByText('没有留出')).toBeTruthy();
    expect(screen.getByText('· 最后留 15 分钟检查')).toBeTruthy();
    expect(screen.getByText('3 → 2')).toBeTruthy();
    await fireEvent.press(screen.getByText('按建议再做一套'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/paper/[id]', params: { id: '5' } });
    await r.unmount();
  });
});

describe('6.2 提分看板', () => {
  beforeEach(() => {
    mockParams = { subjectId: '7' };
    mockResponses['GET /subjects'] = () => ({ status: 200, data: subjects });
  });

  it('预估分与趋势、失分归因占比（点了看错题）、板块掌握度 × 分值占比、以为会了加入今日训练、最近整卷', async () => {
    mockResponses['GET /subjects/{subjectId}/dashboard'] = () => ({ status: 200, data: dashboard() });
    mockResponses['POST /subjects/{subjectId}/false-mastery/plan'] = () => ({ status: 200, data: { added: 2 } });
    const r = await wrap(<DashboardPage />);
    expect(await screen.findByLabelText('预估分 98 到 106')).toBeTruthy();
    expect(screen.getByText('W2')).toBeTruthy();
    expect(screen.getByText('目标 115')).toBeTruthy();
    expect(screen.getByText('50% ›')).toBeTruthy();
    expect(screen.getByText('占30%')).toBeTruthy();
    expect(screen.getByText('2 个「以为会了」')).toBeTruthy();
    await fireEvent.press(screen.getByText('加入今日训练'));
    await waitFor(() => expect(mockCalls.some((c) => c.method === 'POST' && c.path === '/subjects/{subjectId}/false-mastery/plan')).toBe(true));
    await fireEvent.press(screen.getAllByText('25% ›')[0]!);
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/practice/wrong', params: { subjectId: '7', group: 'loss' } });
    await fireEvent.press(screen.getByLabelText('2023 年真题'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/paper/report/[id]', params: { id: '90' } });
    await r.unmount();
  });

  it('切换专业课；还没做整卷的课提示去做整卷', async () => {
    mockResponses['GET /subjects/{subjectId}/dashboard'] = () => {
      const last = [...mockCalls].reverse().find((c) => c.path === '/subjects/{subjectId}/dashboard');
      const sid = (last?.init as { params: { path: { subjectId: number } } }).params.path.subjectId;
      return sid === 8
        ? { status: 200, data: dashboard({ estimate: { subject_id: 8, name: '作文', is_essay: true, full_score: 150, ready: false }, trend: [], loss_points: { knowledge: 0, norm: 0, time: 0 }, loss_shares: { knowledge: 0, norm: 0, time: 0 }, sections: [], sections_ready: false, false_mastery: [], recent_papers: [] }) }
        : { status: 200, data: dashboard() };
    };
    const r = await wrap(<DashboardPage />);
    expect(await screen.findByLabelText('预估分 98 到 106')).toBeTruthy();
    await fireEvent.press(screen.getByText('908 作文'));
    expect(await screen.findByText('做完一套导入的真题卷后生成预估分，AI 组卷的成绩不计入')).toBeTruthy();
    expect(screen.getByText('近 30 天还没有主观题批改，做几道主观题后显示')).toBeTruthy();
    await fireEvent.press(screen.getByText('去做整卷'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/paper/list', params: { subjectId: '8' } });
    await r.unmount();
  });
});
