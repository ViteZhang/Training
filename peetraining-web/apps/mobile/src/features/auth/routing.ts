import type { Href } from 'expo-router';

/**
 * 登录与引导分流（PRD 4 主要入口、0.1）：
 * 未登录 → 0.2 登录；已登录未完成引导 → 回到中断的步骤；完成引导 → 2.1 今日。
 * 引导各步的页面在 T07、T12 加入，这里按步骤号映射。
 */
export function routeFor(state: { loggedIn: boolean; onboardingStep?: string }): Href {
  if (!state.loggedIn) return '/(auth)/login';
  const step = state.onboardingStep ?? '1.1';
  if (step === 'done') return '/(tabs)/today';
  return '/(onboarding)/subject';
}
