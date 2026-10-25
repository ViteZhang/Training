// 3.1b 题目：按题型筛选（显示各题型题数）、按来源筛选、按章节或最近排序；每题显示得分或状态、所属板块章节、做过几次；
// AI 出题注明由哪个知识点生成。「多选」后可以批量删除（如不是自己报考专业的题）。
import { ApiError, type Schemas } from '@training/api-client';
import { colors, radius, semantic } from '@training/ui-tokens';
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, ConfirmDialog, EmptyState, ErrorState, GroupTag, Icon, Loading, Tag, Text, toast } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { Segments } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';
import { bankKeys, sourceLabel, sourceNames, statusTagNames } from './api';

type Source = Schemas['QuestionSource'] | 'all';
const sources: Source[] = ['all', 'exam', 'exercise', 'ai_generated'];

function QuestionRow({ q, selecting, selected, onToggle }: { q: Schemas['QuestionSummary']; selecting?: boolean; selected?: boolean; onToggle?: () => void }) {
  const meta = [q.path.join(' · ')];
  if (q.attempt_count > 0) meta.push(`做过 ${q.attempt_count} 次`);
  if (q.generated_from_kp) meta.push(`由「${q.generated_from_kp}」生成`);
  const bad = q.status_tag === 'wrong' || q.status_tag === 'needs_review';
  const state = q.last_score !== undefined && q.score !== undefined && q.status_tag !== 'needs_review' ? `${q.last_score} / ${q.score}` : statusTagNames[q.status_tag];
  return (
    <Pressable
      accessibilityRole={selecting ? 'checkbox' : 'button'}
      accessibilityState={selecting ? { checked: !!selected } : undefined}
      onPress={() => (selecting ? onToggle?.() : router.push({ pathname: '/bank/question/[id]', params: { id: String(q.id) } }))}
      style={[styles.row, selected && styles.rowSelected]}
    >
      <View style={styles.head}>
        {selecting ? (
          <View style={[styles.box, selected && styles.boxOn]}>{selected ? <Icon name="check" size={12} color={semantic.textOnBrand} /> : null}</View>
        ) : null}
        <View style={styles.pill}>
          <Text variant="small" color={semantic.textPrimary} style={styles.medium}>
            {qtypeNames[q.qtype]}
            {q.score !== undefined ? ` · ${q.score} 分` : ''}
          </Text>
        </View>
        {q.source === 'ai_generated' ? <Tag label="AI 出题" tone="ai" /> : <Text variant="small">{sourceLabel(q.source, q.exam_year)}</Text>}
        <GroupTag group={q.group} />
        <View style={styles.flex} />
        <Text variant="small" color={bad ? colors.orange : q.status_tag === 'mastered' ? semantic.mastered : semantic.textPrimary} style={styles.medium}>
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
  const qc = useQueryClient();
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [asking, setAsking] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const exitSelect = () => {
    setSelecting(false);
    setSelected(new Set());
  };
  const toggle = (id: number) =>
    setSelected((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const remove = async () => {
    setDeleting(true);
    try {
      const r = await unwrap(api.POST('/questions/delete', { body: { ids: [...selected] } }));
      await qc.invalidateQueries({ queryKey: ['bank'] });
      toast(`已删除 ${r.deleted} 道题`);
      exitSelect();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '删除没成功，请重试');
    } finally {
      setDeleting(false);
    }
  };
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
      {selecting ? (
        <View style={styles.tools}>
          <Text variant="small" color={semantic.textPrimary} style={styles.count}>
            已选 {selected.size} 道
          </Text>
          <Button title="全选" kind="text" size="sm" style={styles.tool} onPress={() => setSelected(new Set(items.map((q) => q.id)))} />
          <Button title="删除" kind="text" size="sm" color={semantic.danger} disabled={selected.size === 0} loading={deleting} style={styles.tool} onPress={() => setAsking(true)} />
          <Button title="取消" kind="text" size="sm" color={semantic.textPrimary} style={styles.tool} onPress={exitSelect} />
        </View>
      ) : (
        <View style={styles.tools}>
          <Button
            title={`按来源：${source === 'all' ? '全部' : sourceNames[source]}`}
            kind="text"
            size="sm"
            style={styles.tool}
            onPress={() => setSource(sources[(sources.indexOf(source) + 1) % sources.length]!)}
          />
          <View style={styles.flex} />
          <Button title={sort === 'chapter' ? '按章节排序 ▾' : '最近做过 ▾'} kind="text" size="sm" color={semantic.textPrimary} style={styles.tool} onPress={() => setSort(sort === 'chapter' ? 'recent' : 'chapter')} />
          {items.length > 0 ? <Button title="多选" kind="text" size="sm" color={semantic.textPrimary} style={styles.tool} onPress={() => setSelecting(true)} /> : null}
        </View>
      )}
      {items.length === 0 ? (
        <EmptyState title="没有符合条件的题" />
      ) : (
        items.map((q) => <QuestionRow key={q.id} q={q} selecting={selecting} selected={selected.has(q.id)} onToggle={() => toggle(q.id)} />)
      )}
      {list.hasNextPage ? <Button title="加载更多" kind="secondary" loading={list.isFetchingNextPage} onPress={() => void list.fetchNextPage()} /> : null}
      <ConfirmDialog
        visible={asking}
        title={`删除所选 ${selected.size} 道题？`}
        message="会一起删除它们的作答记录和错题记录，删除后不能恢复。"
        confirmText="删除"
        danger
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false);
          void remove();
        }}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 10 },
  tools: { flexDirection: 'row', alignItems: 'center', gap: 14 },
  tool: { paddingHorizontal: 0 },
  count: { flex: 1, fontWeight: '500' },
  rowSelected: { borderColor: semantic.textPrimary },
  box: { width: 20, height: 20, borderRadius: 10, borderWidth: 1.5, borderColor: colors.faint, alignItems: 'center', justifyContent: 'center' },
  boxOn: { backgroundColor: semantic.textPrimary, borderColor: semantic.textPrimary },
  row: { gap: 8, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  head: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  pill: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill, backgroundColor: semantic.fill },
  medium: { fontWeight: '500' },
  flex: { flex: 1 },
});
