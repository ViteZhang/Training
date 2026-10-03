import { getJSON, setJSON } from '@/lib/storage';

/** 引导过程中还没提交到服务端的选择（考试年份、目标院校），在 1.3 一起写入备考档案。 */
export interface OnboardingDraft {
  examYear?: number;
  targetSchoolMajor?: string;
}

const KEY = 'onboarding_draft';

export const loadDraft = () => getJSON<OnboardingDraft>(KEY) ?? {};
export const saveDraft = (d: OnboardingDraft) => setJSON(KEY, { ...loadDraft(), ...d });
