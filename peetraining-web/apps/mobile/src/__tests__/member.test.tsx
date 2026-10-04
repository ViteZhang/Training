import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import MemberCenter from '../../app/member';
import PayResult from '../../app/member/result';

const mockPush = jest.fn();
const mockReplace = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
jest.mock('expo-crypto', () => ({ randomUUID: () => 'uuid-1234-5678' }));
const mockLaunch = jest.fn();
jest.mock('../features/membership/pay', () => ({ launchPay: (...a: unknown[]) => mockLaunch(...a) }));

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

const rule = (limit: number | null, period: string) => ({ limit, period });
const center = (over: object = {}) => ({
  payment_enabled: false,
  channels: [],
  membership: { is_member: false },
  plans: [
    { tier: 'sprint', name: '冲刺卡', price_cents: 5900, recommended: false, available: true, ends_at: '2026-12-20T16:00:00Z', apple_product_id: 'p.sprint' },
    { tier: 'season', name: '考季卡', price_cents: 16900, recommended: true, available: true, ends_at: '2027-12-26T16:00:00Z', apple_product_id: 'p.season' },
    { tier: 'monthly', name: '月卡', price_cents: 2990, recommended: false, available: true, ends_at: '2026-11-02T02:00:00Z', apple_product_id: 'p.monthly' },
  ],
  benefits: [
    { quota_type: 'grading', name: '主观题批改', free: rule(3, 'daily'), member: rule(null, 'daily') },
    { quota_type: 'parse_pages', name: '资料解析页数', free: rule(100, 'total'), member: rule(1000, 'monthly') },
    { quota_type: 'essay_grading', name: '作文批改', free: rule(1, 'weekly'), member: rule(null, 'weekly') },
  ],
  ...over,
});
const order = (over: object = {}) => ({
  order_no: 'T20261003100000123456', tier: 'season', channel: 'apple_iap', amount_cents: 16900, status: 'created', created_at: '2026-10-03T02:00:00Z', ...over,
});

beforeEach(() => {
  mockCalls.length = 0;
  mockPush.mockReset();
  mockReplace.mockReset();
  mockLaunch.mockReset();
  mockParams = {};
  mockResponses = { 'GET /membership/plans': () => ({ status: 200, data: center() }) };
});

