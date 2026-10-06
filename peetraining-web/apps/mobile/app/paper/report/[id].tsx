// 4.24 整卷报告：总分（标「AI 批改得分 · 仅供参考」）、较上次变化、与目标差距；各题型得分；是否计入预估分；
// 失分归因三类的分值；时间分析入口与一句结论（只有模拟考试）；「逐题查看」「针对失分练一组」。
import { ApiError } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Icon, Loading, Screen, Text } from '@/components';
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
        <PageHeader title={`${r.title} · ${modeNames[r.mode]}`} onBack={back} />
        <View style={styles.head}>
          <Text variant="small">AI 批改得分 · 仅供参考</Text>
          <View style={styles.headRow}>
            <View style={[styles.rowBase, styles.flex]}>
              <Text variant="score" color={colors.ink} style={styles.big}>
                {r.score}
              </Text>
              <Text variant="caption"> / {r.full_score}</Text>
            </View>
            <View style={styles.right}>
              {r.prev_delta !== undefined ? (
                <View style={[styles.delta, { backgroundColor: r.prev_delta >= 0 ? semantic.masteredSoft : semantic.dangerSoft }]}>
                  <Text variant="small" color={r.prev_delta >= 0 ? '#1F6B4A' : semantic.danger}>
                    较上次 {r.prev_delta >= 0 ? `+${r.prev_delta}` : r.prev_delta}
                  </Text>
                </View>
              ) : null}
              {r.target_score !== undefined ? (
                <Text variant="small">
                  目标 {r.target_score} ·{' '}
                  {r.gap ? (
                    <Text variant="small" color={semantic.danger} style={styles.bold}>
                      差 {r.gap}
                    </Text>
                  ) : (
                    '已达到'
                  )}
                </Text>
              ) : null}
            </View>
          </View>
        </View>
        <Card style={styles.gap}>
          {r.by_qtype.map((q) => {
            const rate = q.full ? q.got / q.full : 0;
            return (
              <View key={q.qtype} style={styles.barRow}>
                <Text variant="small" color={colors.ink} style={styles.qname}>
                  {qtypeNames[q.qtype]}
                </Text>
                <View style={styles.bar}>
                  <View style={[styles.fill, { width: `${rate * 100}%`, backgroundColor: q.qtype === r.weakest_qtype ? colors.amber : colors.indigo }]} />
                </View>
                <Text variant="small" style={styles.frac}>
                  {q.got}/{q.full}
                </Text>
              </View>
            );
          })}
          <Text variant="small">按你资料里的采分点批改 · {r.counts_for_estimate ? '已计入预估分' : 'AI 组卷的成绩只作参考，不计入预估分'}</Text>
        </Card>

        <Card style={styles.gap}>
          <Text variant="caption" color={colors.ink} style={styles.bold}>
            失分归因 · 共失 {r.loss_total} 分
          </Text>
          <View style={styles.stack}>
            {(['knowledge', 'norm', 'time'] as const).map((k, i) => (r.loss[k] > 0 ? <View key={k} style={{ flex: r.loss[k], backgroundColor: lossColor[i] }} /> : null))}
          </View>
          <View style={styles.legend}>
            {(['knowledge', 'norm', 'time'] as const).map((k, i) => (
              <Text key={k} variant="small" color={lossInk[i]}>
                {lossNames[k]} {r.loss[k]}
              </Text>
            ))}
          </View>
        </Card>

        {r.time ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="时间分析报告"
            onPress={() => router.push({ pathname: '/paper/time/[id]', params: { id: String(sid) } })}
            style={styles.timeEntry}
          >
            <Icon name="clock" size={20} color="#1F6B4A" />
            <View style={styles.flex}>
              <Text variant="caption" color="#1F6B4A" style={styles.bold}>
                时间分析报告
              </Text>
              <Text variant="small" color="#1F6B4A">
                {r.time.conclusion}
              </Text>
            </View>
            <Icon name="chevron" size={16} color="#1F6B4A" />
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
          style={styles.flex2}
          loading={start.isPending}
          onPress={() =>
            start.mutate({ subject_id: r.subject_id, kind: 'custom', config: { qtypes: weakest ? [weakest] : [], count: 10, only_unmastered: true, ai_fill: false } })
          }
        />
      </View>
    </Screen>
  );
}

const lossColor = [colors.amber, '#B26A00', colors.ink];
const lossInk = ['#8A4B12', '#9A5B00', colors.ink];

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  gap: { gap: 10 },
  flex: { flex: 1 },
  flex2: { flex: 1.6 },
  bold: { fontWeight: '700' },
  head: { gap: 2, marginTop: spacing.sm },
  headRow: { flexDirection: 'row', alignItems: 'flex-end' },
  rowBase: { flexDirection: 'row', alignItems: 'baseline' },
  big: { fontSize: 52, lineHeight: 58 },
  right: { alignItems: 'flex-end', gap: 4, paddingBottom: 8 },
  delta: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill },
  barRow: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 26 },
  qname: { width: 64 },
  bar: { flex: 1, height: 6, borderRadius: 3, backgroundColor: semantic.border, overflow: 'hidden' },
  fill: { height: 6, borderRadius: 3 },
  frac: { width: 44, textAlign: 'right' },
  stack: { flexDirection: 'row', gap: 2, height: 8, borderRadius: 4, overflow: 'hidden' },
  legend: { flexDirection: 'row', justifyContent: 'space-between', flexWrap: 'wrap' },
  timeEntry: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 64, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, backgroundColor: semantic.masteredSoft },
  nav: { flexDirection: 'row', gap: 10, paddingVertical: spacing.md },
});
