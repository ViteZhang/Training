// 4.18 整卷列表：真题卷按年份（进行中 / 已完成 / 未开始；缺题卷说明按多少分计分）；AI 组卷（标准卷、针对卷）与已做的 AI 组卷；
// 「AI 组卷成绩只作参考，不计入预估分」。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { modeNames, paperKeys, usePapers } from '@/features/paper/api';
import { api, unwrap } from '@/lib/api';

type Brief = Schemas['PaperBrief'];

function day(iso: string) {
  const d = new Date(iso);
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

function PaperRow({ p }: { p: Brief }) {
  const open = () =>
    p.session
      ? router.push({ pathname: '/paper/session/[id]', params: { id: String(p.session.id) } })
      : router.push({ pathname: '/paper/[id]', params: { id: String(p.id) } });
  let desc = `${p.question_count} 题 · ${p.full_score} 分 · ${p.duration_minutes} 分钟`;
  if (p.session) desc = `进行中 · 已答 ${p.session.answered} / ${p.session.total} · ${modeNames[p.session.mode]}`;
  else if (p.last) desc = `${day(p.last.finished_at)} · ${modeNames[p.last.mode]}${p.last.status === 'grading' ? ' · 批改中' : ''}`;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={p.title} onPress={open} style={styles.row}>
      <View style={styles.flex}>
        <View style={styles.titleRow}>
          <Text variant="bodyStrong">{p.title}</Text>
          {p.ai_filled > 0 ? <Tag label={`AI 补 ${p.ai_filled} 题`} tone="ai" /> : null}
        </View>
        <Text variant="caption">{desc}</Text>
        {p.missing_note ? (
          <Text variant="caption" color={semantic.info}>
            {p.missing_note}
          </Text>
        ) : null}
      </View>
      {p.last?.score !== undefined && !p.session ? (
        <Text variant="number">
          {p.last.score}
          <Text variant="caption"> / {p.last.full_score}</Text>
        </Text>
      ) : (
        <Text variant="bodyStrong" color={semantic.primary}>
          {p.session ? '继续' : p.status === 'done' ? '再做' : '开始'}
        </Text>
      )}
    </Pressable>
  );
}

export default function PaperList() {
  const { subjectId } = useLocalSearchParams<{ subjectId: string }>();
  const sid = Number(subjectId);
  const qc = useQueryClient();
  const list = usePapers(sid);
  const compose = useMutation({
    mutationFn: (kind: 'ai_standard' | 'ai_targeted') => unwrap(api.POST('/subjects/{subjectId}/papers', { params: { path: { subjectId: sid } }, body: { kind } })),
    onSuccess: (d) => {
      void qc.invalidateQueries({ queryKey: paperKeys.list(sid) });
      router.push({ pathname: '/paper/[id]', params: { id: String(d.id) } });
    },
    onError: (e) => toast(e instanceof Error ? e.message : '组卷失败，请重试'),
  });
  if (list.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (list.isError || !list.data) return <Screen><ErrorState error={list.error} onRetry={() => void list.refetch()} /></Screen>;
  const l = list.data;
  const remaining = l.weekly_remaining;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader
          title="整卷"
          onBack={() => router.back()}
          right={<Button title="+ 导入卷子" kind="text" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />}
        />
        {l.in_progress && l.in_progress.subject_id !== sid ? (
          <Card style={styles.card}>
            <Text variant="caption">「{l.in_progress.title}」还没做完，同一时间只能做一套</Text>
            <Button title="继续那一套" kind="secondary" onPress={() => router.push({ pathname: '/paper/session/[id]', params: { id: String(l.in_progress!.session_id) } })} />
          </Card>
        ) : null}
        {remaining !== null && remaining !== undefined ? (
          <Text variant="caption">本周还能批改 {remaining} 套整卷</Text>
        ) : null}
        <Card style={styles.list}>
          <Text variant="bodyStrong" style={styles.head}>
            真题卷 · {l.real_exam.length} 套
          </Text>
          {l.real_exam.length === 0 ? (
            <EmptyState title="还没有真题卷" desc="导入历年真题后，会按年份组成整卷" actionText="导入真题" onAction={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />
          ) : (
            l.real_exam.map((p) => <PaperRow key={p.id} p={p} />)
          )}
        </Card>
        <Card style={styles.card}>
          <Text variant="h3">AI 组卷</Text>
          <Text variant="caption">
            按你真题的题型结构从你的题库里抽题组卷，做过的真题不会再出现。题目不够时用 AI 变式题补足，会标出来。
          </Text>
          <View style={styles.compose}>
            <Pressable accessibilityRole="button" disabled={compose.isPending || l.real_exam.length === 0} onPress={() => compose.mutate('ai_standard')} style={styles.tile}>
              <Text variant="bodyStrong">标准卷</Text>
              <Text variant="caption">按真题板块比例</Text>
            </Pressable>
            <Pressable accessibilityRole="button" disabled={compose.isPending || l.real_exam.length === 0} onPress={() => compose.mutate('ai_targeted')} style={styles.tile}>
              <Text variant="bodyStrong">针对卷</Text>
              <Text variant="caption">多出你的薄弱点</Text>
            </Pressable>
          </View>
          {compose.isPending ? <Text variant="caption">正在组卷…</Text> : null}
        </Card>
        {l.ai_papers.length > 0 ? (
          <Card style={styles.list}>
            <Text variant="bodyStrong" style={styles.head}>
              已做 AI 组卷
            </Text>
            {l.ai_papers.map((p) => (
              <PaperRow key={p.id} p={p} />
            ))}
          </Card>
        ) : null}
        <Text variant="small" color={semantic.textSecondary}>
          做完的真题卷会用来算今日页的预估分，AI 组卷的成绩只作参考、不计入
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  card: { gap: spacing.sm },
  list: { paddingVertical: spacing.sm },
  head: { paddingVertical: spacing.xs },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 64, paddingVertical: spacing.sm, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs },
  flex: { flex: 1 },
  compose: { flexDirection: 'row', gap: spacing.sm },
  tile: { flex: 1, minHeight: 64, padding: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
});
