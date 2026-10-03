// 4.17 背诵完成：本轮记住、模糊、没记住数量，与上一轮对比；下次复习安排（明天 / 2 天后 / 3–7 天后）；「再背没记住的」「完成」。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { reciteKeys, useStartRecite } from '@/features/recite/api';
import { Stat } from '@/features/today/Cards';
import { api, unwrap } from '@/lib/api';

export default function ReciteDonePage() {
  const p = useLocalSearchParams<{ id: string; subjectId: string }>();
  const sessionId = Number(p.id);
  const retry = useStartRecite(true);
  const q = useQuery({
    queryKey: reciteKeys.summary(sessionId),
    queryFn: () => unwrap(api.POST('/recite-sessions/{sessionId}/finish', { params: { path: { sessionId } } })),
    staleTime: Infinity,
  });
  if (q.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const s = q.data;
  const total = s.remembered + s.vague + s.forgot;
  const diff = s.previous_remembered !== undefined ? s.remembered - s.previous_remembered : undefined;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="h1">这轮背完了</Text>
        <Text variant="body" color={semantic.textSecondary}>
          {total} 条里记住了 {s.remembered} 条
          {diff !== undefined ? (diff > 0 ? `，比上一轮多 ${diff} 条` : diff < 0 ? `，比上一轮少 ${-diff} 条` : '，和上一轮一样') : ''}
        </Text>
        <View style={styles.stats}>
          <Stat value={s.remembered} unit="记住了" />
          <Stat value={s.vague} unit="模糊" />
          <Stat value={s.forgot} unit="没记住" />
        </View>
        <Card style={styles.card}>
          <Text variant="bodyStrong">下次复习安排</Text>
          {(
            [
              ['明天（没记住）', s.schedule.tomorrow],
              ['2 天后（模糊）', s.schedule.two_days],
              ['3–7 天后（记住了）', s.schedule.later],
            ] as const
          ).map(([label, n]) => (
            <View key={label} style={styles.row}>
              <Text variant="body" style={styles.flex}>
                {label}
              </Text>
              <Text variant="bodyStrong">{n} 条</Text>
            </View>
          ))}
          <Text variant="caption">按遗忘规律安排：越熟悉的内容，间隔越长。到期后会自动出现在今日训练里。</Text>
        </Card>
        {s.forgot_count > 0 ? (
          <Button title="再背没记住的" kind="secondary" loading={retry.isPending} onPress={() => retry.mutate({ subject_id: Number(p.subjectId), retry_of: sessionId })} />
        ) : null}
        <Button title="完成" onPress={() => router.replace('/(tabs)/train')} />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingTop: spacing.xl, paddingBottom: spacing.xl },
  stats: { flexDirection: 'row', gap: spacing.sm },
  card: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', minHeight: 32 },
  flex: { flex: 1 },
});
