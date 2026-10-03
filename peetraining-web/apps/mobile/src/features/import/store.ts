import { create } from 'zustand';
import type { ImportMode } from './api';
import type { Picked } from './files';

/** 导入流程（1.4 → 1.5）里跨页面的选择：导入方式、专业课、已选文件。只在内存里，离开流程即清空。 */
interface ImportFlow {
  mode: ImportMode;
  subjectId?: number;
  /** 从引导进入（1.8 完成后结束引导） */
  onboarding: boolean;
  files: Picked[];
  start: (mode: ImportMode, subjectId: number, onboarding: boolean) => void;
  add: (files: Picked[]) => void;
  update: (key: string, patch: Partial<Picked>) => void;
  remove: (key: string) => void;
  reset: () => void;
}

export const useImportFlow = create<ImportFlow>((set) => ({
  mode: 'question',
  onboarding: false,
  files: [],
  start: (mode, subjectId, onboarding) => set({ mode, subjectId, onboarding, files: [] }),
  add: (files) => set((s) => ({ files: [...s.files, ...files] })),
  update: (key, patch) => set((s) => ({ files: s.files.map((f) => (f.key === key ? { ...f, ...patch } : f)) })),
  remove: (key) => set((s) => ({ files: s.files.filter((f) => f.key !== key) })),
  reset: () => set({ files: [], onboarding: false }),
}));
