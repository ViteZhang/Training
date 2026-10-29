import { act, render, screen } from '@testing-library/react-native';
import Splash, { SPLASH_MS } from '../../app/index';
import { useSession } from '../lib/session';
import { storage } from '../lib/storage';

const mockReplace = jest.fn();
jest.mock('expo-router', () => ({ router: { replace: (...a: unknown[]) => mockReplace(...a), push: jest.fn() } }));
jest.mock('expo-crypto', () => ({ randomUUID: () => 'uuid-1234-5678' }));

const mockGet = jest.fn();
const mockRefresh = jest.fn();
jest.mock('../lib/api', () => {
  const actual = jest.requireActual('@training/api-client');
  return { api: { GET: (...a: unknown[]) => mockGet(...a) }, refreshSession: () => mockRefresh(), unwrap: actual.unwrap };
});

const session = { accessToken: 'a', accessExpiresAt: '2000-01-01T00:00:00Z', refreshToken: 'r', refreshExpiresAt: '2099-01-01T00:00:00Z' };
const ok = (data: unknown) => Promise.resolve({ data, response: new Response(null, { status: 200 }) });

async function launch() {
  jest.useFakeTimers();
  await render(<Splash />);
  await act(async () => {
    for (let i = 0; i < 20; i++) await Promise.resolve();
    jest.advanceTimersByTime(SPLASH_MS);
  });
  jest.useRealTimers();
}

beforeEach(() => {
  mockReplace.mockReset();
  mockGet.mockReset();
  mockRefresh.mockReset();
  useSession.getState().setSession(session);
  storage.remove('onboarding_step');
});

describe('0.1 启动页', () => {
  it('启动时服务端确认未登录、刷新因网络失败：保留会话并提供恢复入口', async () => {
    storage.set('onboarding_step', 'done');
    mockGet.mockReturnValue(ok({ server_time: '', app_name: '考研Training', flags: {}, logged_in: false }));
    mockRefresh.mockResolvedValue(undefined);
    await launch();
    expect(useSession.getState().session).toBeDefined();
    expect(mockReplace).not.toHaveBeenCalled();
    expect(screen.getByText('登录状态暂时无法验证')).toBeTruthy();
    expect(screen.getByText('重新登录')).toBeTruthy();
  });

  it('联网正常时记下引导进度', async () => {
    mockGet.mockReturnValue(ok({ server_time: '', app_name: '考研Training', flags: {}, logged_in: true, onboarding_step: 'done' }));
    await launch();
    expect(storage.getString('onboarding_step')).toBe('done');
    expect(mockReplace).toHaveBeenCalledWith('/(tabs)/today');
  });
});
