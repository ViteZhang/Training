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

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 4xx 不重试（404 是越权或开关关闭，402 是额度不足），网络错误与 5xx 重试 2 次。
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
      staleTime: 30_000,
      refetchOnReconnect: true,
    },
  },
});
