// 2.2 今日训练完成：连续打卡、题数、正确率、用时、预估分变化、掌握度变化、主观题失分归因。
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { BackButton, Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { MasteryPill } from '@/features/bank/MasteryPill';
import { lossNames, minutes, useHome, useTodaySummary } from '@/features/today/api';

function Tile({ value, unit, label }: { value: number | string; unit?: string; label: string }) {
  return (
    <View style={styles.tile}>
      <Text style={styles.tileNum}>
        {value}
        {unit ? <Text variant="caption" color={colors.ink}>{unit}</Text> : null}
      </Text>
      <Text variant="small">{label}</Text>
    </View>
  );
}

const lossColors = [colors.amber, colors.blue, '#8C80E0'];

export default function TodaySummaryPage() {
  const q = useTodaySummary();
  const home = useHome();
  if (q.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const s = q.data;
  const losses = (Object.keys(lossNames) as (keyof typeof lossNames)[]).filter((k) => (s.loss_shares[k] ?? 0) > 0);
  const changed = (home.data?.estimates ?? []).filter((e) => e.ready && e.today_change);
  return (
    <Screen>
      <View style={styles.close}>
        <BackButton icon="close" label="关闭" onPress={() => router.replace('/(tabs)/today')} />
      </View>
      <ScrollView contentContainerStyle={styles.scroll}>
        <View style={styles.gap4}>
          <Text variant="h1">今日训练完成</Text>
          <Text variant="caption">
            已连续打卡 <Text variant="caption" color={colors.ink} style={styles.bold}>{s.streak_days}</Text> 天
          </Text>
        </View>
        <View style={styles.tiles}>
          <Tile value={s.question_count} label="道题" />
          <Tile value={Math.round(s.correct_rate * 100)} unit="%" label="正确率" />
          <Tile value={minutes(s.minutes)} label="分钟" />
        </View>

        {changed.map((e) => (
          <Card key={e.subject_id} style={styles.estimate}>
            <View style={styles.flex}>
              <Text variant="small">{e.name} 预估分</Text>
              <Text style={styles.range}>
                {e.low}–{e.high}
              </Text>
              {e.main_gap_dimension ? <Text variant="small">{e.main_gap_dimension}</Text> : null}
            </View>
            <Text style={[styles.change, { color: (e.today_change ?? 0) > 0 ? colors.green : colors.red }]}>
              {(e.today_change ?? 0) > 0 ? `+${e.today_change}` : e.today_change}
            </Text>
          </Card>
        ))}

        {s.mastery_changes.length === 0 && s.new_mastered > 0 ? (
          <Text variant="caption" color={colors.green}>
            新增 {s.new_mastered} 个已掌握
          </Text>
        ) : null}
        {s.mastery_changes.length > 0 ? (
          <View style={styles.section}>
            <View style={styles.row}>
              <Text variant="h3" style={styles.flex}>
                掌握度变化
              </Text>
              <Text variant="caption">共 {s.mastery_changes.length} 个</Text>
            </View>
            <Card style={styles.list}>
              {s.mastery_changes.slice(0, 5).map((c, i) => (
                <View key={c.kp_id} style={[styles.change_row, i > 0 && styles.divider]}>
                  <Text variant="body" style={styles.flex} numberOfLines={1}>
                    {c.name}
                  </Text>
                  <MasteryPill state={c.from} />
                  <Text variant="small">→</Text>
                  <MasteryPill state={c.to} />
                </View>
              ))}
            </Card>
          </View>
        ) : null}

        {losses.length > 0 ? (
          <View style={styles.section}>
            <Text variant="h3">主观题失分</Text>
            <Card tone="fill" style={styles.gap8}>
              <View style={styles.lossBar}>
                {losses.map((k, i) => (
                  <View key={k} style={{ flex: s.loss_shares[k] ?? 0, backgroundColor: lossColors[i] }} />
                ))}
              </View>
              <View style={styles.lossLegend}>
                {losses.map((k) => (
                  <Text key={k} variant="small">
                    {lossNames[k]} <Text variant="small" color={colors.ink}>{Math.round((s.loss_shares[k] ?? 0) * 100)}%</Text>
                  </Text>
                ))}
              </View>
            </Card>
          </View>
        ) : null}
      </ScrollView>
      <View style={styles.footer}>
        <Button title="再练一组" kind="secondary" style={styles.flex} onPress={() => router.replace('/(tabs)/train')} />
        <Button title="回到今日" style={styles.flex2} onPress={() => router.replace('/(tabs)/today')} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  close: { alignItems: 'flex-end', marginRight: -12 },
  scroll: { gap: spacing.xl, paddingTop: spacing.sm, paddingBottom: spacing.xl },
  gap4: { gap: 4 },
  gap8: { gap: 8 },
  bold: { fontWeight: '700' },
  tiles: { flexDirection: 'row', gap: 8 },
  tile: { flex: 1, gap: 2, paddingVertical: 14, paddingHorizontal: 14, borderRadius: radius.xl, backgroundColor: semantic.fill },
  tileNum: { fontFamily: fontFamily.numberSemiBold, fontSize: 28, lineHeight: 34, color: colors.ink },
  estimate: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  range: { fontFamily: fontFamily.numberSemiBold, fontSize: 22, lineHeight: 28, color: colors.indigo },
  change: { fontFamily: fontFamily.numberSemiBold, fontSize: 20 },
  section: { gap: 10 },
  row: { flexDirection: 'row', alignItems: 'center' },
  flex: { flex: 1 },
  flex2: { flex: 1.6 },
  list: { paddingVertical: 0 },
  change_row: { flexDirection: 'row', alignItems: 'center', gap: 6, minHeight: 52 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  lossBar: { flexDirection: 'row', gap: 2, height: 8, borderRadius: 4, overflow: 'hidden' },
  lossLegend: { flexDirection: 'row', justifyContent: 'space-between', flexWrap: 'wrap', gap: 8 },
  footer: { flexDirection: 'row', gap: 10, paddingVertical: spacing.md },
});
