import type { Schemas } from '@training/api-client';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from '@/lib/api';

export type ImportJob = Schemas['ImportJob'];
export type ImportItem = Schemas['ImportItem'];
export type ImportMode = Schemas['ImportMode'];
export type QuestionDraft = Schemas['QuestionDraft'];

export const importKeys = {
  jobs: (active: boolean) => ['import-jobs', active] as const,
  job: (id: number) => ['import-job', id] as const,
  items: (id: number, filter: string) => ['import-items', id, filter] as const,
  item: (id: number) => ['import-item', id] as const,
  quota: ['quota'] as const,
};

/** 解析中的任务每 3 秒刷新一次进度；结束后停止。 */
export function isRunning(job?: ImportJob) {
  return job?.status === 'queued' || job?.status === 'running';
}

export function useImportJob(id: number) {
  return useQuery({
    queryKey: importKeys.job(id),
    queryFn: () => unwrap(api.GET('/import-jobs/{jobId}', { params: { path: { jobId: id } } })),
    refetchInterval: (q) => (isRunning(q.state.data) ? 3000 : false),
  });
}

/** 首页 2.1b：解析中或待确认的任务。 */
export function useActiveImportJobs() {
  return useQuery({
    queryKey: importKeys.jobs(true),
    queryFn: () => unwrap(api.GET('/import-jobs', { params: { query: { active: true } } })),
    refetchInterval: (q) => (q.state.data?.items.some(isRunning) ? 5000 : false),
  });
}

export function useImportItem(id: number) {
  return useQuery({
    queryKey: importKeys.item(id),
    queryFn: () => unwrap(api.GET('/import-items/{itemId}', { params: { path: { itemId: id } } })),
  });
}

export function useQuota() {
  return useQuery({ queryKey: importKeys.quota, queryFn: () => unwrap(api.GET('/quota')) });
}

/** 1.6 每个文件的步骤文案：识别文字 · 拆分题目 · 配答案和采分点。 */
export const stepGroups = [
  { label: '识别文字', steps: ['queued', 'extract', 'moderate'] },
  { label: '拆分题目', steps: ['split', 'structure'] },
  { label: '配答案和采分点', steps: ['match', 'rubric', 'tag', 'dedupe', 'done'] },
] as const;

export function stepIndex(step: Schemas['ImportStep']) {
  return stepGroups.findIndex((g) => (g.steps as readonly string[]).includes(step));
}

export const qtypeNames: Record<Schemas['QuestionType'], string> = {
  single_choice: '单选',
  multi_choice: '多选',
  true_false: '判断',
  fill_blank: '填空',
  term: '名词解释',
  short_answer: '简答',
  discussion: '论述',
  essay: '作文',
  calculation: '计算',
  other: '其他',
};

export const essayTypeNames: Partial<Record<Schemas['ImportItemType'], string>> = {
  essay_topic: '作文题',
  essay_rubric: '评分细则',
  writing_method: '写作方法',
  essay_material: '素材',
  model_essay: '范文',
};

export function isSubjective(q?: QuestionDraft) {
  return !!q && ['term', 'short_answer', 'discussion', 'essay'].includes(q.qtype);
}

/** 1.7 每条的状态：已确认、需核对、缺答案、采分点待确认、疑似重复。 */
export function itemStatus(it: ImportItem): { label: string; tone: 'mastered' | 'danger' | 'progress' | 'info' | 'neutral' } {
  const r = it.review_reasons;
  if (it.status === 'confirmed' || it.status === 'edited') {
    if (!r.includes('duplicate') && !r.includes('low_confidence')) return { label: '已确认', tone: 'mastered' };
  }
  if (r.includes('duplicate')) return { label: '疑似重复', tone: 'danger' };
  if (r.includes('low_confidence') || r.includes('rubric_sum_mismatch')) return { label: '需核对', tone: 'danger' };
  if (r.includes('missing_answer')) return { label: '缺答案', tone: 'progress' };
  if (r.includes('rubric_unconfirmed')) return { label: '采分点待确认', tone: 'info' };
  return { label: '待确认', tone: 'neutral' };
}

/** 每条下面的一行说明。 */
export function itemNote(it: ImportItem): string | undefined {
  const r = it.review_reasons;
  if (r.includes('duplicate')) return '题库里已有相同或很像的题，入库后会有两道';
  if (r.includes('low_confidence')) return '原文不太清楚，请对照原文核对题干和答案';
  if (r.includes('rubric_sum_mismatch')) return '采分点分值合计不等于题目分值';
  if (r.includes('missing_answer')) return '没找到参考答案，可以让 AI 生成';
  if (r.includes('rubric_unconfirmed')) return 'AI 从参考答案里提取了采分点，请核对';
  return undefined;
}

export function rubricTotal(points: Schemas['RubricPointInput'][] = []) {
  return Math.round(points.reduce((s, p) => s + (p.score ?? 0), 0) * 100) / 100;
}

export function sourceText(src?: Schemas['SourceRef']) {
  if (!src) return undefined;
  const name = src.file_name.replace(/\.[^.]+$/, '');
  return src.page ? `${name} · 第 ${src.page} 页` : name;
}
