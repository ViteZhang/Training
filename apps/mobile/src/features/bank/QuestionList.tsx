// 3.1b 题目：按题型筛选（显示各题型题数）、按来源筛选、按章节或最近排序；每题显示得分或状态、所属板块章节、做过几次；
// AI 出题注明由哪个知识点生成。
import type { Schemas } from '@training/api-client';
import { radius, semantic } from '@training/ui-tokens';
import { useInfiniteQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, EmptyState, ErrorState, Loading, Tag, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { Segments } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';
import { bankKeys, sourceLabel, sourceNames, statusTagNames } from './api';

type Source = Schemas['QuestionSource'] | 'all';
const sources: Source[] = ['all', 'exam', 'exercise', 'ai_generated'];

function QuestionRow({ q }: { q: Schemas['QuestionSummary'] }) {
  const meta = [q.path.join(' · ')];
  if (q.attempt_count > 0) meta.push(`做过 ${q.attempt_count} 次`);
  if (q.generated_from_kp) meta.push(`由「${q.generated_from_kp}」生成`);
  const bad = q.status_tag === 'wrong' || q.status_tag === 'needs_review';
  const state = q.last_score !== undefined && q.score !== undefined && q.status_tag !== 'needs_review' ? `${q.last_score} / ${q.score}` : statusTagNames[q.status_tag];
  return (
    <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(q.id) } })} style={styles.row}>
      <View style={styles.head}>
        <View style={styles.pill}>
          <Text variant="small" color={semantic.textPrimary} style={styles.medium}>
            {qtypeNames[q.qtype]}
            {q.score !== undefined ? ` · ${q.score} 分` : ''}
          </Text>
        </View>
        {q.source === 'ai_generated' ? <Tag label="AI 出题" tone="ai" /> : <Text variant="small">{sourceLabel(q.source, q.exam_year)}</Text>}
        <View style={styles.flex} />
        <Text variant="small" color={bad ? '#9A5B00' : q.status_tag === 'mastered' ? semantic.mastered : semantic.textPrimary} style={styles.medium}>
          {state}
        </Text>
      </View>
      <Text variant="body" numberOfLines={2}>
        {q.stem}
      </Text>
      {meta.filter(Boolean).length ? <Text variant="small">{meta.filter(Boolean).join(' · ')}</Text> : null}
    </Pressable>
  );
}

export function QuestionList({ subjectId, kpId, status }: { subjectId: number; kpId?: number; status?: Schemas['QuestionStatusTag'] }) {
  const [qtype, setQtype] = useState<Schemas['QuestionType'] | 'all'>('all');
  const [source, setSource] = useState<Source>('all');
  const [sort, setSort] = useState<'chapter' | 'recent'>('chapter');
  const filter = { qtype, source, sort, kpId, status };
  const list = useInfiniteQuery({
    queryKey: bankKeys.questions(subjectId, filter),
    queryFn: ({ pageParam }) =>
      unwrap(
        api.GET('/subjects/{subjectId}/questions', {
          params: {
            path: { subjectId },
            query: {
              qtype: qtype === 'all' ? undefined : qtype,
              source: source === 'all' ? undefined : source,
              kp_id: kpId,
              status: status === 'answered' ? undefined : status,
              sort,
              cursor: pageParam,
              limit: 30,
            },
          },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
  });
  if (list.isLoading) return <Loading rows={4} />;
  if (list.isError) return <ErrorState error={list.error} onRetry={() => void list.refetch()} />;
  const first = list.data?.pages[0];
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];
  const byQtype = first?.facets.by_qtype ?? {};
  const qtypes = Object.keys(byQtype) as Schemas['QuestionType'][];
  return (
    <View style={styles.wrap}>
      {qtypes.length > 1 ? (
        <Segments
          value={qtype}
          onChange={setQtype}
          options={[{ key: 'all' as const, label: '全部' }, ...qtypes.map((t) => ({ key: t, label: qtypeNames[t], count: byQtype[t] }))]}
        />
      ) : null}
      <View style={styles.tools}>
        <Button
          title={`按来源：${source === 'all' ? '全部' : sourceNames[source]}`}
          kind="text"
          size="sm"
          style={styles.tool}
          onPress={() => setSource(sources[(sources.indexOf(source) + 1) % sources.length]!)}
        />
        <Button title={sort === 'chapter' ? '按章节排序 ▾' : '最近做过 ▾'} kind="text" size="sm" color={semantic.textPrimary} style={styles.tool} onPress={() => setSort(sort === 'chapter' ? 'recent' : 'chapter')} />
      </View>
      {items.length === 0 ? <EmptyState title="没有符合条件的题" /> : items.map((q) => <QuestionRow key={q.id} q={q} />)}
      {list.hasNextPage ? <Button title="加载更多" kind="secondary" loading={list.isFetchingNextPage} onPress={() => void list.fetchNextPage()} /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 10 },
  tools: { flexDirection: 'row', justifyContent: 'space-between' },
  tool: { paddingHorizontal: 0 },
  row: { gap: 8, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  head: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  pill: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill, backgroundColor: semantic.fill },
  medium: { fontWeight: '500' },
  flex: { flex: 1 },
});
