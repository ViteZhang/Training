// 5.8 作文本：篇数、平均分、最高分；分数趋势；最弱维度及平均分，「去学」→ 写作方法（3.10）；
// 作文列表：题目、日期、来源（真题 / AI 命题 / 自拟）、第几稿、得分。
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { fmtScore, sourceNames, useNotebook } from '@/features/essay/api';

function day(iso: string) {
  const d = new Date(iso);
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

export default function EssayBookPage() {
  const { subjectId } = useLocalSearchParams<{ subjectId: string }>();
  const sid = Number(subjectId);
  const book = useNotebook(sid);
  if (book.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (book.isError || !book.data) return <Screen><ErrorState error={book.error} onRetry={() => void book.refetch()} /></Screen>;
  const b = book.data;
  const top = Math.max(...b.trend.map((t) => t.full_score), 1);
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="作文本" onBack={() => router.back()} />
        {b.items.length === 0 ? (
          <EmptyState title="还没有作文" desc="写一篇作文并批改后，会收在这里" actionText="去写作文" onAction={() => router.replace({ pathname: '/essay', params: { subjectId: String(sid) } })} />
        ) : null}
        {b.count > 0 ? (
          <>
            <View style={styles.stats}>
              <View style={styles.stat}>
                <Text style={styles.statNum}>{b.count}</Text>
                <Text variant="small">篇作文</Text>
              </View>
              <View style={styles.stat}>
                <Text style={styles.statNum}>{fmtScore(b.avg_score)}</Text>
                <Text variant="small">平均分</Text>
              </View>
              <View style={styles.stat}>
                <Text style={styles.statNum}>{fmtScore(b.best)}</Text>
                <Text variant="small">最高分</Text>
              </View>
            </View>
            <Card style={styles.gap}>
              <Text variant="caption" color={colors.ink} style={styles.bold}>
                分数趋势
              </Text>
              <View style={styles.chart}>
                {b.trend.map((t, i) => {
                  const last = i === b.trend.length - 1;
                  return (
                    <View key={t.essay_id} style={styles.col} accessibilityLabel={`${day(t.date)} ${t.score} 分`}>
                      <Text variant="small" color={last ? colors.ink : undefined} style={last ? styles.bold : undefined}>
                        {fmtScore(t.score)}
                      </Text>
                      <View style={[styles.bar, { height: Math.max((t.score / top) * 110, 4) }, last && styles.barLast]} />
                      <Text variant="small" style={styles.tiny}>
                        {new Date(t.date).getMonth() + 1}/{new Date(t.date).getDate()}
                      </Text>
                    </View>
                  );
                })}
              </View>
            </Card>
            {b.weakest ? (
              <View style={styles.weak}>
                <Text variant="caption" color={colors.ink} style={styles.flex}>
                  最弱维度：<Text variant="caption" color={colors.ink} style={styles.bold}>{b.weakest.name}</Text>，平均 {fmtScore(b.weakest.avg_score)} / {fmtScore(b.weakest.avg_max)}
                  {b.weakest.method_title ? `（${b.weakest.method_title}）` : ''}
                </Text>
                <Button title="去学" kind="text" size="sm" color={colors.ink} onPress={() => router.push('/(tabs)/bank')} />
              </View>
            ) : null}
          </>
        ) : null}
        {b.items.length > 0 ? (
          <View style={styles.list}>
            {b.items.map((e) => (
              <Pressable
                key={e.id}
                accessibilityRole="button"
                accessibilityLabel={e.topic}
                onPress={() =>
                  e.status === 'draft' || e.status === 'failed'
                    ? router.push({ pathname: '/essay/write/[id]', params: { id: String(e.id) } })
                    : router.push({ pathname: '/essay/result/[id]', params: { id: String(e.id) } })
                }
                style={styles.item}
              >
                <View style={styles.flex}>
                  <Text variant="body" numberOfLines={1}>
                    {e.topic}
                  </Text>
                  <Text variant="small">
                    {day(e.graded_at ?? e.created_at)} · {sourceNames[e.topic_source]} · 第 {e.draft_no} 稿
                    {e.status === 'draft' ? ' · 草稿' : e.status === 'grading' ? ' · 批改中' : e.status === 'failed' ? ' · 批改失败' : ''}
                  </Text>
                </View>
                {e.score !== undefined ? <Text style={styles.score}>{fmtScore(e.score)}</Text> : null}
              </Pressable>
            ))}
          </View>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  gap: { gap: 12 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  tiny: { fontSize: 11 },
  stats: { flexDirection: 'row', marginTop: spacing.sm, marginBottom: 4 },
  stat: { flex: 1, gap: 2 },
  statNum: { fontFamily: fontFamily.numberSemiBold, fontSize: 26, lineHeight: 32, color: colors.ink },
  chart: { flexDirection: 'row', alignItems: 'flex-end', gap: spacing.sm, minHeight: 160 },
  col: { flex: 1, alignItems: 'center', gap: 6 },
  bar: { width: 28, borderRadius: 8, backgroundColor: '#DCD8CF' },
  barLast: { backgroundColor: colors.indigo },
  weak: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingLeft: 16, paddingRight: 6, minHeight: 48, borderRadius: radius.xl, backgroundColor: semantic.fill },
  list: { marginTop: 4 },
  item: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 60, borderTopWidth: 1, borderTopColor: semantic.border },
  score: { fontFamily: fontFamily.numberSemiBold, fontSize: 18, color: colors.ink },
});
