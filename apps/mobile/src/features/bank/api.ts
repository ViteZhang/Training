import type { Schemas } from '@training/api-client';
import { useQuery } from '@tanstack/react-query';
import { api, unwrap } from '@/lib/api';

export type KnowledgeNode = Schemas['KnowledgeNode'];
export type KPDetail = Schemas['KnowledgePointDetail'];
export type QuestionDetail = Schemas['QuestionDetail'];
export type Material = Schemas['Material'];

export const bankKeys = {
  overview: (s: number) => ['bank', s, 'overview'] as const,
  tree: (s: number, filter: string) => ['bank', s, 'tree', filter] as const,
  questions: (s: number, f: unknown) => ['bank', s, 'questions', f] as const,
  materials: (s: number) => ['bank', s, 'materials'] as const,
  kp: (id: number) => ['kp', id] as const,
  question: (id: number) => ['question', id] as const,
  page: (m: number, p: number, h?: string) => ['material-page', m, p, h ?? ''] as const,
};

/** 题库相关的数据改了之后统一刷新（知识点、题目、资料互相影响）。 */
export const bankRoot = (subjectId: number) => ['bank', subjectId] as const;

export function useOverview(subjectId?: number) {
  return useQuery({
    queryKey: bankKeys.overview(subjectId ?? 0),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/bank', { params: { path: { subjectId: subjectId! } } })),
    enabled: !!subjectId,
  });
}

export function useMaterials(subjectId?: number) {
  return useQuery({
    queryKey: bankKeys.materials(subjectId ?? 0),
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/materials', { params: { path: { subjectId: subjectId! } } })),
    enabled: !!subjectId,
  });
}

export function useKP(id: number) {
  return useQuery({ queryKey: bankKeys.kp(id), queryFn: () => unwrap(api.GET('/knowledge-points/{kpId}', { params: { path: { kpId: id } } })) });
}

export function useQuestion(id: number) {
  return useQuery({ queryKey: bankKeys.question(id), queryFn: () => unwrap(api.GET('/questions/{questionId}', { params: { path: { questionId: id } } })) });
}

export const stateNames: Record<Schemas['MasteryState'], string> = { unlearned: '未学习', learning: '学习中', consolidating: '待巩固', mastered: '已掌握' };
export const stateTone: Record<Schemas['MasteryState'], 'neutral' | 'info' | 'progress' | 'mastered'> = {
  unlearned: 'neutral',
  learning: 'info',
  consolidating: 'progress',
  mastered: 'mastered',
};

export const statusTagNames: Record<Schemas['QuestionStatusTag'], string> = {
  mastered: '已掌握',
  wrong: '错题',
  needs_review: '待核对',
  unanswered: '还没做过',
  answered: '已做过',
};

export const sourceNames: Record<Schemas['QuestionSource'], string> = { exam: '真题', exercise: '习题', ai_generated: 'AI 出题', official: '官方' };

/** 题目来源的展示：真题带年份。 */
export function sourceLabel(source: Schemas['QuestionSource'], year?: number) {
  return source === 'exam' && year ? `${year} 真题` : sourceNames[source];
}

export const formatBadge: Record<Schemas['MaterialFormat'], string> = { pdf: 'PDF', docx: 'DOC', xlsx: 'XLS', image: 'IMG', text: 'TXT' };

/** 3.1c 资料行的说明：页数、识别出的题数、识别不完整的提醒。 */
export function materialMeta(m: Material) {
  const unit = m.format === 'image' ? '张' : '页';
  const parts = [`${m.page_count} ${unit}`];
  if (m.status === 'uploading' || m.status === 'uploaded' || m.status === 'parsing') parts.push('整理中');
  else if (m.status === 'failed' || m.status === 'rejected') parts.push(m.fail_reason ?? '没能识别');
  else if (m.status === 'partial') parts.push(`只识别出 ${m.question_count} 题，可能有遗漏`);
  else {
    parts.push(m.question_count > 0 ? `识别出 ${m.question_count} 题` : `${m.kp_count} 个知识点`);
    if (m.paper_count) parts.push(`${m.paper_count} 套真题卷`);
    if (m.needs_review_count) parts.push(`${m.needs_review_count} 处待核对`);
  }
  return parts.join(' · ');
}

export function shortDate(iso: string) {
  const d = new Date(iso);
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

export const relationNames: Record<Schemas['RelationType'], string> = { contrast: '易混对比', component: '组成要素', sibling: '同章并列', related: '相关' };
