import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import TodayTab from '../../app/(tabs)/today';
import TodaySummaryPage from '../../app/plan/summary';

const mockPush = jest.fn();
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: jest.fn(), back: jest.fn() },
  useLocalSearchParams: () => ({}),
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

const bank = (extra = {}) => ({
  subject_id: 7, name: '语言文学基础', code: '654', is_essay: false, question_count: 246, kp_count: 58, organizing: false,
  mastery_distribution: { unlearned: 22, learning: 15, consolidating: 9, mastered: 12 }, ...extra,
});
const plan = (extra = {}) => ({
  date: '2026-10-02', stage: 'strengthen', budget_minutes: 45, total_minutes: 44.5, done_minutes: 0, completed: false,
  groups: [{ group: 'review', count: 12, done: 0, minutes: 18 }, { group: 'weak', count: 16, done: 0, minutes: 20 }, { group: 'recite', count: 10, done: 0, minutes: 6.5 }],
  items: [{ group: 'review', kp_id: 1, subject_id: 7, question_id: 3, qtype: 'term', minutes: 1.5, done: false }],
  ...extra,
});
const home = (extra = {}) => ({
  state: 'normal', days_to_exam: 83, stage: 'strengthen', estimates: [], false_mastery_count: 3, streak_days: 5, unread_messages: 0,
  tomorrow_minutes: 45, banks: [bank()], plan: plan(),
  stage_push: { kind: 'weekly_qtype_drill', title: '本周题型专项', desc: '每周 3 次，专练真题里分值最高的题型', qtype: 'term' },
  ...extra,
});
const subjects = { items: [{ id: 7, name: '语言文学基础', code: '654', full_score: 150, target_score: 115, is_essay: false, bank_id: 1, question_count: 246, kp_count: 58, material_count: 2 }], max_subjects: 3, can_add: true };

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockResponses = {
    'GET /subjects': () => ({ status: 200, data: subjects }),
    'GET /import-jobs': () => ({ status: 200, data: { items: [] } }),
    'GET /messages/unread-count': () => ({ status: 200, data: { count: 3 } }),
    'GET /plans/today/summary': () => ({ status: 200, data: { streak_days: 5, question_count: 38, correct_rate: 0.78, minutes: 46.2, new_mastered: 3, loss_shares: { knowledge: 0.62, norm: 0.38 }, mastery_changes: [] } }),
  };
});

