import { ApiError } from '@training/api-client';
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import Code from '../../app/(auth)/code';
import Login from '../../app/(auth)/login';
import { formatPhone, isValidPhone, normalizePhone } from '../features/auth/phone';
import { routeFor } from '../features/auth/routing';
import { useSession } from '../lib/session';

const mockPush = jest.fn();
const mockReplace = jest.fn();
let mockParams: Record<string, string> = {};
jest.mock('expo-router', () => ({
  router: { push: (...a: unknown[]) => mockPush(...a), replace: (...a: unknown[]) => mockReplace(...a), back: jest.fn() },
  useLocalSearchParams: () => mockParams,
}));
jest.mock('expo-device', () => ({ modelName: 'iPhone 15', deviceName: null }));
jest.mock('expo-crypto', () => ({ randomUUID: () => 'uuid-1234-5678' }));

const mockPost = jest.fn();
jest.mock('../lib/api', () => {
  const actual = jest.requireActual('@training/api-client');
  return { api: { POST: (...a: unknown[]) => mockPost(...a) }, unwrap: actual.unwrap };
});

const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
const wrap = (ui: React.ReactElement) => render(<SafeAreaProvider initialMetrics={metrics}>{ui}</SafeAreaProvider>);

function ok(data: unknown) {
  return Promise.resolve({ data, response: new Response(null, { status: 200 }) });
}
function fail(status: number, error: unknown) {
  return Promise.resolve({ error, response: new Response(null, { status }) });
}

beforeEach(() => {
  mockPush.mockReset();
  mockReplace.mockReset();
  mockPost.mockReset();
  mockParams = {};
  useSession.getState().clear();
});

describe('手机号', () => {
  it('3-4-4 分隔，只留数字，最多 11 位', () => {
    expect(formatPhone('13812345678')).toBe('138 1234 5678');
    expect(formatPhone('1381')).toBe('138 1');
    expect(normalizePhone('138-1234 5678 99')).toBe('13812345678');
  });
  it('13–19 开头的 11 位才合法', () => {
    expect(isValidPhone('13812345678')).toBe(true);
    expect(isValidPhone('12812345678')).toBe(false);
    expect(isValidPhone('1381234567')).toBe(false);
  });
});

describe('分流', () => {
  it('未登录、引导中、已完成', () => {
    expect(routeFor({ loggedIn: false })).toBe('/(auth)/login');
    expect(routeFor({ loggedIn: true, onboardingStep: '1.3' })).toBe('/(onboarding)/setup');
    expect(routeFor({ loggedIn: true, onboardingStep: 'done' })).toBe('/(tabs)/today');
  });
});

describe('0.2 登录', () => {
  it('号码不完整时不能获取验证码；格式不对时提示', async () => {
    await wrap(<Login />);
    const input = screen.getByLabelText('手机号');
    await fireEvent.changeText(input, '1281234567');
    expect(screen.getByText('获取验证码')).toBeTruthy();
    await fireEvent.press(screen.getByText('获取验证码'));
    expect(mockPost).not.toHaveBeenCalled();
    await fireEvent.changeText(input, '12812345678');
    expect(screen.getByText('请输入正确的手机号')).toBeTruthy();
  });

  it('未勾选协议先弹 0.2b，同意后发码并进入 0.3', async () => {
    mockPost.mockReturnValue(ok({ resend_after_seconds: 60, remaining_today: 9 }));
    await wrap(<Login />);
    await fireEvent.changeText(screen.getByLabelText('手机号'), '13812345678');
    await fireEvent.press(screen.getByText('获取验证码'));
    expect(screen.getByText('请阅读并同意以下条款')).toBeTruthy();
    expect(mockPost).not.toHaveBeenCalled();
    await fireEvent.press(screen.getByText('同意并获取验证码'));
    expect(mockPost).toHaveBeenCalledWith('/auth/sms-codes', { body: { phone: '13812345678', purpose: 'login', agree: true } });
    expect(mockPush).toHaveBeenCalledWith({ pathname: '/(auth)/code', params: { phone: '13812345678' } });
  });
});

describe('0.3 输入验证码', () => {
  it('输满 6 位自动登录，新用户进入 1.1', async () => {
    mockParams = { phone: '13812345678' };
    mockPost.mockReturnValue(
      ok({
        access_token: 'a', access_expires_at: '2026-10-01T00:00:00Z', refresh_token: 'r', refresh_expires_at: '2026-12-01T00:00:00Z',
        is_new_user: true, user: { onboarding_step: '1.1' },
      }),
    );
    await wrap(<Code />);
    await fireEvent.changeText(screen.getByLabelText('验证码'), '123456');
    expect(mockPost).toHaveBeenCalledWith('/auth/login', expect.objectContaining({ body: expect.objectContaining({ phone: '13812345678', code: '123456' }) }));
    expect(useSession.getState().session?.accessToken).toBe('a');
    expect(mockReplace).toHaveBeenCalledWith('/(onboarding)/subject');
  });

  it('验证码错误：清空并提示剩余次数（0.3b）', async () => {
    mockParams = { phone: '13812345678' };
    mockPost.mockReturnValue(fail(400, { code: 'BAD_REQUEST', message: '验证码错误，还可以输入 4 次', detail: { remaining_attempts: 4 } }));
    await wrap(<Code />);
    await fireEvent.changeText(screen.getByLabelText('验证码'), '000000');
    expect(screen.getByText('验证码错误，还可以输入 4 次')).toBeTruthy();
    expect(screen.getByLabelText('验证码').props.value).toBe('');
  });

  it('收不到验证码弹出排查建议（0.3c）', async () => {
    mockParams = { phone: '13812345678' };
    await wrap(<Code />);
    await fireEvent.press(screen.getByText('收不到验证码？'));
    expect(screen.getByText(/每天最多获取 10 次/)).toBeTruthy();
  });

  it('倒计时结束后可以重新获取', async () => {
    jest.useFakeTimers();
    mockParams = { phone: '13812345678', cooldown: '2' };
    await wrap(<Code />);
    expect(screen.queryByText('重新获取验证码')).toBeNull();
    await act(() => jest.advanceTimersByTime(1000));
    await act(() => jest.advanceTimersByTime(1000));
    expect(screen.getByText('重新获取验证码')).toBeTruthy();
    jest.useRealTimers();
  });
});

it('ApiError 类型可用', () => {
  expect(new ApiError(429, { code: 'TOO_MANY_REQUESTS', message: 'x' }).code).toBe('TOO_MANY_REQUESTS');
});
