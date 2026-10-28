import { ApiError } from '@training/api-client';
import { router } from 'expo-router';
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { Button, EmptyState, ErrorState, Loading, ProgressBar, Tag, Text, toast, ToastHost } from '../components';
import { logout } from '../features/auth/actions';
import { useSession } from '../lib/session';
import { getJSON, setJSON } from '../lib/storage';

jest.mock('../features/auth/actions', () => ({ logout: jest.fn() }));
// 顶部栏的返回按钮默认用 expo-router 返回上一页
jest.mock('expo-router', () => ({ router: { back: jest.fn(), push: jest.fn(), replace: jest.fn() } }));

const metrics = { frame: { x: 0, y: 0, width: 390, height: 844 }, insets: { top: 0, left: 0, right: 0, bottom: 0 } };
const wrap = (ui: React.ReactElement) => render(<SafeAreaProvider initialMetrics={metrics}>{ui}</SafeAreaProvider>);

describe('基础组件', () => {
  it('按钮可点；加载中不可点', async () => {
    const onPress = jest.fn();
    await wrap(
      <>
        <Button title="开始今日训练" onPress={onPress} />
        <Button title="提交中" loading onPress={onPress} />
      </>,
    );
    await fireEvent.press(screen.getByText('开始今日训练'));
    await fireEvent.press(screen.getByText('提交中'));
    expect(onPress).toHaveBeenCalledTimes(1);
  });

  it('文字最大放大 1.3 倍', async () => {
    await wrap(<Text>正文</Text>);
    expect(screen.getByText('正文').props.maxFontSizeMultiplier).toBe(1.3);
  });

  it('进度条报告无障碍进度', async () => {
    await wrap(<ProgressBar value={0.42} target={0.85} />);
    expect(screen.getByRole('progressbar').props.accessibilityValue.now).toBe(42);
  });

  it('标签', async () => {
    await wrap(<Tag label="AI 生成" tone="ai" />);
    expect(screen.getByText('AI 生成')).toBeTruthy();
  });
});

describe('五种状态', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    useSession.getState().clear();
    jest.mocked(logout).mockReset().mockResolvedValue(undefined);
    jest.mocked(router.replace).mockReset();
  });
  afterEach(() => jest.useRealTimers());

  it('加载超过 300 毫秒才显示骨架屏', async () => {
    await wrap(<Loading />);
    expect(screen.queryByLabelText('加载中')).toBeNull();
    await act(() => jest.advanceTimersByTime(301));
    expect(screen.getByLabelText('加载中')).toBeTruthy();
  });

  it('空状态有说明与行动按钮', async () => {
    const onAction = jest.fn();
    await wrap(<EmptyState title="还没有导入资料" desc="先导入一份真题" actionText="导入第一份资料" onAction={onAction} />);
    await fireEvent.press(screen.getByText('导入第一份资料'));
    expect(onAction).toHaveBeenCalled();
  });

  it('网络错误与业务错误文案不同', async () => {
    const { unmount } = await wrap(<ErrorState error={new ApiError(0, undefined)} onRetry={() => {}} />);
    expect(screen.getByText('网络开小差了')).toBeTruthy();
    expect(screen.queryByText('重新登录')).toBeNull();
    await unmount();
    await wrap(<ErrorState error={new ApiError(500, { code: 'INTERNAL', message: '服务暂时出了点问题' })} />);
    expect(screen.getByText('服务暂时出了点问题')).toBeTruthy();
  });

  it('网络故障时允许保留中的会话由用户主动重新登录', async () => {
    useSession.getState().setSession({
      accessToken: 'access',
      accessExpiresAt: '2099-01-01T00:00:00Z',
      refreshToken: 'refresh',
      refreshExpiresAt: '2099-01-02T00:00:00Z',
    });
    await wrap(<ErrorState error={new ApiError(503, { code: 'NETWORK', message: '网络还没连上，请稍后再试' })} onRetry={() => {}} />);

    await fireEvent.press(screen.getByText('重新登录'));

    expect(logout).toHaveBeenCalledTimes(1);
    expect(router.replace).toHaveBeenCalledWith('/(auth)/login');
  });

  it('Toast 2 秒后消失', async () => {
    await wrap(<ToastHost />);
    await act(() => toast('已保存'));
    expect(screen.getByText('已保存')).toBeTruthy();
    await act(() => jest.advanceTimersByTime(2001));
    expect(screen.queryByText('已保存')).toBeNull();
  });
});

describe('本地存储', () => {
  it('JSON 读写', async () => {
    setJSON('k', { a: 1 });
    expect(getJSON<{ a: number }>('k')).toEqual({ a: 1 });
    expect(getJSON('missing')).toBeUndefined();
  });
});

describe('Text 数字字体', () => {
  const styleOf = (el: ReturnType<typeof screen.getByText>) => el.props.style as { fontFamily?: string; fontWeight?: string };

  it('num 按字重取对应的 DM Sans 文件，字重置为 normal（安卓自定义字体不认 fontWeight）', async () => {
    await render(
      <>
        <Text num>12</Text>
        <Text num={600}>34</Text>
        <Text variant="bodyStrong" num>56</Text>
        <Text style={{ fontWeight: '700' }} num>78</Text>
      </>,
    );
    expect(styleOf(screen.getByText('12'))).toMatchObject({ fontFamily: 'DMSans-Regular', fontWeight: 'normal' });
    expect(styleOf(screen.getByText('34'))).toMatchObject({ fontFamily: 'DMSans-SemiBold', fontWeight: 'normal' });
    expect(styleOf(screen.getByText('56'))).toMatchObject({ fontFamily: 'DMSans-Medium', fontWeight: 'normal' });
    expect(styleOf(screen.getByText('78'))).toMatchObject({ fontFamily: 'DMSans-Bold', fontWeight: 'normal' });
  });

  it('分数样式默认用 DM Sans SemiBold；中文正文不指定字体', async () => {
    await render(
      <>
        <Text variant="score">98</Text>
        <Text>正文</Text>
      </>,
    );
    expect(styleOf(screen.getByText('98')).fontFamily).toBe('DMSans-SemiBold');
    expect(styleOf(screen.getByText('正文')).fontFamily).toBeUndefined();
  });
});
