import { api, unwrap } from '@/lib/api';

/** 记录引导进度：中途退出，下次启动回到这一步（PRD 4）。 */
export async function setStep(step: '1.1' | '1.2' | '1.3' | '1.4' | '1.5' | '1.6' | '1.7' | '1.8' | 'done') {
  await unwrap(api.PATCH('/me', { body: { onboarding_step: step } }));
}

export const stageInfo = {
  foundation: { name: '基础期', desc: '系统学新知识点，边学边练', range: '150 天以上' },
  strengthen: { name: '强化期', desc: '题型专项和查漏为主', range: '60–149 天' },
  sprint: { name: '冲刺期', desc: '整卷练习，查漏补缺', range: '14–59 天' },
  final: { name: '考前期', desc: '背诵和模拟考试，保持手感', range: '14 天以内' },
} as const;

export type StageKey = keyof typeof stageInfo;
export const stages = Object.keys(stageInfo) as StageKey[];
