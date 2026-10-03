import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import MeTab from '../../app/(tabs)/me';
import ExportPage from '../../app/mine/export';
import FeedbackPage from '../../app/mine/feedback';
import RedeemPage from '../../app/mine/redeem';
import SurveyPage from '../../app/mine/survey';
import SettingsPage from '../../app/settings';

const mockPush = jest.fn();
const mockReplace = jest.fn();
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => ({}),
}));
const mockDownload = jest.fn(async (_url: string, f: { uri: string }) => f);
const mockDeleteCache = jest.fn();
jest.mock('expo-file-system', () => {
  class File {
    uri: string;
    exists = false;
    size = 2048;
    constructor(...parts: unknown[]) {
      this.uri = parts.map(String).join('/');
    }
    delete() {}
    upload = async () => ({ status: 200 });
    static downloadFileAsync = (url: string, f: { uri: string }) => mockDownload(url, f);
  }
  class Directory {
    size = 5 * 1024 * 1024;
    list() {
      return [{ delete: mockDeleteCache }];
    }
  }
  return { File, Directory, Paths: { cache: 'cache://' } };
});
const mockShare = jest.fn(async () => {});
jest.mock('expo-sharing', () => ({ isAvailableAsync: async () => true, shareAsync: (...a: unknown[]) => mockShare(...(a as [])) }));
jest.mock('expo-image-picker', () => ({
  launchImageLibraryAsync: async () => ({ canceled: false, assets: [{ uri: 'file:///s1.png', mimeType: 'image/png', fileSize: 1000 }] }),
}));
const mockLogout = jest.fn(async () => {});
jest.mock('../features/auth/actions', () => ({ logout: () => mockLogout() }));

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

const me = (over: object = {}) => ({ id: 1, phone_masked: '138****0000', nickname: '小林同学', invite_code: 'ABC', onboarding_step: 'done', membership: { is_member: false }, ...over });
const profile = {
  exam_year: 2027, stage: 'strengthen', stage_manual: false, suggested_stage: 'strengthen', days_to_exam: 83, subject_exam_date: '2026-12-20', daily_minutes: 45,
  reminder_times: ['20:00'], essay_weekly_goal: 2, mock_time_reminders: true, notify_daily: true, notify_review_due: true, notify_task_done: false,
};
const subjects = { items: [{ id: 7, name: '语言文学基础', code: '654', full_score: 150, target_score: 115, is_essay: false, bank_id: 1, question_count: 246, kp_count: 58, material_count: 3 }], max_subjects: 3, can_add: true };
const survey = (over: object = {}) => ({
  open: true, exam_year: 2027, submitted: false, reward_days: 30, share_consent: false,
  subjects: [{ subject_id: 7, name: '语言文学基础', full_score: 150, low: 98, high: 106 }, { subject_id: 8, name: '作文', full_score: 150 }],
  ...over,
});

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockReplace.mockReset();
  mockShare.mockClear();
  mockDownload.mockClear();
  mockLogout.mockClear();
  mockResponses = {
    'GET /me': () => ({ status: 200, data: me() }),
    'GET /profile': () => ({ status: 200, data: profile }),
    'GET /subjects': () => ({ status: 200, data: subjects }),
    'GET /me/overview': () => ({ status: 200, data: { materials: 3, questions: 246, wrong: 23, essays: 0 } }),
    'GET /me/survey': () => ({ status: 200, data: survey({ open: false }) }),
    'GET /subjects/{subjectId}/dashboard': () => ({
      status: 200,
      data: { estimate: { subject_id: 7, name: '语言文学基础', is_essay: false, full_score: 150, target_score: 115, ready: true, low: 98, high: 106 }, trend: [],
        loss_points: { knowledge: 2, norm: 1, time: 1 }, loss_shares: { knowledge: 0.5, norm: 0.25, time: 0.25 }, sections_ready: false, sections: [], false_mastery: [], recent_papers: [], essay_dims: [] },
    }),
  };
});

