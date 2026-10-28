import { networkStateIsOnline } from '../lib/queryClient';

it('waits for a confirmed offline state and treats unknown reachability as retryable', () => {
  expect(networkStateIsOnline({ isConnected: false, isInternetReachable: false })).toBe(false);
  expect(networkStateIsOnline({ isConnected: true, isInternetReachable: false })).toBe(false);
  expect(networkStateIsOnline({ isConnected: true, isInternetReachable: null })).toBe(true);
});
