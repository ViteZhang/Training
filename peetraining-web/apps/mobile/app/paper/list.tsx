// 4.18 整卷列表：真题卷按年份（进行中 / 已完成 / 未开始；缺题卷说明按多少分计分）；AI 组卷（标准卷、针对卷）与已做的 AI 组卷；
// 「AI 组卷成绩只作参考，不计入预估分」。
import type { Schemas } from '@training/api-client';
import { fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Icon, Loading, Screen, Tag, Text, toast } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { modeNames, paperKeys, usePapers } from '@/features/paper/api';
import { api, unwrap } from '@/lib/api';

type Brief = Schemas['PaperBrief'];

function day(iso: string) {
  const d = new Date(iso);
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

function PaperRow({ p, first }: { p: Brief; first?: boolean }) {
  const open = () =>
    p.session
      ? router.push({ pathname: '/paper/session/[id]', params: { id: String(p.session.id) } })
      : router.push({ pathname: '/paper/[id]', params: { id: String(p.id) } });
  let desc = `${p.question_count} 题 · ${p.full_score} 分 · ${p.duration_minutes} 分钟`;
  if (p.session) desc = `进行中 · 已答 ${p.session.answered} / ${p.session.total} · ${modeNames[p.session.mode]}`;
  else if (p.last) desc = `${day(p.last.finished_at)} · ${modeNames[p.last.mode]}${p.last.status === 'grading' ? ' · 批改中' : ''}`;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={p.title} onPress={open} style={[styles.row, !first && styles.divider]}>
      <View style={styles.flex}>
        <View style={styles.titleRow}>
          <Text variant="body">{p.title}</Text>
          {p.ai_filled > 0 ? <Tag label={`AI 补 ${p.ai_filled} 题`} tone="ai" /> : null}
        </View>
        <Text variant="small">{desc}</Text>
        {p.missing_note ? (
          <Text variant="small" color="#9A5B00">
            {p.missing_note}
          </Text>
        ) : null}
      </View>
      {p.last?.score !== undefined && !p.session ? (
        <Text style={styles.score}>
          {p.last.score}
          <Text variant="small"> / {p.last.full_score}</Text>
        </Text>
      ) : (
        <View style={[styles.pill, p.session && styles.pillInk]}>
          <Text variant="caption" color={p.session ? semantic.textOnBrand : semantic.textPrimary}>
            {p.session ? '继续' : p.status === 'done' ? '再做' : '开始'}
          </Text>
        </View>
      )}
    </Pressable>
  );
}

export default function PaperList() {
  const { subjectId } = useLocalSearchParams<{ subjectId: string }>();
  const sid = Number(subjectId);
  const qc = useQueryClient();
  const list = usePapers(sid);
  const [all, setAll] = useState(false);
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
  // 设计稿 4.18：未开始的真题卷只露出最近两套，其余折叠
  const notStarted = l.real_exam.filter((p) => !p.session && p.status === 'not_started');
  const hidden = all ? [] : notStarted.slice(2);
  const shown = l.real_exam.filter((p) => !hidden.includes(p));
  const remaining = l.weekly_remaining;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader
          title="整卷"
          onBack={() => router.back()}
          right={<Button title="+ 导入卷子" kind="text" size="sm" color={semantic.textPrimary} style={styles.link} onPress={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />}
        />
        {l.in_progress && l.in_progress.subject_id !== sid ? (
          <Card style={styles.card}>
            <Text variant="caption">「{l.in_progress.title}」还没做完，同一时间只能做一套</Text>
            <Button title="继续那一套" kind="secondary" onPress={() => router.push({ pathname: '/paper/session/[id]', params: { id: String(l.in_progress!.session_id) } })} />
          </Card>
        ) : null}
        <View style={styles.sectionHead}>
          <Text variant="h3" style={styles.flex}>
            真题卷 · {l.real_exam.length} 套
          </Text>
          {remaining !== null && remaining !== undefined ? <Text variant="small">本周还能批改 {remaining} 套</Text> : null}
        </View>
        {l.real_exam.length === 0 ? (
          <Card>
            <EmptyState title="还没有真题卷" desc="导入历年真题后，会按年份组成整卷" actionText="导入真题" onAction={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />
          </Card>
        ) : (
          <Card style={styles.list}>
            {shown.map((p, i) => (
              <PaperRow key={p.id} p={p} first={i === 0} />
            ))}
            {hidden.length > 0 ? (
              <Pressable accessibilityRole="button" onPress={() => setAll(true)} style={[styles.row, styles.divider]}>
                <Text variant="caption" style={styles.flex}>
                  还有 {hidden.length} 套未开始
                  {hidden[hidden.length - 1]?.exam_year && hidden[0]?.exam_year ? `（${hidden[hidden.length - 1]!.exam_year}–${hidden[0]!.exam_year} 年）` : ''}
                </Text>
                <Icon name="down" size={16} color={semantic.textSecondary} />
              </Pressable>
            ) : null}
          </Card>
        )}
        <View style={styles.sectionHead}>
          <Text variant="h3" style={styles.flex}>
            AI 组卷
          </Text>
          <Text variant="small">真题做完了也有卷子练</Text>
        </View>
        <Card tone="fill" style={styles.card}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.lh}>
            按你真题的题型结构从你的题库里抽题组卷，做过的真题不会再出现。题目不够时用 AI 变式题补足，会标出来。
          </Text>
          <View style={styles.compose}>
            <Pressable accessibilityRole="button" disabled={compose.isPending || l.real_exam.length === 0} onPress={() => compose.mutate('ai_standard')} style={styles.tile}>
              <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
                标准卷
              </Text>
              <Text variant="small">按真题板块比例</Text>
            </Pressable>
            <Pressable accessibilityRole="button" disabled={compose.isPending || l.real_exam.length === 0} onPress={() => compose.mutate('ai_targeted')} style={styles.tile}>
              <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
                针对卷
              </Text>
              <Text variant="small">多出你的薄弱点</Text>
            </Pressable>
          </View>
          {compose.isPending ? <Text variant="caption">正在组卷…</Text> : null}
        </Card>
        {l.ai_papers.length > 0 ? (
          <Card style={styles.list}>
            {l.ai_papers.map((p, i) => (
              <PaperRow key={p.id} p={p} first={i === 0} />
            ))}
          </Card>
        ) : null}
        <Text variant="small" style={styles.center}>
          做完的真题卷会用来算今日页的预估分，AI 组卷的成绩只作参考、不计入
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  card: { gap: 12 },
  list: { paddingVertical: 0 },
  sectionHead: { flexDirection: 'row', alignItems: 'baseline', marginTop: 6 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 62, paddingVertical: 10 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 22 },
  center: { textAlign: 'center' },
  link: { paddingHorizontal: 0 },
  score: { fontFamily: fontFamily.numberSemiBold, fontSize: 18, color: semantic.textPrimary },
  pill: { minHeight: 32, paddingHorizontal: 14, borderRadius: radius.pill, justifyContent: 'center', backgroundColor: semantic.fill },
  pillInk: { backgroundColor: semantic.primary },
  compose: { flexDirection: 'row', gap: 10 },
  tile: { flex: 1, minHeight: 60, padding: 14, gap: 2, borderRadius: radius.lg, backgroundColor: semantic.surface },
});