describe('6.1 我的', () => {
  it('昵称、阶段与倒计时、会员条、提分看板卡片、各入口数字', async () => {
    mockResponses['GET /me/survey'] = () => ({ status: 200, data: survey() });
    const r = await wrap(<MeTab />);
    expect(await screen.findByText('小林同学')).toBeTruthy();
    expect(screen.getByText('654 · 强化期 · 83 天后初试')).toBeTruthy();
    expect(screen.getByText('开通会员：批改、整卷、资料解析不限')).toBeTruthy();
    expect(await screen.findByText('失分：知识没掌握 50% · 答题不规范 25% · 时间不够 25%')).toBeTruthy();
    expect(screen.getByText('3 份 · 246 题')).toBeTruthy();
    expect(screen.getByText('还没有作文')).toBeTruthy();
    expect(screen.getByText('初试辛苦了！填写考后回访送 30 天会员')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('兑换码'));
    expect(mockPush).toHaveBeenCalledWith('/mine/redeem');
    await r.unmount();
  });

  it('会员显示档位与有效期', async () => {
    mockResponses['GET /me'] = () => ({ status: 200, data: me({ membership: { is_member: true, tier: 'season', ends_at: '2027-12-26T16:00:00Z' } }) });
    const r = await wrap(<MeTab />);
    expect(await screen.findByText('考季卡')).toBeTruthy();
    expect(screen.getByText(/有效期至 2027 年 12 月/)).toBeTruthy();
    expect(screen.queryByText('开通')).toBeNull();
    await r.unmount();
  });
});

describe('6.7 兑换码', () => {
  it('失败提示服务端原因；成功显示叠加后的有效期', async () => {
    let ok = false;
    mockResponses['POST /me/redeem'] = () =>
      ok
        ? { status: 200, data: { tier: 'monthly', days: 30, starts_at: '2026-10-03T00:00:00Z', ends_at: '2026-11-02T00:00:00Z', membership: { is_member: true, tier: 'monthly', ends_at: '2026-11-02T00:00:00Z' } } }
        : { status: 400, error: { code: 'BAD_REQUEST', message: '这个兑换码已经被使用过了', detail: { reason: 'used' } } };
    const r = await wrap(<RedeemPage />);
    await fireEvent.changeText(screen.getByLabelText('兑换码'), 'abcd2345');
    expect(screen.getByDisplayValue('ABCD2345')).toBeTruthy();
    await fireEvent.press(screen.getByText('兑换'));
    expect(await screen.findByText('这个兑换码已经被使用过了')).toBeTruthy();
    ok = true;
    await fireEvent.press(screen.getByText('兑换'));
    expect(await screen.findByText('兑换成功')).toBeTruthy();
    expect(screen.getByText('月卡 · 30 天，已叠加到当前会员之后')).toBeTruthy();
    await r.unmount();
  });
});

describe('6.4 导出题库', () => {
  it('勾选内容与格式、预计页数；生成后保存到手机或分享', async () => {
    mockResponses['GET /subjects/{subjectId}/export-preview'] = () => ({
      status: 200,
      data: { subject_id: 7, questions: { count: 246, pages: 60 }, kps: { count: 58, pages: 20 }, wrong: { count: 23, pages: 12 }, ai_variants: { count: 10, pages: 4 } },
    });
    mockResponses['POST /exports'] = () => ({ status: 202, data: { id: 5, subject_id: 7, format: 'docx', options: {}, status: 'queued', pages: 80, file_name: '语言文学基础题库_20261003.docx', created_at: '2026-10-03T00:00:00Z' } });
    mockResponses['GET /exports/{exportId}'] = () => ({
      status: 200,
      data: { id: 5, subject_id: 7, format: 'docx', options: {}, status: 'done', pages: 80, file_name: '语言文学基础题库_20261003.docx', download_url: 'https://oss/x', created_at: '2026-10-03T00:00:00Z' },
    });
    const r = await wrap(<ExportPage />);
    expect(await screen.findByText('246 题，按题型排，附采分点')).toBeTruthy();
    expect(screen.getByText('约 92 页 · 生成后保存到手机或发到微信')).toBeTruthy();
    await fireEvent.press(screen.getByText('错题和我的作答'));
    expect(screen.getByText('约 80 页 · 生成后保存到手机或发到微信')).toBeTruthy();
    await fireEvent.press(screen.getByText('Word'));
    await fireEvent.press(screen.getByText('生成文档'));
    await waitFor(() => expect(mockCalls.find((c) => c.path === '/exports')?.init).toMatchObject({ body: { subject_id: 7, format: 'docx', options: { questions: true, kps: true, wrong: false, ai_variants: false } } }));
    await fireEvent.press(await screen.findByText('保存到手机或分享'));
    await waitFor(() => expect(mockShare).toHaveBeenCalled());
    expect(mockDownload.mock.calls[0]![0]).toBe('https://oss/x');
    await r.unmount();
  });
});

