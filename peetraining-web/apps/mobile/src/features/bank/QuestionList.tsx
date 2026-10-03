// 3.1b 题目：按题型筛选（显示各题型题数）、按来源筛选、按章节或最近排序；每题显示得分或状态、所属板块章节、做过几次；
// AI 出题注明由哪个知识点生成。
import type { Schemas } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
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
  const tone = q.status_tag === 'wrong' || q.status_tag === 'needs_review' ? 'danger' : q.status_tag === 'mastered' ? 'mastered' : 'neutral';
  return (
    <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/question/[id]', params: { id: String(q.id) } })} style={styles.row}>
      <View style={styles.head}>
        <Text variant="caption" style={styles.flex}>
          {qtypeNames[q.qtype]}
          {q.score !== undefined ? ` · ${q.score} 分` : ''} · {sourceLabel(q.source, q.exam_year)}
        </Text>
        <Tag label={q.last_score !== undefined && q.score !== undefined && q.status_tag !== 'needs_review' ? `${q.last_score} / ${q.score}` : statusTagNames[q.status_tag]} tone={tone} />
      </View>
      <Text variant="body" numberOfLines={2}>
        {q.stem}
      </Text>
      {meta.filter(Boolean).length ? <Text variant="caption">{meta.filter(Boolean).join(' · ')}</Text> : null}
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
          onPress={() => setSource(sources[(sources.indexOf(source) + 1) % sources.length]!)}
        />
        <Button title={sort === 'chapter' ? '按章节排序' : '最近做过'} kind="text" onPress={() => setSort(sort === 'chapter' ? 'recent' : 'chapter')} />
      </View>
      {items.length === 0 ? <EmptyState title="没有符合条件的题" /> : items.map((q) => <QuestionRow key={q.id} q={q} />)}
      {list.hasNextPage ? <Button title="加载更多" kind="secondary" loading={list.isFetchingNextPage} onPress={() => void list.fetchNextPage()} /> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: spacing.sm },
  tools: { flexDirection: 'row', justifyContent: 'space-between' },
  row: { paddingVertical: spacing.md, gap: spacing.xs, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: semantic.border },
  head: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
});
