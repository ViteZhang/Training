import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import InvitePage from '../../app/mine/invite';

const mockPush = jest.fn();
const mockReplace = jest.fn();
const mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
const mockCopy = jest.fn(async () => true);
jest.mock('expo-clipboard', () => ({ setStringAsync: (...a: unknown[]) => mockCopy(...(a as [])) }));
jest.mock('react-native-view-shot', () => ({ captureRef: async () => 'file:///poster.png' }));
const mockShareFile = jest.fn(async () => {});
jest.mock('expo-sharing', () => ({ isAvailableAsync: async () => true, shareAsync: (...a: unknown[]) => mockShareFile(...(a as [])) }));

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

import { Share } from 'react-native';

const overview = (over: object = {}) => ({
  code: 'K7Q2HN3P', reward_days: 7, max_days: 70, earned_days: 14, invited: 3,
  records: [
    { registered_at: '2026-10-03T02:00:00Z', activated: false, days: 0 },
    { registered_at: '2026-10-02T02:00:00Z', activated: true, days: 7 },
    { registered_at: '2026-10-01T02:00:00Z', activated: true, days: 7 },
  ],
  ...over,
});

beforeEach(() => {
  mockCalls.length = 0;
  mockCopy.mockClear();
  mockShareFile.mockClear();
  mockResponses = { 'GET /me/invites': () => ({ status: 200, data: overview() }) };
});

describe('6.8 邀请研友', () => {
  it('邀请码、规则、记录与已获得天数；复制、分享、海报', async () => {
    const share = jest.spyOn(Share, 'share').mockResolvedValue({ action: 'sharedAction' });
    const r = await wrap(<InvitePage />);
    expect(await screen.findByLabelText('邀请码 K7Q2HN3P')).toBeTruthy();
    expect(screen.getByText('双方各得 7 天会员')).toBeTruthy();
    expect(screen.getByText('好友用你的邀请码注册，并导入第一份资料后生效')).toBeTruthy();
    expect(screen.getByText('已获得 14 天')).toBeTruthy();
    expect(screen.getByText('研友 C')).toBeTruthy();
    expect(screen.getByText(/已注册，还没导入资料/)).toBeTruthy();
    expect(screen.getAllByText('+7 天')).toHaveLength(2);
    await fireEvent.press(screen.getByText('复制'));
    expect(mockCopy).toHaveBeenCalledWith('K7Q2HN3P');
    await fireEvent.press(screen.getByText('分享给微信好友'));
    expect(share).toHaveBeenCalledWith({ message: expect.stringContaining('K7Q2HN3P') });
    await fireEvent.press(screen.getByText('生成海报'));
    await fireEvent.press(screen.getByText('分享海报'));
    await waitFor(() => expect(mockShareFile).toHaveBeenCalledWith('file:///poster.png', expect.objectContaining({ mimeType: 'image/png' })));
    share.mockRestore();
    await r.unmount();
  });

  it('达到上限后提示，好友仍得奖励', async () => {
    mockResponses['GET /me/invites'] = () => ({ status: 200, data: overview({ earned_days: 70, records: [{ registered_at: '2026-10-03T02:00:00Z', activated: true, days: 0 }] }) });
    const r = await wrap(<InvitePage />);
    expect(await screen.findByText(/已达上限，好友仍可获得奖励/)).toBeTruthy();
    expect(screen.getByText('已达上限')).toBeTruthy();
    await r.unmount();
  });

  it('没有记录时显示空状态', async () => {
    mockResponses['GET /me/invites'] = () => ({ status: 200, data: overview({ earned_days: 0, invited: 0, records: [] }) });
    const r = await wrap(<InvitePage />);
    expect(await screen.findByText('还没有好友用你的邀请码注册')).toBeTruthy();
    await r.unmount();
  });
});