describe('2.1 今日首页', () => {
  it('正常：倒计时、阶段、今日训练分组、题型专项、我的题库、以为会了', async () => {
    mockResponses['GET /home'] = () => ({ status: 200, data: home() });
    await wrap(<TodayTab />);
    expect(await screen.findByText('83')).toBeTruthy();
    expect(screen.getByText('强化期')).toBeTruthy();
    expect(screen.getByText('今日训练')).toBeTruthy();
    expect(screen.getByLabelText('到期复习 12')).toBeTruthy();
    expect(screen.getByLabelText('题型专项 16')).toBeTruthy();
    expect(screen.getByText('本周题型专项 · 名词解释')).toBeTruthy();
    expect(screen.getByText('246 题 · 58 个知识点')).toBeTruthy();
    expect(screen.getByText('3 个知识点「以为会了」')).toBeTruthy();
    expect(screen.getByText('做完一套整卷后生成预估分')).toBeTruthy();
    await fireEvent.press(screen.getByText('开始训练'));
    expect(mockPush).toHaveBeenCalledWith('/(tabs)/train');
    // 铃铛：有未读时显示红点，点进 2.3。
    await fireEvent.press(await screen.findByLabelText('消息，3 条未读'));
    expect(mockPush).toHaveBeenCalledWith('/messages');
  });

  it('预估分卡（PRD 11.6）：区间、今天的变化、差距与主要差在、依据；点「提分看板」进 6.2', async () => {
    const est = { subject_id: 7, name: '语言文学基础', is_essay: false, full_score: 150, target_score: 115, ready: true, low: 98, high: 106, gap: 9,
      main_gap_qtype: 'discussion', basis_papers: 2, basis_questions: 20, today_change: 2 };
    mockResponses['GET /home'] = () => ({ status: 200, data: home({ estimates: [est] }) });
    await wrap(<TodayTab />);
    expect(await screen.findByLabelText('预估分 98 到 106')).toBeTruthy();
    expect(screen.getByText(/还差约 9 分 · 主要差在论述题/)).toBeTruthy();
    expect(screen.getByText('依据你导入的 2 套真题卷实测和近 20 道主观题估算')).toBeTruthy();
    expect(screen.getByText(/今天 \+2 分/)).toBeTruthy();
    expect(screen.queryByText('做完一套整卷后生成预估分')).toBeNull();
    await fireEvent.press(screen.getByText('提分看板'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/dashboard', params: { subjectId: '7' } });
  });

  it('还没导入资料（2.1c）', async () => {
    mockResponses['GET /home'] = () => ({ status: 200, data: home({ state: 'no_material', plan: undefined, stage_push: undefined, false_mastery_count: 0, banks: [bank({ question_count: 0, kp_count: 0, mastery_distribution: { unlearned: 0, learning: 0, consolidating: 0, mastered: 0 } })] }) });
    await wrap(<TodayTab />);
    expect(await screen.findByText('导入第一份资料，开始今天的训练')).toBeTruthy();
    expect(screen.getByText('还没导入资料')).toBeTruthy();
    expect(screen.queryByText('今日训练')).toBeNull();
  });

  it('今日已完成（2.1d）：打卡、今日数据、明天预计', async () => {
    mockResponses['GET /home'] = () => ({ status: 200, data: home({ state: 'done', plan: plan({ completed: true }), tomorrow_minutes: 50 }) });
    await wrap(<TodayTab />);
    expect(await screen.findByText('今天的训练完成了')).toBeTruthy();
    expect(await screen.findByText('38')).toBeTruthy();
    expect(screen.getByText('明天预计')).toBeTruthy();
    expect(screen.getByText('约 50 分钟')).toBeTruthy();
    await fireEvent.press(screen.getByText('今日总结'));
    expect(mockPush).toHaveBeenCalledWith('/plan/summary');
  });

  it('进入新阶段提示（2.1e）：显示构成对比，接受后提交', async () => {
    let answered = false;
    mockResponses['GET /home'] = () => ({
      status: 200,
      data: home({ stage_prompt: answered ? undefined : { to: 'sprint', reason: 'by_date', mix: { current: { new: 0.1, review: 0.25, weak: 0.45, recite: 0.2 }, next: { new: 0, review: 0.3, weak: 0.5, recite: 0.2 } } } }),
    });
    mockResponses['POST /home/stage-prompt'] = () => {
      answered = true;
      return { status: 200, data: home({ stage: 'sprint' }) };
    };
    await wrap(<TodayTab />);
    expect(await screen.findByText('离初试还有 83 天，进入冲刺期')).toBeTruthy();
    expect(screen.getByText('10 · 25 · 45 · 20')).toBeTruthy();
    expect(screen.getByText('0 · 30 · 50 · 20')).toBeTruthy();
    await fireEvent.press(screen.getByText('好的'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/home/stage-prompt')?.init).toEqual({ body: { accept: true } }));
    await waitFor(() => expect(screen.queryByText('离初试还有 83 天，进入冲刺期')).toBeNull());
  });

  it('修改目标分（2.1f）', async () => {
    mockResponses['GET /home'] = () => ({ status: 200, data: home() });
    mockResponses['PATCH /subjects/{subjectId}'] = () => ({ status: 200, data: subjects.items[0] });
    await wrap(<TodayTab />);
    await fireEvent.press(await screen.findByLabelText('修改语言文学基础目标分'));
    expect(screen.getByText('修改目标分')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('增加'));
    await fireEvent.press(screen.getByText('保存'));
    await waitFor(() =>
      expect(mockCalls.find((c) => c.method === 'PATCH')?.init).toEqual({ params: { path: { subjectId: 7 } }, body: { target_score: 120 } }),
    );
  });
});

describe('2.2 今日训练完成', () => {
  it('题数、正确率、用时与失分归因', async () => {
    await wrap(<TodaySummaryPage />);
    expect(await screen.findByText(/^78%$/)).toBeTruthy();
    expect(screen.getByText('46')).toBeTruthy();
    expect(screen.getByText('新增 3 个已掌握')).toBeTruthy();
    expect(screen.getByText(/知识没掌握 62%/)).toBeTruthy();
  });
});
