// 2.2 今日训练完成：连续打卡、题数、正确率、用时、新增已掌握、主观题失分归因。预估分变化在 T22 接通。
import { semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, ProgressBar, Screen, Text } from '@/components';
import { lossNames, minutes, useTodaySummary } from '@/features/today/api';
import { Stat } from '@/features/today/Cards';

export default function TodaySummaryPage() {
  const q = useTodaySummary();
  if (q.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const s = q.data;
  const losses = (Object.keys(lossNames) as (keyof typeof lossNames)[]).filter((k) => s.loss_shares[k] !== undefined);
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="h1">今日训练完成</Text>
        <Text variant="body">
          已连续打卡 <Text variant="number">{s.streak_days}</Text> 天
        </Text>
        <View style={styles.stats}>
          <Stat value={s.question_count} unit="道题" />
          <Stat value={`${Math.round(s.correct_rate * 100)}%`} unit="正确率" />
          <Stat value={minutes(s.minutes)} unit="分钟" />
        </View>
        {s.new_mastered > 0 ? (
          <Card style={styles.card}>
            <Text variant="bodyStrong">掌握度变化</Text>
            <Text variant="body" color={semantic.mastered}>
              新增 {s.new_mastered} 个已掌握
            </Text>
          </Card>
        ) : null}
        {losses.length > 0 ? (
          <Card style={styles.card}>
            <Text variant="bodyStrong">主观题失分</Text>
            {losses.map((k) => (
              <View key={k} style={styles.loss}>
                <View style={styles.row}>
                  <Text variant="body" style={styles.flex}>
                    {lossNames[k]}
                  </Text>
                  <Text variant="bodyStrong">{Math.round((s.loss_shares[k] ?? 0) * 100)}%</Text>
                </View>
                <ProgressBar value={s.loss_shares[k] ?? 0} />
              </View>
            ))}
          </Card>
        ) : null}
        <Button title="再练一组" onPress={() => router.replace('/(tabs)/train')} />
        <Button title="回到今日" kind="text" onPress={() => router.replace('/(tabs)/today')} />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingTop: spacing.xl, paddingBottom: spacing.xl },
  stats: { flexDirection: 'row', gap: spacing.sm },
  card: { gap: spacing.sm },
  loss: { gap: spacing.xs },
  row: { flexDirection: 'row', alignItems: 'center' },
  flex: { flex: 1 },
});
