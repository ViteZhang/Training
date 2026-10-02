// 4.11 本组训练总结：一句总结（只描述数据）、完成题数、正确率、用时、按题型得分、来自题库与 AI 出题的题数、需要再看的知识点（最多 3 个）。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { practiceKeys, useSession } from '@/features/practice/api';
import { Stat } from '@/features/today/Cards';
import { api, unwrap } from '@/lib/api';

export default function PracticeSummaryPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const sessionId = Number(id);
  const session = useSession(sessionId);
  const q = useQuery({
    queryKey: practiceKeys.summary(sessionId),
    queryFn: () => unwrap(api.POST('/practice-sessions/{sessionId}/finish', { params: { path: { sessionId } } })),
    staleTime: Infinity,
  });
  if (q.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const s = q.data;
  const subjectId = session.data?.subject_id;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="h1">这组练完了</Text>
        <Text variant="body" color={semantic.textSecondary}>
          {s.comment}
        </Text>
        <View style={styles.stats}>
          <Stat value={s.answered} unit="完成题数" />
          <Stat value={`${Math.round(s.correct_rate * 100)}%`} unit="正确率" />
          <Stat value={Math.round(s.minutes)} unit="分钟" />
        </View>
        {s.by_qtype.length > 0 ? (
          <Card style={styles.card}>
            {s.by_qtype.map((t) => (
              <View key={t.qtype} style={styles.row}>
                <Text variant="body" style={styles.flex}>
                  {qtypeNames[t.qtype]}
                </Text>
                <Text variant="bodyStrong">
                  {t.correct}/{t.total}
                </Text>
              </View>
            ))}
          </Card>
        ) : null}
        <Text variant="caption">
          {s.from_bank} 道来自你的题库{s.from_ai > 0 ? `，${s.from_ai} 道是 AI 按你的知识点出的` : ''}
        </Text>
        {s.review_kps.length > 0 ? (
          <Card style={styles.card}>
            <Text variant="bodyStrong">需要再看看的知识点</Text>
            {s.review_kps.map((k) => (
              <Button key={k.kp_id} title={`${k.name} · ${k.reason} ›`} kind="text" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.kp_id) } })} />
            ))}
          </Card>
        ) : null}
        {subjectId ? <Button title="查看错题" kind="secondary" onPress={() => router.replace({ pathname: '/practice/wrong', params: { subjectId: String(subjectId) } })} /> : null}
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
