// 3.8 考情分析：标「AI 统计」并写明依据（哪份资料、哪些年份、几套）；题型结构与建议用时；板块分值占比与「你的掌握度」对照；
// 缺资料提醒；高频考点（可「刷一组高频考点」）；出题风格标签；回忆版等不完整的题不计入并注明数量（PRD 11.11）。
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { MasteryPill } from '@/features/bank/MasteryPill';
import { qtypeNames } from '@/features/import/api';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

const pct = (v: number) => `${Math.round(v * 100)}%`;

export default function ExamProfileScreen() {
  const subjectId = Number(useLocalSearchParams<{ subjectId: string }>().subjectId);
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const subject = subjects.data?.items.find((s) => s.id === subjectId);
  const q = useQuery({
    queryKey: ['bank', subjectId, 'exam-profile'],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/exam-profile', { params: { path: { subjectId } } })),
  });
  if (q.isLoading) return <Screen><Loading rows={8} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const p = q.data;
  const title = subject ? `${subject.code ? `${subject.code} ` : ''}${subject.name}怎么考` : '考情分析';

  if (!p.ready) {
    return (
      <Screen>
        <PageHeader title="考情分析" onBack={() => router.back()} />
        <EmptyState
          title={p.paper_count === 0 ? '还没有导入真题' : `目前只有 ${p.paper_count} 套真题`}
          desc={`至少 ${p.min_papers} 套不同年份的真题卷才能统计这门课怎么考。导入更多年份的真题后会自动更新`}
          actionText="导入真题"
          onAction={() => router.push({ pathname: '/import', params: { subjectId: String(subjectId) } })}
        />
      </Screen>
    );
  }
  const years = [...p.years].sort((a, b) => a - b);
  const span = years.length ? `${years[0]}–${years[years.length - 1]}` : '';
  const used = p.structure.reduce((s, x) => s + x.suggested_minutes, 0);
  const qColors = [colors.blue, '#8C80E0', colors.amber, colors.green, colors.indigo];
  const totalScore = p.structure.reduce((n, x) => n + x.total, 0) || 1;
  return (
    <Screen>
      <PageHeader title="考情分析" onBack={() => router.back()} />
      <ScrollView contentContainerStyle={styles.scroll}>
        <View style={styles.gap6}>
          <Text variant="h2">{title}</Text>
          <View style={styles.row}>
            <Tag label="AI 统计" tone="mastered" />
            <Text variant="small" style={styles.flex}>
              依据：{p.basis.map((b) => b.file_name).join('、')} 中 {span} 年 {p.paper_count} 套
            </Text>
          </View>
        </View>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="caption" color={colors.ink} style={[styles.flex, styles.bold]}>
              题型结构与建议用时
            </Text>
            <Text variant="small">{p.changed_years?.length ? `${p.changed_years.join('、')} 年有变化` : `近 ${p.stable_years ?? 0} 年未变`}</Text>
          </View>
          <View style={styles.stack}>
            {p.structure.map((x, i) => (
              <View key={x.qtype} style={{ flex: x.total / totalScore, backgroundColor: qColors[i % qColors.length] }} />
            ))}
          </View>
          {p.structure.map((x, i) => (
            <View key={x.qtype} style={styles.structRow}>
              <View style={[styles.square, { backgroundColor: qColors[i % qColors.length] }]} />
              <Text variant="caption" color={colors.ink} style={styles.qname}>
                {qtypeNames[x.qtype]}
              </Text>
              <Text variant="small" style={styles.flex}>
                {x.count} 题 × {x.score_each} 分
              </Text>
              <Text variant="caption" color={colors.ink} style={[styles.num, styles.bold]}>
                {x.total} 分
              </Text>
              <Text variant="small" style={styles.num}>
                {x.suggested_minutes}′
              </Text>
            </View>
          ))}
          <Text variant="small">
            建议用时按分值折算，另留 {p.check_minutes} 分钟检查 · 合计 {used + (p.check_minutes ?? 0)} 分钟
          </Text>
        </Card>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="caption" color={colors.ink} style={[styles.flex, styles.bold]}>
              板块分值占比
            </Text>
            <Text variant="small">你的掌握度</Text>
          </View>
          {p.sections.map((x) => (
            <View key={x.id} style={styles.secRow}>
              <Text variant="small" color={colors.ink} style={styles.secName} numberOfLines={1}>
                {x.name}
              </Text>
              <View style={styles.bar}>
                <View style={[styles.barFill, { width: `${Math.min(1, x.share) * 100}%` }]} />
              </View>
              <Text variant="small" color={colors.ink} style={[styles.pct, styles.bold]}>
                {pct(x.share)}
              </Text>
              <Text variant="small" color={x.mastery < 40 ? weakInk : undefined} style={styles.pct}>
                {Math.round(x.mastery)}%
              </Text>
            </View>
          ))}
          {p.missing_sections.map((x) => (
            <View key={x.id} style={styles.missing}>
              <View style={styles.dot} />
              <Text variant="small" color="#8A3A16" style={styles.flex}>
                {x.name}占 {pct(x.share)} 分值，你的资料里只有 {x.kp_count} 个知识点、掌握度 {Math.round(x.mastery)}%。可以补一份{x.name}的讲义或笔记
              </Text>
            </View>
          ))}
        </Card>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="caption" color={colors.ink} style={[styles.flex, styles.bold]}>
              高频考点
            </Text>
            <Text variant="small">真题考过 {p.high_freq_total ?? 0} 个</Text>
          </View>
          {p.high_freq.length === 0 ? <Text variant="small">还没有考过 2 次以上的知识点</Text> : null}
          {p.high_freq.map((k, i) => (
            <Pressable key={k.kp_id} accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.kp_id), subjectId: String(subjectId) } })} style={[styles.row, styles.hf]}>
              <Text variant="caption" style={styles.rank}>
                {i + 1}
              </Text>
              <View style={styles.flex}>
                <Text variant="caption" color={colors.ink}>
                  {k.name}
                </Text>
                <Text variant="small">
                  {k.path.join(' · ')} · 考过 {k.exam_count} 次
                </Text>
              </View>
              <MasteryPill state={k.state} />
            </Pressable>
          ))}
        </Card>

        {p.style_tags.length > 0 ? (
          <View style={styles.tags}>
            <Text variant="small">出题风格</Text>
            {p.style_tags.map((t) => (
              <View key={t} style={styles.styleTag}>
                <Text variant="small" color={colors.ink}>
                  {t}
                </Text>
              </View>
            ))}
          </View>
        ) : null}

        <Button title="刷一组高频考点" disabled={p.high_freq.length === 0} onPress={() => toast('专项练习在训练模块上线后开放')} />
        <Text variant="small" style={styles.center}>
          只统计你导入的真题{p.excluded_count > 0 ? `，另有 ${p.excluded_count} 道回忆版或缺分值的题目不完整，未计入` : ''}。导入更多年份后会自动更新
        </Text>
      </ScrollView>
    </Screen>
  );
}