describe('6.13 意见反馈', () => {
  it('选类型、写描述、加截图、授权查看后提交；可看历史', async () => {
    mockResponses['POST /handwriting/upload-requests'] = () => ({ status: 200, data: { items: [{ object_key: 'u/1/hw/s1.png', upload_url: 'https://oss/u', upload_headers: {}, expires_at: '2026-10-03T00:00:00Z' }] } });
    mockResponses['POST /feedbacks'] = () => ({ status: 201, data: { id: 1, ftype: 'recognition', content: 'x', screenshot_keys: [], allow_access: true, status: 'open', created_at: '2026-10-03T00:00:00Z' } });
    mockResponses['GET /feedbacks'] = () => ({ status: 200, data: { items: [{ id: 1, ftype: 'recognition', content: '第 3 页识别错了', screenshot_keys: [], allow_access: true, status: 'replied', reply: '已修正', created_at: '2026-10-03T00:00:00Z' }] } });
    const r = await wrap(<FeedbackPage />);
    await fireEvent.press(screen.getByText('识别不准'));
    await fireEvent.changeText(screen.getByLabelText('详细描述'), '第 3 页识别错了');
    await fireEvent.press(screen.getByText('+ 添加'));
    expect(await screen.findByLabelText('删除第 1 张截图')).toBeTruthy();
    await fireEvent.press(screen.getByText(/允许客服查看相关资料/));
    await fireEvent.press(screen.getByText('提交'));
    await waitFor(() =>
      expect(mockCalls.find((c) => c.path === '/feedbacks' && c.method === 'POST')?.init).toMatchObject({ body: { ftype: 'recognition', content: '第 3 页识别错了', screenshot_keys: ['u/1/hw/s1.png'], allow_access: true } }),
    );
    expect(await screen.findByText('回复：已修正')).toBeTruthy();
    await r.unmount();
  });
});

describe('6.14 考后回访', () => {
  it('初试前未开放', async () => {
    const r = await wrap(<SurveyPage />);
    expect(await screen.findByText('初试后开放')).toBeTruthy();
    await r.unmount();
  });

  it('考前预估、没有预估分的课；填成绩与复试结果后提交', async () => {
    mockResponses['GET /me/survey'] = () => ({ status: 200, data: survey() });
    mockResponses['POST /me/survey'] = () => ({ status: 200, data: survey({ submitted: true }) });
    const r = await wrap(<SurveyPage />);
    expect(await screen.findByText('考前预估 98–106')).toBeTruthy();
    expect(screen.getByText('考前没有预估分')).toBeTruthy();
    await fireEvent.changeText(screen.getByLabelText('语言文学基础实际成绩'), '108');
    await fireEvent.press(screen.getByText('进复试'));
    await fireEvent.press(screen.getByText('提交'));
    await waitFor(() => expect(mockCalls.find((c) => c.method === 'POST')?.init).toMatchObject({ body: { scores: [{ subject_id: 7, score: 108 }], retest_result: 'in' } }));
    await r.unmount();
  });
});

describe('6.10 设置', () => {
  it('通知开关与提醒时间、缓存大小与清除、退出登录', async () => {
    mockResponses['PUT /profile'] = () => ({ status: 200, data: { ...profile, notify_task_done: true } });
    const r = await wrap(<SettingsPage />);
    expect(await screen.findByText('138****0000')).toBeTruthy();
    expect(screen.getByText('每天 20:00')).toBeTruthy();
    await fireEvent(screen.getByLabelText('资料解析和批改完成提醒开关'), 'valueChange', true);
    await waitFor(() => expect(mockCalls.find((c) => c.method === 'PUT')?.init).toMatchObject({ body: { notify_task_done: true, exam_year: 2027 } }));
    expect(screen.getByText('5 MB')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('清除缓存'));
    expect(mockDeleteCache).toHaveBeenCalled();
    await fireEvent.press(screen.getByText('退出登录'));
    await fireEvent.press(screen.getByText('退出'));
    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith('/(auth)/login'));
    expect(mockLogout).toHaveBeenCalled();
    await r.unmount();
  });
});
