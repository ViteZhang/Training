// 购买入口（PRD v3 3.4、13.3，10 月 26 日更新）：后台 7.8 按平台开关，iOS 提审前关闭。
// 关闭时 App 不显示会员中心入口、获取兑换码、兑换码页和价格，额度用完时只给免费出路（4.9b）。
// 在线支付开通前，购买走 6.5a 获取兑换码（客服企业微信）→ 6.7 兑换；开通后走 6.5「立即开通」。
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { Platform } from 'react-native';
import { api, unwrap } from '@/lib/api';
import { useFeatureFlag } from '@/lib/flags';

const platform = Platform.OS === 'ios' ? 'ios' : 'android';

export const purchaseKeys = { entry: ['purchase-entry', platform] as const };

/** 后台改开关立即生效、不用发版：缓存 1 分钟，回到前台时重新取。 */
export function usePurchaseEntry() {
  return useQuery({
    queryKey: purchaseKeys.entry,
    queryFn: () => unwrap(api.GET('/purchase-entry', { params: { query: { platform } } })),
    staleTime: 60_000,
  });
}

/** 这个平台是否显示购买与兑换码入口；还没取到时按关闭处理，避免 iOS 提审版闪出价格。 */
export function usePurchaseEnabled(): boolean {
  return usePurchaseEntry().data?.enabled ?? false;
}

export interface UpgradeActions {
  primary: { label: string; onPress: () => void };
  secondary?: { label: string; onPress: () => void };
}

/**
 * 额度用完弹层（4.9 及其他额度处）的购买按钮：
 * - 入口关闭（4.9b）：返回 undefined，弹层只给免费出路
 * - 在线支付开通前：「获取兑换码」（→ 6.5a）+「我有兑换码」（→ 6.7）
 * - 在线支付开通后：「开通会员」（→ 6.5）
 * close 是先关掉当前弹层（两个弹层不叠在一起）。
 */
export function useUpgradeActions(close: () => void): UpgradeActions | undefined {
  const enabled = usePurchaseEnabled();
  const online = useFeatureFlag('online_payment');
  if (!enabled) return undefined;
  if (online) return { primary: { label: '开通会员', onPress: () => { close(); router.push('/member'); } } };
  return {
    primary: { label: '获取兑换码', onPress: () => { close(); router.push('/member/getcode'); } },
    secondary: { label: '我有兑换码', onPress: () => { close(); router.push('/mine/redeem'); } },
  };
}
