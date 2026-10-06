// 1.7 确认导入结果：汇总「N 份文件 · 识别出 N 题 · N 处需要核对」；筛选全部 / 需核对 / 主观题 / 客观题；
// 每条显示题型、分值、状态与说明；需核对的题也可以先入库并带标记；「确认入库 N 题」。
import { ApiError, type Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { Button, EmptyState, ErrorState, Loading, QuotaSheet, Screen, Text, toast } from '@/components';
import { essayTypeNames, importKeys, itemNote, itemStatus, qtypeNames, useImportJob, type ImportItem } from '@/features/import/api';
import { PageHeader, Segments } from '@/features/import/ui';
import { useImportFlow } from '@/features/import/store';
import { Footer, StepHeader } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

type Filter = 'all' | 'needs_review' | 'subjective' | 'objective';

function title(it: ImportItem) {
  if (it.question) return it.question.stem;
  if (it.knowledge_point) return it.knowledge_point.name;
  const e = it.essay ?? {};
  return String(e.title ?? e.name ?? e.theme ?? '');
}

function kindLabel(it: ImportItem) {
  if (it.question) {
    const score = it.question.score !== undefined ? ` · ${it.question.score} 分` : '';
    const year = it.question.exam_year ? ` · ${it.question.exam_year} 真题` : '';
    return `${qtypeNames[it.question.qtype]}${score}${year}`;
  }
  if (it.knowledge_point) return `知识点 · ${it.knowledge_point.kp_path.join(' / ')}`;
  return essayTypeNames[it.item_type] ?? '作文资料';
}

const warnInk = '#9A5B00';
const statusInk: Record<string, string> = { mastered: '#1F7A4D', danger: warnInk, progress: warnInk, info: warnInk, neutral: '#1B1A17' };

export function ItemRow({ it, onPress }: { it: ImportItem; onPress: () => void }) {
  const st = itemStatus(it);
  const note = itemNote(it);
  const check = it.needs_review && st.tone !== 'mastered';
  return (
    <Pressable accessibilityRole="button" onPress={onPress} style={[styles.item, check && styles.itemCheck]}>
      <View style={styles.itemHead}>
        <View style={[styles.kind, check && styles.kindCheck]}>
          <Text variant="small" color={semantic.textPrimary} style={styles.medium}>
            {kindLabel(it)}
          </Text>
        </View>
        <View style={styles.flex} />
        <Text variant="small" color={statusInk[st.tone]} style={styles.medium}>
          {st.label}
        </Text>
      </View>
      <Text variant="body" numberOfLines={2}>
        {title(it)}
      </Text>
      {note ? (
        <Text variant="small" color={check ? '#6B4A12' : undefined}>
          {note}
        </Text>
      ) : null}
    </Pressable>
  );
}

export default function ConfirmScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const jobId = Number(id);
  const onboarding = useImportFlow((x) => x.onboarding);
  const qc = useQueryClient();
  const job = useImportJob(jobId);
  const [filter, setFilter] = useState<Filter>('all');
  const [busy, setBusy] = useState(false);
  const [quota, setQuota] = useState(false);
  const items = useInfiniteQuery({
    queryKey: importKeys.items(jobId, filter),
    queryFn: ({ pageParam }) =>
      unwrap(api.GET('/import-jobs/{jobId}/items', { params: { path: { jobId }, query: { filter, cursor: pageParam, limit: 30 } } })),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
  });

  const counts: Schemas['ImportCounts'] | undefined = items.data?.pages[0]?.counts ?? job.data?.counts;
  const list = items.data?.pages.flatMap((p) => p.items) ?? [];
  const total = counts ? counts.questions + counts.knowledge_points + (counts.essay_items ?? 0) - counts.confirmed : 0;
  const unit = counts && counts.questions === 0 ? '条' : '题';

  const confirm = async () => {
    setBusy(true);
    try {
      const r = await unwrap(api.POST('/import-jobs/{jobId}/confirm', { params: { path: { jobId } } }));
      track('confirm_submit');
      await Promise.all([qc.invalidateQueries({ queryKey: ['subjects'] }), qc.invalidateQueries({ queryKey: ['import-jobs'] }), qc.invalidateQueries({ queryKey: ['quota'] })]);
      router.replace({
        pathname: '/import/done/[id]',
        params: {
          id: String(jobId),
          questions: String(r.question_count),
          kps: String(r.kp_count),
          review: String(r.needs_review_count),
          papers: String(r.paper_count ?? 0),
          without: (r.subjects_without_import ?? []).join(','),
        },
      });
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) setQuota(true);
      else toast(e instanceof ApiError ? e.message : '入库没成功，请重试');
    } finally {
      setBusy(false);
    }
  };

  if (job.isLoading || items.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (job.isError || items.isError) return <Screen><ErrorState error={job.error ?? items.error} onRetry={() => void Promise.all([job.refetch(), items.refetch()])} /></Screen>;

  const files = job.data?.materials.length ?? 0;
  return (
    <Screen>
      <FlatList
        data={list}
        keyExtractor={(it) => String(it.id)}
        ListHeaderComponent={
          <View style={styles.header}>
            {onboarding ? (
              <StepHeader step={5} title="确认导入结果" onBack={() => router.back()} />
            ) : (
              <PageHeader title="确认导入结果" large onBack={() => router.back()} />
            )}
            <Text variant="caption" style={styles.summary}>
              {files} 份文件 · 识别出 <Text variant="caption" color={semantic.textPrimary}>{counts ? counts.questions + counts.knowledge_points + (counts.essay_items ?? 0) : 0}</Text> {unit}
              {counts?.needs_review ? <Text variant="caption" color={warnInk}> · {counts.needs_review} 处需要核对</Text> : null}
            </Text>
            <Segments<Filter>
              value={filter}
              onChange={setFilter}
              options={[
                { key: 'all', label: '全部' },
                { key: 'needs_review', label: '需核对', count: counts?.needs_review },
                { key: 'subjective', label: '主观题', count: counts?.subjective },
                { key: 'objective', label: '客观题', count: counts?.objective },
              ]}
            />
          </View>
        }
        renderItem={({ item }) => <ItemRow it={item} onPress={() => router.push({ pathname: '/import/item/[id]', params: { id: String(item.id), job: String(jobId) } })} />}
        ListEmptyComponent={<EmptyState title={filter === 'all' ? '还没有识别出的内容' : '这一类没有'} desc={filter === 'all' ? '解析还在进行中，稍后再来看看' : undefined} />}
        onEndReached={() => {
          if (items.hasNextPage && !items.isFetchingNextPage) void items.fetchNextPage();
        }}
        contentContainerStyle={styles.list}
      />
      <Footer>
        <Text variant="small">需要核对的题也会入库并标出来，之后随时能改</Text>
        <Button title={total > 0 ? `确认入库 ${total} ${unit}` : '都已入库'} disabled={total <= 0} loading={busy} onPress={() => void confirm()} />
      </Footer>
      <QuotaSheet
        visible={quota}
        onClose={() => setQuota(false)}
        title="题目导入额度不够了"
        desc="免费版最多导入 500 题。开通会员后不限题数。"
        onUpgrade={() => {
          setQuota(false);
          router.push('/member');
        }}
        freeOptions={[{ label: '删掉一些再入库', onPress: () => setQuota(false) }]}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  header: { gap: 14, paddingBottom: 14 },
  summary: { fontSize: 14, lineHeight: 21, marginTop: -8 },
  list: { paddingBottom: spacing.xl, gap: 10 },
  item: { gap: 8, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  itemCheck: { borderColor: '#F3DDB3', backgroundColor: semantic.amberSoft },
  itemHead: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  kind: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill, backgroundColor: semantic.fill },
  kindCheck: { backgroundColor: semantic.surface },
  medium: { fontWeight: '500' },
  flex: { flex: 1 },
});
