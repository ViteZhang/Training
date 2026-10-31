import { onlineManager } from '@tanstack/react-query';
import { ApiError } from '@training/api-client';
import { networkStateIsOnline, QUERY_RETRY_DELAYS, queryClient } from '../lib/queryClient';

// 这里只验证重试策略，原生网络模块用虚拟 mock 顶掉（未安装时也能跑）。
jest.mock(
  'expo-network',
  () => ({ addNetworkStateListener: () => ({ remove: () => undefined }), getNetworkStateAsync: async () => ({ isConnected: true, isInternetReachable: true }) }),
  { virtual: true },
);

it('waits for a confirmed offline state and treats unknown reachability as retryable', () => {
  expect(networkStateIsOnline({ isConnected: false, isInternetReachable: false })).toBe(false);
  expect(networkStateIsOnline({ isConnected: true, isInternetReachable: false })).toBe(false);
  expect(networkStateIsOnline({ isConnected: true, isInternetReachable: null })).toBe(true);
});

it('网络与 5xx 按退避多试几次，4xx 直接失败', () => {
  const queries = queryClient.getDefaultOptions().queries!;
  const retry = queries.retry as (count: number, err: Error) => boolean;
  const retryDelay = queries.retryDelay as (count: number) => number;
  const network = new ApiError(0, undefined);
  expect(retry(0, network)).toBe(true);
  expect(retry(QUERY_RETRY_DELAYS.length - 1, network)).toBe(true);
  expect(retry(QUERY_RETRY_DELAYS.length, network)).toBe(false);
  expect(retry(0, new ApiError(500, { code: 'INTERNAL' }))).toBe(true);
  expect(retry(0, new ApiError(404, { code: 'NOT_FOUND' }))).toBe(false);
  expect(retryDelay(0)).toBe(QUERY_RETRY_DELAYS[0]);
  expect(retryDelay(1)).toBe(QUERY_RETRY_DELAYS[1]);
});

it('离线时查询不挂起，直接失败且不空跑重试', () => {
  const queries = queryClient.getDefaultOptions().queries!;
  const retry = queries.retry as (count: number, err: Error) => boolean;
  // 挂起（paused）会让页面拿到 undefined 数据渲染崩掉，所以离线也要真去请求、尽快进错误态。
  expect(queries.networkMode).toBe('always');
  onlineManager.setOnline(false);
  try {
    expect(retry(0, new ApiError(0, undefined))).toBe(false);
    expect(retry(0, new ApiError(500, { code: 'INTERNAL' }))).toBe(false);
  } finally {
    onlineManager.setOnline(true);
  }
  expect(retry(0, new ApiError(0, undefined))).toBe(true);
});