const weakInk = '#9A4A1C';

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: 12 },
  gap: { gap: 10 },
  gap6: { gap: 6, marginTop: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  center: { textAlign: 'center' },
  stack: { flexDirection: 'row', gap: 2, height: 8, borderRadius: 4, overflow: 'hidden' },
  structRow: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 36 },
  square: { width: 9, height: 9, borderRadius: 2 },
  qname: { width: 72 },
  num: { minWidth: 40, textAlign: 'right' },
  secRow: { flexDirection: 'row', alignItems: 'center', gap: 8, minHeight: 26 },
  secName: { width: 96 },
  bar: { flex: 1, height: 6, borderRadius: 3, backgroundColor: semantic.border, overflow: 'hidden' },
  barFill: { height: 6, borderRadius: 3, backgroundColor: colors.indigo },
  pct: { width: 36, textAlign: 'right' },
  missing: { flexDirection: 'row', gap: 10, padding: 14, borderRadius: 14, backgroundColor: semantic.dangerSoft },
  dot: { width: 7, height: 7, borderRadius: 4, marginTop: 5, backgroundColor: colors.amber },
  hf: { minHeight: 48, borderTopWidth: 1, borderTopColor: semantic.border, paddingTop: 6 },
  rank: { width: 16, fontFamily: fontFamily.numberSemiBold },
  tags: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 8 },
  styleTag: { paddingHorizontal: 10, paddingVertical: 5, borderRadius: radius.pill, backgroundColor: semantic.fill },
});
