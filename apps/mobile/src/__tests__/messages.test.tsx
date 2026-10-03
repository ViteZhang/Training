import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import MessagesPage from '../../app/messages';
import { messageHref, messageTime } from '../features/messages/api';
import { syncReminders } from '../lib/reminders';

const mockPush = jest.fn();
const mockReplace = jest.fn();
const mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
const mockScheduled: { identifier: string }[] = [];
const mockSchedule = jest.fn(async (r: { identifier: string }) => {
  mockScheduled.push({ identifier: r.identifier });
  return r.identifier;
});
const mockCancel = jest.fn(async (id: string) => {
  const i = mockScheduled.findIndex((n) => n.identifier === id);
  if (i >= 0) mockScheduled.splice(i, 1);
});
let mockGranted = true;
jest.mock('expo-notifications', () => ({
  SchedulableTriggerInputTypes: { DAILY: 'daily' },
  getAllScheduledNotificationsAsync: async () => [...mockScheduled],
  cancelScheduledNotificationAsync: (id: string) => mockCancel(id),
  getPermissionsAsync: async () => ({ granted: mockGranted }),
  scheduleNotificationAsync: (r: { identifier: string }) => mockSchedule(r),
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
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
  return render(
    <SafeAreaProvider initialMetrics={metrics}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </SafeAreaProvider>,
  );
}

const now = new Date();
const today = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 8, 0).toISOString();
const msg = (over: object) => ({ id: 1, type: 'import_done', title: '题库整理完成', body: '识别出 246 道题', read: false, created_at: today, ...over });

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockResponses = {
    'GET /messages': () => ({
      status: 200,
      data: {
        unread: 2,
        items: [
          msg({ id: 3, page: 'import_review', params: { job_id: 7 } }),
          msg({ id: 2, type: 'content_accessed', title: '客服查看了你授权的资料', body: '根据你的授权……', read: true }),
          msg({ id: 1, type: 'agreement_update', title: '隐私政策更新', body: '新增导出说明', created_at: '2026-09-20T02:00:00Z', page: 'agreement', params: { kind: 'privacy' } }),
        ],
      },
    }),
    'POST /messages/{messageId}/read': () => ({ status: 204 }),
    'POST /messages/read-all': () => ({ status: 204 }),
  };
});

describe('2.3 消息中心', () => {
  it('按今天 / 更早分组；点击跳转并标已读；全部已读；注明保留 30 天', async () => {
    const r = await wrap(<MessagesPage />);
    expect(await screen.findByText('今天')).toBeTruthy();
    expect(screen.getByText('更早')).toBeTruthy();
    expect(screen.getByText('9 月 20 日')).toBeTruthy();
    expect(screen.getByText('消息保留 30 天')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('未读 题库整理完成'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/import/confirm/[id]', params: { id: '7' } });
    expect(mockCalls.some((c) => c.path === '/messages/{messageId}/read' && JSON.stringify(c.init).includes('"messageId":3'))).toBe(true);
    // 已读的不再标已读；没有跳转目标的只展示。
    mockCalls.length = 0;
    await fireEvent.press(screen.getByLabelText('客服查看了你授权的资料'));
    expect(mockCalls.some((c) => c.path === '/messages/{messageId}/read')).toBe(false);
    await fireEvent.press(screen.getByText('全部已读'));
    await waitFor(() => expect(mockCalls.some((c) => c.path === '/messages/read-all')).toBe(true));
    await r.unmount();
  });

  it('没有消息时显示空状态', async () => {
    mockResponses['GET /messages'] = () => ({ status: 200, data: { unread: 0, items: [] } });
    const r = await wrap(<MessagesPage />);
    expect(await screen.findByText('还没有消息')).toBeTruthy();
    expect(screen.queryByText('全部已读')).toBeNull();
    await r.unmount();
  });

  it('跳转目标与时间格式', () => {
    expect(messageHref(msg({ page: 'paper_report', params: { session_id: 5 } }) as never)).toEqual({ pathname: '/paper/report/[id]', params: { id: '5' } });
    expect(messageHref(msg({ page: 'nope' }) as never)).toBeUndefined();
    const ref = new Date(2026, 9, 3, 12, 0);
    expect(messageTime(new Date(2026, 9, 3, 8, 5).toISOString(), ref)).toBe('08:05');
    expect(messageTime(new Date(2026, 9, 2, 8, 5).toISOString(), ref)).toBe('昨天');
    expect(messageTime(new Date(2026, 8, 26, 8, 5).toISOString(), ref)).toBe('9 月 26 日');
  });
});

describe('本地学习提醒', () => {
  beforeEach(() => {
    mockScheduled.length = 0;
    mockScheduled.push({ identifier: 'study-reminder:07:30' }, { identifier: 'other' });
    mockSchedule.mockClear();
    mockGranted = true;
  });

  it('按设置的时间每天提醒；重排时只取消自己的', async () => {
    expect(await syncReminders({ reminder_times: ['20:00', '12:30'], notify_daily: true })).toBe(2);
    expect(mockScheduled.map((n) => n.identifier).sort()).toEqual(['other', 'study-reminder:12:30', 'study-reminder:20:00']);
    expect(mockSchedule).toHaveBeenCalledWith(expect.objectContaining({ trigger: { type: 'daily', hour: 20, minute: 0 } }));
  });

  it('关闭每日训练提醒或没有通知权限时不提醒', async () => {
    expect(await syncReminders({ reminder_times: ['20:00'], notify_daily: false })).toBe(0);
    expect(mockScheduled.map((n) => n.identifier)).toEqual(['other']);
    mockGranted = false;
    expect(await syncReminders({ reminder_times: ['20:00'], notify_daily: true })).toBe(0);
  });
});