describe('6.5 会员中心', () => {
  it('支付关闭时只显示权益对比与兑换码入口，注明不自动续费', async () => {
    const r = await wrap(<MemberCenter />);
    expect(await screen.findByText('开通会员')).toBeTruthy();
    expect(screen.getByLabelText('主观题批改 免费版 3 次/天 会员 不限')).toBeTruthy();
    expect(screen.getByLabelText('资料解析页数 免费版 100 页 会员 1000 页/月')).toBeTruthy();
    expect(screen.queryByText(/立即开通/)).toBeNull();
    expect(screen.queryByText('冲刺卡')).toBeNull();
    expect(screen.getByText(/不自动续费/)).toBeTruthy();
    await fireEvent.press(screen.getByText('有兑换码？'));
    expect(mockPush).toHaveBeenCalledWith('/mine/redeem');
    await fireEvent.press(screen.getByText('会员服务协议'));
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/(auth)/agreement', params: { kind: 'membership' } });
    await r.unmount();
  });

  it('支付打开：默认选推荐档，iOS 只用 App 内购买；下单后调起支付，成功进入 6.6', async () => {
    mockResponses['GET /membership/plans'] = () => ({ status: 200, data: center({ payment_enabled: true, channels: ['wechat', 'alipay', 'apple_iap'] }) });
    mockResponses['POST /orders'] = () => ({ status: 200, data: order({ app_account_token: 'tok', apple_product_id: 'p.season' }) });
    mockLaunch.mockResolvedValue({ ok: true });
    const r = await wrap(<MemberCenter />);
    expect(await screen.findByText('立即开通 · ¥169')).toBeTruthy();
    expect(screen.getByText('有效期至 2026 年 11 月 2 日 · 灵活试用')).toBeTruthy();
    expect(screen.queryByText('微信支付')).toBeNull();
    await fireEvent.press(screen.getByLabelText('月卡 ¥29.9'));
    expect(screen.getByText('立即开通 · ¥29.9')).toBeTruthy();
    await fireEvent.press(screen.getByLabelText('考季卡 ¥169'));
    await fireEvent.press(screen.getByText('立即开通 · ¥169'));
    await waitFor(() => expect(mockPush).toHaveBeenCalledWith({ pathname: '/member/result', params: { orderNo: 'T20261003100000123456' } }));
    const body = (mockCalls.find((c) => c.path === '/orders')?.init as { body: { tier: string; channel: string; idempotency_key: string } }).body;
    expect(body.tier).toBe('season');
    expect(body.channel).toBe('apple_iap');
    expect(body.idempotency_key).toBeTruthy();
    expect(mockLaunch).toHaveBeenCalled();
    await r.unmount();
  });

  it('不可购买的档位显示原因；下单失败提示服务端原因', async () => {
    const base = center();
    const plans = [base.plans[0], { tier: 'season', name: '考季卡', price_cents: 16900, recommended: true, available: false, unavailable_reason: '初试日期公布后开放' }, base.plans[2]];
    const c = center({ payment_enabled: true, channels: ['apple_iap'], plans });
    mockResponses['GET /membership/plans'] = () => ({ status: 200, data: c });
    mockResponses['POST /orders'] = () => ({ status: 409, error: { code: 'CONFLICT', message: '下单失败，请稍后再试或换一种支付方式', detail: { reason: 'prepay_failed' } } });
    const r = await wrap(<MemberCenter />);
    expect(await screen.findByText('初试日期公布后开放')).toBeTruthy();
    // 推荐档不可买时默认选第一个可买的。
    await fireEvent.press(screen.getByText('立即开通 · ¥59'));
    expect(await screen.findByText('下单失败，请稍后再试或换一种支付方式')).toBeTruthy();
    expect(mockLaunch).not.toHaveBeenCalled();
    await r.unmount();
  });

  it('调起支付失败或取消时进入 6.6 显示原因', async () => {
    mockResponses['GET /membership/plans'] = () => ({ status: 200, data: center({ payment_enabled: true, channels: ['apple_iap'] }) });
    mockResponses['POST /orders'] = () => ({ status: 200, data: order() });
    mockLaunch.mockResolvedValue({ ok: false, message: '已取消支付' });
    const r = await wrap(<MemberCenter />);
    await fireEvent.press(await screen.findByText('立即开通 · ¥169'));
    await waitFor(() =>
      expect(mockPush).toHaveBeenCalledWith({ pathname: '/member/result', params: { orderNo: 'T20261003100000123456', failed: '已取消支付' } }),
    );
    await r.unmount();
  });
});

describe('6.6 支付结果', () => {
  it('成功：档位与有效期、权益说明、去训练', async () => {
    mockParams = { orderNo: 'T1' };
    mockResponses['GET /orders/{orderNo}'] = () => ({ status: 200, data: order({ status: 'paid', paid_at: '2026-10-03T02:00:05Z', membership_ends_at: '2027-12-26T16:00:00Z' }) });
    const r = await wrap(<PayResult />);
    expect(await screen.findByText('开通成功')).toBeTruthy();
    expect(screen.getByText(/考季卡已生效，有效期至 2027 年 12 月 2[67] 日/)).toBeTruthy();
    expect(screen.getByText('批改、出题、资料解析都不限次数')).toBeTruthy();
    await fireEvent.press(screen.getByText('去训练'));
    expect(mockReplace).toHaveBeenCalledWith('/(tabs)/train');
    await r.unmount();
  });

  it('回调还没到：显示确认中，可刷新', async () => {
    mockParams = { orderNo: 'T1' };
    let status = 'created';
    mockResponses['GET /orders/{orderNo}'] = () => ({ status: 200, data: order({ status }) });
    const r = await wrap(<PayResult />);
    expect(await screen.findByText('支付结果确认中')).toBeTruthy();
    status = 'paid';
    await fireEvent.press(screen.getByText('刷新'));
    expect(await screen.findByText('开通成功')).toBeTruthy();
    await r.unmount();
  });

  it('失败或取消：说明原因与重试', async () => {
    mockParams = { orderNo: 'T1', failed: '已取消支付' };
    const r = await wrap(<PayResult />);
    expect(await screen.findByText('支付未完成')).toBeTruthy();
    expect(screen.getByText('已取消支付')).toBeTruthy();
    expect(screen.getByText('重新支付')).toBeTruthy();
    expect(mockCalls.some((c) => c.path === '/orders/{orderNo}')).toBe(false);
    await r.unmount();
  });
});
