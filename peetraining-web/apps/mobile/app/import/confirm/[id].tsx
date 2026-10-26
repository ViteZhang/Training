// 1.7 确认导入结果：汇总「N 份文件 · 识别出 N 题 · N 处需要核对」；筛选全部 / 需核对 / 主观题 / 客观题；
// 每条显示题型、分值、状态与说明；需核对的题也可以先入库并带标记；「确认入库 N 题」。
// 不是自己要的内容（如非报考专业的分组题）可以直接删：每条右侧删除，或「多选」后批量删除。
// 按专业分组的论述题：顶部卡片选报考方向，只保留那一组；多题共用的材料在这组小题前显示一张材料卡，可整组删除。
import { ApiError, type Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { FlatList, Pressable, StyleSheet, View } from 'react-native';
import { Button, ConfirmDialog, EmptyState, ErrorState, GroupTag, Icon, Loading, PassageCard, QuotaSheet, Screen, Text, toast } from '@/components';
import { essayTypeNames, importKeys, itemNote, itemStatus, qtypeNames, useImportJob, type ImportItem } from '@/features/import/api';
import { GroupBanner } from '@/features/import/groups';
import { PageHeader, Segments } from '@/features/import/ui';
import { useImportFlow } from '@/features/import/store';
import { todayKeys } from '@/features/today/api';
import { Footer, StepHeader } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';
import { useUpgradeActions } from '@/features/membership/purchase';

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

const warnInk = colors.orange;
const statusInk: Record<string, string> = { mastered: colors.green, danger: warnInk, progress: warnInk, info: warnInk, neutral: colors.ink };

interface RowProps {
  it: ImportItem;
  onPress: () => void;
  /** 多选模式：显示勾选框，点整行切换选中 */
  selecting?: boolean;
  selected?: boolean;
  onDelete?: () => void;
}

export function ItemRow({ it, onPress, selecting, selected, onDelete }: RowProps) {
  const st = itemStatus(it);
  const note = itemNote(it);
  const check = it.needs_review && st.tone !== 'mastered';
  return (
    <Pressable
      accessibilityRole={selecting ? 'checkbox' : 'button'}
      accessibilityState={selecting ? { checked: !!selected } : undefined}
      onPress={onPress}
      style={[styles.item, check && styles.itemCheck, selected && styles.itemSelected]}
    >
      {/* 左边题型、分组标签放不下时换行；右边状态和删除固定宽度，不会被挤出屏幕 */}
      <View style={styles.itemHead}>
        {selecting ? (
          <View style={[styles.box, selected && styles.boxOn]}>{selected ? <Icon name="check" size={12} color={semantic.textOnBrand} /> : null}</View>
        ) : null}
        <View style={styles.tags}>
          <View style={[styles.kind, check && styles.kindCheck]}>
            <Text variant="small" color={semantic.textPrimary} style={styles.medium}>
              {kindLabel(it)}
            </Text>
          </View>
          <GroupTag group={it.question?.group} size="md" />
        </View>
        <View style={styles.side}>
          <Text variant="small" color={statusInk[st.tone]} style={styles.medium}>
            {st.label}
          </Text>
          {!selecting && onDelete ? (
            <Pressable accessibilityRole="button" accessibilityLabel="删除这一条" onPress={onDelete} hitSlop={8} style={styles.trash}>
              <Icon name="trash" size={18} color={semantic.textSecondary} />
            </Pressable>
          ) : null}
        </View>
      </View>
      <Text variant="body" numberOfLines={2}>
        {title(it)}
      </Text>
      {note ? (
        <Text variant="small" color={check ? colors.orangeInk : undefined}>
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
  const upgrade = useUpgradeActions(() => setQuota(false));
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [asking, setAsking] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const items = useInfiniteQuery({
    queryKey: importKeys.items(jobId, filter),
    queryFn: ({ pageParam }) =>
      unwrap(api.GET('/import-jobs/{jobId}/items', { params: { path: { jobId }, query: { filter, cursor: pageParam, limit: 30 } } })),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor,
  });

  const counts: Schemas['ImportCounts'] | undefined = items.data?.pages[0]?.counts ?? job.data?.counts;
  const list = items.data?.pages.flatMap((p) => p.items) ?? [];
  const groups = items.data?.pages[0]?.groups ?? [];
  // 共用同一段材料、在列表里连在一起的小题：第一题前显示材料卡
  const passageRun = (index: number) => {
    const key = list[index]?.question?.passage?.key;
    if (!key || list[index - 1]?.question?.passage?.key === key) return null;
    const ids: number[] = [];
    for (let i = index; i < list.length && list[i]?.question?.passage?.key === key; i++) ids.push(list[i]!.id);
    return { text: list[index]!.question!.passage!.text, ids };
  };
  const total = counts ? counts.questions + counts.knowledge_points + (counts.essay_items ?? 0) - counts.confirmed : 0;
  const unit = counts && counts.questions === 0 ? '条' : '题';

  const toggle = (itemId: number) =>
    setSelected((cur) => {
      const next = new Set(cur);
      if (next.has(itemId)) next.delete(itemId);
      else next.add(itemId);
      return next;
    });
  const exitSelect = () => {
    setSelecting(false);
    setSelected(new Set());
  };

  // 删除后服务端返回新的计数；已入库的条目会被跳过（要到题库里删）。
  const remove = async (ids: number[]) => {
    setDeleting(true);
    try {
      const r = await unwrap(api.POST('/import-jobs/{jobId}/items/delete', { params: { path: { jobId } }, body: { ids } }));
      track('confirm_edit', { action: 'delete', count: r.deleted });
      // 删光了这次的条目，首页就不再提示「去核对」，一并刷新
      await Promise.all([
        qc.invalidateQueries({ queryKey: ['import-items', jobId] }),
        qc.invalidateQueries({ queryKey: importKeys.job(jobId) }),
        qc.invalidateQueries({ queryKey: ['import-jobs'] }),
        qc.invalidateQueries({ queryKey: todayKeys.home }),
      ]);
      toast(r.deleted < ids.length ? `已删除 ${r.deleted} 条，已入库的请到题库里删除` : `已删除 ${r.deleted} 条`);
      exitSelect();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '删除没成功，请重试');
    } finally {
      setDeleting(false);
    }
  };

  const confirm = async () => {
    setBusy(true);
    try {
      const r = await unwrap(api.POST('/import-jobs/{jobId}/confirm', { params: { path: { jobId } } }));
      track('confirm_submit');
      await Promise.all([qc.invalidateQueries({ queryKey: ['subjects'] }), qc.invalidateQueries({ queryKey: ['import-jobs'] }), qc.invalidateQueries({ queryKey: ['quota'] }), qc.invalidateQueries({ queryKey: todayKeys.home })]);
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
            <GroupBanner jobId={jobId} groups={groups} />
            {list.length > 0 ? (
              <View style={styles.selectBar}>
                <Text variant="small" style={styles.flex}>
                  {selecting ? `已选 ${selected.size} 条` : '不需要的题可以直接删掉，比如不是你报考专业的分组题'}
                </Text>
                {selecting ? (
                  <Button title="全选" kind="text" size="sm" onPress={() => setSelected(new Set(list.map((x) => x.id)))} />
                ) : null}
                <Button title={selecting ? '取消' : '多选'} kind="text" size="sm" onPress={() => (selecting ? exitSelect() : setSelecting(true))} />
              </View>
            ) : null}
            <Text variant="caption" style={styles.summary}>
              {files} 份文件 · 识别出 <Text variant="caption" num color={semantic.textPrimary}>{counts ? counts.questions + counts.knowledge_points + (counts.essay_items ?? 0) : 0}</Text> {unit}
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
        renderItem={({ item, index }) => {
          const run = passageRun(index);
          const row = (
            <ItemRow
              it={item}
              selecting={selecting}
              selected={selected.has(item.id)}
              onPress={() => (selecting ? toggle(item.id) : router.push({ pathname: '/import/item/[id]', params: { id: String(item.id), job: String(jobId) } }))}
              onDelete={deleting ? undefined : () => void remove([item.id])}
            />
          );
          if (!run) return row;
          return (
            <View style={styles.run}>
              <PassageCard text={run.text} count={run.ids.length} lines={3} />
              {!selecting ? (
                <Button title={`删除材料和这 ${run.ids.length} 题`} kind="text" size="sm" color={semantic.textSecondary} style={styles.runDelete} onPress={() => void remove(run.ids)} />
              ) : null}
              {row}
            </View>
          );
        }}
        ListEmptyComponent={<EmptyState title={filter === 'all' ? '还没有识别出的内容' : '这一类没有'} desc={filter === 'all' ? '解析还在进行中，稍后再来看看' : undefined} />}
        onEndReached={() => {
          if (items.hasNextPage && !items.isFetchingNextPage) void items.fetchNextPage();
        }}
        contentContainerStyle={styles.list}
      />
      {selecting ? (
        <Footer>
          <Button title={selected.size > 0 ? `删除所选 ${selected.size} 条` : '选择要删除的条目'} kind="danger" disabled={selected.size === 0} loading={deleting} onPress={() => setAsking(true)} />
        </Footer>
      ) : (
        <Footer>
          <Text variant="small">需要核对的题也会入库并标出来，之后随时能改</Text>
          <Button title={total > 0 ? `确认入库 ${total} ${unit}` : '都已入库'} disabled={total <= 0} loading={busy} onPress={() => void confirm()} />
        </Footer>
      )}
      <ConfirmDialog
        visible={asking}
        title={`删除所选 ${selected.size} 条？`}
        message="删除后不会入库。原资料还在，需要时可以重新导入。"
        confirmText="删除"
        danger
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false);
          void remove([...selected]);
        }}
      />
      <QuotaSheet
        visible={quota}
        onClose={() => setQuota(false)}
        title="题目导入额度不够了"
        desc="免费版最多导入 500 题。开通会员后不限题数。"
        upgrade={upgrade}
        freeDesc="最多导入 500 题，可以先少选几题入库。"
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
  itemCheck: { borderColor: colors.orangeLine, backgroundColor: semantic.peachSoft },
  itemSelected: { borderColor: semantic.textPrimary },
  box: { width: 20, height: 20, borderRadius: 10, borderWidth: 1.5, borderColor: colors.faint, alignItems: 'center', justifyContent: 'center', marginTop: 6 },
  boxOn: { backgroundColor: semantic.textPrimary, borderColor: semantic.textPrimary },
  trash: { width: 32, height: 32, marginRight: -6, alignItems: 'center', justifyContent: 'center' },
  selectBar: { flexDirection: 'row', alignItems: 'center', gap: 4, minHeight: 32 },
  run: { gap: 6 },
  runDelete: { alignSelf: 'flex-end', paddingHorizontal: 0, marginTop: -4 },
  itemHead: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm },
  tags: { flex: 1, flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 6, minHeight: 32, alignContent: 'center' },
  side: { flexDirection: 'row', alignItems: 'center', gap: 2, flexShrink: 0, minHeight: 32 },
  kind: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill, backgroundColor: semantic.fill },
  kindCheck: { backgroundColor: semantic.surface },
  medium: { fontWeight: '500' },
  flex: { flex: 1 },
});
