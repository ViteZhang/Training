import { focusManager, onlineManager, QueryClient } from '@tanstack/react-query';
import { ApiError } from '@training/api-client';
import * as Network from 'expo-network';
import { AppState } from 'react-native';

type NetworkSnapshot = { isConnected?: boolean | null; isInternetReachable?: boolean | null };

export function networkStateIsOnline(state: NetworkSnapshot): boolean {
  return state.isConnected !== false && state.isInternetReachable !== false;
}

const isTest = typeof process !== 'undefined' && !!process.env?.JEST_WORKER_ID;

if (!isTest) {
  onlineManager.setEventListener((setOnline) => {
    let eventVersion = 0;
    const update = (state: NetworkSnapshot) => {
      eventVersion += 1;
      setOnline(networkStateIsOnline(state));
    };
    const subscription = Network.addNetworkStateListener(update);
    const initialVersion = eventVersion;
    void Network.getNetworkStateAsync().then((state) => {
      if (eventVersion === initialVersion) setOnline(networkStateIsOnline(state));
    }).catch(() => undefined);
    return () => subscription.remove();
  });

  focusManager.setEventListener((setFocused) => {
    const subscription = AppState.addEventListener('change', (state) => setFocused(state === 'active'));
    return () => subscription.remove();
  });
}

/** 网络/5xx 的重试间隔：覆盖手机熄屏唤醒后 2～5 秒才真正可用的窗口。 */
export const QUERY_RETRY_DELAYS = [1_000, 3_000, 8_000];

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 离线时不让查询停在 paused：paused 下 isLoading 是 false、data 还是 undefined，
      // 页面按 isLoading/isError 分流后会把 undefined 当数据渲染（题库页就是这么白屏的）。
      // 离线直接去请求、尽快失败进错误态，由 ErrorState 负责重试与网络恢复后的自动恢复。
      networkMode: 'always',
      // 4xx 不重试（404 是越权或开关关闭，402 是额度不足）；离线时也不空跑重试，失败即出错。
      retry: (count, err) =>
        onlineManager.isOnline() &&
        !(err instanceof ApiError && err.status >= 400 && err.status < 500) &&
        count < QUERY_RETRY_DELAYS.length,
      retryDelay: (count) => QUERY_RETRY_DELAYS[count] ?? QUERY_RETRY_DELAYS[QUERY_RETRY_DELAYS.length - 1]!,
      staleTime: 30_000,
      refetchOnReconnect: true,
    },
  },
});
