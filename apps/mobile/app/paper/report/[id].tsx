// 4.24 整卷报告：总分（标「AI 批改得分 · 仅供参考」）、较上次变化、与目标差距；各题型得分；是否计入预估分；
// 失分归因三类的分值；时间分析入口与一句结论（只有模拟考试）；「逐题查看」「针对失分练一组」。
import { ApiError } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { PageHeader } from '@/features/import/ui';
import { lossNames, modeNames, usePaperReport } from '@/features/paper/api';
import { useStartPractice } from '@/features/practice/api';

export default function PaperReportPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const sid = Number(id);
  const report = usePaperReport(sid);
  const start = useStartPractice();
  const back = () => router.back();

  if (report.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (report.error instanceof ApiError && report.error.status === 409) {
    return (
      <Screen>
        <PageHeader title="整卷报告" onBack={back} />
        <Card style={styles.gap}>
          <Text variant="h3">正在整卷批改</Text>
          <Text variant="caption">按你资料里的采分点逐题批改，大约需要 2 分钟。批改完成后会发消息提醒你，可以先离开。</Text>
        </Card>
      </Screen>
    );
  }
  if (report.isError || !report.data) return <Screen><ErrorState error={report.error} onRetry={() => void report.refetch()} /></Screen>;
  const r = report.data;
  const weakest = r.weakest_qtype;

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="整卷报告" onBack={back} />
        <Text variant="caption">
          {r.title} · {modeNames[r.mode]}
        </Text>
        <Card style={styles.gap}>
          <Text variant="small">AI 批改得分 · 仅供参考</Text>
          <View style={styles.rowBase}>
            <Text variant="score">{r.score}</Text>
            <Text variant="caption"> / {r.full_score}</Text>
          </View>
          <View style={styles.row}>
            {r.prev_delta !== undefined ? (
              <Text variant="caption" color={r.prev_delta >= 0 ? semantic.mastered : semantic.danger}>
                较上次 {r.prev_delta >= 0 ? `+${r.prev_delta}` : r.prev_delta}
              </Text>
            ) : null}
            {r.target_score !== undefined ? (
              <Text variant="caption">
                目标 {r.target_score} · {r.gap ? `差 ${r.gap}` : '已达到'}
              </Text>
            ) : null}
          </View>
          <View style={styles.qtypes}>
            {r.by_qtype.map((q) => (
              <View key={q.qtype} style={styles.qtype}>
                <Text variant="caption">{qtypeNames[q.qtype]}</Text>
                <Text variant="bodyStrong">
                  {q.got}/{q.full}
                </Text>
              </View>
            ))}
          </View>
          <Text variant="small">
            按你资料里的采分点批改 · {r.counts_for_estimate ? '已计入预估分' : 'AI 组卷的成绩只作参考，不计入预估分'}
          </Text>
        </Card>

        <Card style={styles.gap}>
          <Text variant="h3">失分归因 · 共失 {r.loss_total} 分</Text>
          {(['knowledge', 'norm', 'time'] as const).map((k) => (
            <View key={k} style={styles.lossRow}>
              <Text variant="body" style={styles.flex}>
                {lossNames[k]}
              </Text>
              <Text variant="bodyStrong">{r.loss[k]}</Text>
            </View>
          ))}
        </Card>

        {r.time ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="时间分析报告"
            onPress={() => router.push({ pathname: '/paper/time/[id]', params: { id: String(sid) } })}
            style={styles.timeEntry}
          >
            <View style={styles.flex}>
              <Text variant="bodyStrong">时间分析报告</Text>
              <Text variant="caption">{r.time.conclusion}</Text>
            </View>
            <Text variant="bodyStrong" color={semantic.primary}>
              ›
            </Text>
          </Pressable>
        ) : null}
      </ScrollView>
      <View style={styles.nav}>
        <Button
          title="逐题查看"
          kind="secondary"
          style={styles.flex}
          onPress={() => router.push({ pathname: '/paper/session/[id]', params: { id: String(sid) } })}
        />
        <Button
          title="针对失分练一组"
          style={styles.flex}
          loading={start.isPending}
          onPress={() =>
            start.mutate({ subject_id: r.subject_id, kind: 'custom', config: { qtypes: weakest ? [weakest] : [], count: 10, only_unmastered: true, ai_fill: false } })
          }
        />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.md },
  rowBase: { flexDirection: 'row', alignItems: 'baseline' },
  qtypes: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm, marginTop: spacing.sm },
  qtype: { minWidth: 96, padding: spacing.sm, borderRadius: radius.md, backgroundColor: semantic.background, gap: 2 },
  lossRow: { flexDirection: 'row', alignItems: 'center', minHeight: 32, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  timeEntry: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 64, padding: spacing.lg, borderRadius: radius.lg, backgroundColor: semantic.surface, borderWidth: 1, borderColor: colors.line },
  nav: { flexDirection: 'row', gap: spacing.sm },
});
