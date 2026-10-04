import type { Href } from 'expo-router';

/** 引导步骤 → 页面（PRD 1 模块：1.1–1.5 是 5 步进度条的第 1–5 步）。1.4 起的导入页面在 T12 实现。 */
const onboardingRoutes: Record<string, Href> = {
  '1.1': '/(onboarding)/subject',
  '1.2': '/(onboarding)/target',
  '1.3': '/(onboarding)/setup',
};

/**
 * 登录与引导分流（PRD 4 主要入口、0.1）：
 * 未登录 → 0.2 登录；已登录未完成引导 → 回到中断的步骤；完成引导 → 2.1 今日。
 */
export function routeFor(state: { loggedIn: boolean; onboardingStep?: string }): Href {
  if (!state.loggedIn) return '/(auth)/login';
  const step = state.onboardingStep ?? '1.1';
  if (step === 'done') return '/(tabs)/today';
  return onboardingRoutes[step] ?? '/(onboarding)/import';
}
