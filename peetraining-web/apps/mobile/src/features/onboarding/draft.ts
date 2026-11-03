import { getJSON, setJSON } from '@/lib/storage';

/** 引导过程中还没提交到服务端的选择（考试年份、目标院校），在 1.3 一起写入备考档案。 */
export interface OnboardingDraft {
  examYear?: number;
  targetSchoolMajor?: string;
  /** 新 1.1（引导分流）选的院校与专业，1.11 申请收录从这里带入 */
  school?: string;
  major?: string;
  /** 1.1 选的课里没有官方题库的（1.1a 下方列出、1.1c 显示、1.11 带入） */
  noOfficial?: { subjectId: number; name: string; code?: string; major: string }[];
  /** 1.1 从院校专业课库选的课（1.1a 据此列出官方题库） */
  picked?: number[];
  /** 首次批改后 1.2、1.3 走完（或跳过）要去的页面（PRD v3 增补：1.2–1.3 移到首次批改之后） */
  after?: string;
}

const KEY = 'onboarding_draft';

export const loadDraft = () => getJSON<OnboardingDraft>(KEY) ?? {};
export const saveDraft = (d: OnboardingDraft) => setJSON(KEY, { ...loadDraft(), ...d });
