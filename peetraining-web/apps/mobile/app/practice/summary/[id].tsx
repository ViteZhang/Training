// 4.11 本组训练总结：一句总结（只描述数据）、完成题数、正确率、用时、按题型得分、来自题库与 AI 出题的题数、需要再看的知识点（最多 3 个）。
import { colors, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { BackButton, Button, Card, ErrorState, Icon, Loading, Screen, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { practiceKeys, useSession } from '@/features/practice/api';
import { Stat } from '@/features/today/Cards';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

export default function PracticeSummaryPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const sessionId = Number(id);
  const session = useSession(sessionId);
  const q = useQuery({
    queryKey: practiceKeys.summary(sessionId),
    queryFn: () => unwrap(api.POST('/practice-sessions/{sessionId}/finish', { params: { path: { sessionId } } })),
    staleTime: Infinity,
  });
  useEffect(() => {
    if (q.data) track('session_finish');
  }, [q.data]);
  if (q.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const s = q.data;
  const subjectId = session.data?.subject_id;
  return (
    <Screen>
      <View style={styles.close}>
        <BackButton icon="close" label="关闭" onPress={() => router.replace('/(tabs)/train')} />
      </View>
      <ScrollView contentContainerStyle={styles.scroll}>
        <View style={styles.gap4}>
          <Text variant="h1">这组练完了</Text>
          {s.comment ? <Text variant="caption">{s.comment}</Text> : null}
        </View>
        <Card>
          <View style={styles.stats}>
            <Stat value={s.answered} unit="完成题数" />
            <Stat value={`${Math.round(s.correct_rate * 100)}%`} unit="正确率" />
            <Stat value={Math.round(s.minutes)} unit="分钟" />
          </View>
        </Card>
        {s.by_qtype.length > 0 ? (
          <Card style={styles.card}>
            {s.by_qtype.map((t) => {
              const rate = t.total ? t.correct / t.total : 0;
              return (
                <View key={t.qtype} style={styles.row}>
                  <Text variant="small" color={semantic.textPrimary} style={styles.qname}>
                    {qtypeNames[t.qtype]}
                  </Text>
                  <View style={styles.bar}>
                    <View style={[styles.fill, { width: `${rate * 100}%`, backgroundColor: rate < 0.7 ? colors.amber : colors.indigo }]} />
                  </View>
                  <Text variant="small" style={styles.frac}>
                    {t.correct}/{t.total}
                  </Text>
                </View>
              );
            })}
            <Text variant="small">
              {s.from_bank} 道来自你的题库{s.from_ai > 0 ? `，${s.from_ai} 道是 AI 按你的知识点出的` : ''}
            </Text>
          </Card>
        ) : null}
        {s.review_kps.length > 0 ? (
          <View style={styles.card}>
            <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
              需要再看看的知识点
            </Text>
            {s.review_kps.map((k) => (
              <Pressable key={k.kp_id} accessibilityRole="button" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(k.kp_id) } })} style={styles.kp}>
                <View style={styles.flex}>
                  <Text variant="body">{k.name}</Text>
                  <Text variant="small">{k.reason}</Text>
                </View>
                <Icon name="chevron" size={16} color={semantic.textSecondary} />
              </Pressable>
            ))}
          </View>
        ) : null}
      </ScrollView>
      <View style={styles.footer}>
        {subjectId ? <Button title="查看错题" kind="secondary" style={styles.flex} onPress={() => router.replace({ pathname: '/practice/wrong', params: { subjectId: String(subjectId) } })} /> : null}
        <Button title="完成" style={styles.flex2} onPress={() => router.replace('/(tabs)/train')} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  close: { alignItems: 'flex-start' },
  scroll: { gap: 12, paddingTop: spacing.sm, paddingBottom: spacing.xl },
  gap4: { gap: 4, marginBottom: 6 },
  stats: { flexDirection: 'row', gap: spacing.sm },
  card: { gap: 10 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 24 },
  qname: { width: 64 },
  bar: { flex: 1, height: 6, borderRadius: 3, backgroundColor: semantic.border, overflow: 'hidden' },
  fill: { height: 6, borderRadius: 3 },
  frac: { width: 40, textAlign: 'right' },
  bold: { fontWeight: '700' },
  kp: { flexDirection: 'row', alignItems: 'center', minHeight: 52, borderTopWidth: 1, borderTopColor: semantic.border },
  flex: { flex: 1 },
  flex2: { flex: 1.6 },
  footer: { flexDirection: 'row', gap: 10, paddingVertical: spacing.md },
});
