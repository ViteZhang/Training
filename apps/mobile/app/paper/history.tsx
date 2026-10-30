// 逐次整卷历史：按 session 回到原作答、批改进度和报告。
import { router, useLocalSearchParams } from 'expo-router';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { semantic, spacing } from '@training/ui-tokens';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { modeNames, usePaperHistory } from '@/features/paper/api';

const statusNames = { in_progress: '进行中', paused: '已暂停', grading: '批改中', graded: '已完成', abandoned: '已放弃' };
const kindNames = { real_exam: '历年真题', ai_standard: 'AI 标准卷', ai_targeted: 'AI 针对卷', official_mock: '模拟卷' };
export default function PaperHistoryPage() {
  const subjectId = Number(useLocalSearchParams<{ subjectId: string }>().subjectId);
  const q = usePaperHistory(subjectId);
  const items = q.data?.pages.flatMap((p) => p.items) ?? [];
  return <Screen>
    <PageHeader title="历史答卷" onBack={() => router.back()} />
    {q.isLoading ? <Loading rows={5} /> : !q.data ? <ErrorState error={q.error} onRetry={() => void q.refetch()} /> :
    <ScrollView contentContainerStyle={styles.scroll}>
      {q.isError ? <View><Text variant="small">历史记录暂时刷新失败，已加载的答卷仍可查看。</Text><Button title="重试" kind="text" onPress={() => void q.refetch()} /></View> : null}
      {!items.length ? <EmptyState title="还没有整卷作答记录" desc="开始整卷作答后，每次记录都会保存在这里" /> : null}
      {items.map((it) => <Card key={it.id}>
        <Pressable accessibilityRole="button" accessibilityLabel={`${it.title}，${statusNames[it.status]}`} disabled={it.status === 'abandoned'} onPress={() => router.push({ pathname: '/paper/session/[id]', params: { id: String(it.id) } })} style={styles.row}>
          <Text variant="bodyStrong">{it.title}</Text>
          <Text variant="small">{new Date(it.submitted_at ?? it.started_at).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai' })}</Text>
          <Text variant="small">{modeNames[it.mode]} · {kindNames[it.kind]} · {statusNames[it.status]}</Text>
          <Text variant="caption">{it.status === 'graded' ? `${it.score ?? '-'} / ${it.full_score} 分 · 查看答卷及报告` : `已答 ${it.answered} / ${it.total}${it.status === 'grading' ? ' · 查看批改进度' : ''}`}</Text>
        </Pressable>
      </Card>)}
      {q.hasNextPage ? <Button title="加载更多" kind="secondary" loading={q.isFetchingNextPage} onPress={() => void q.fetchNextPage()} /> : null}
    </ScrollView>}
  </Screen>;
}
const styles = StyleSheet.create({ scroll: { gap: spacing.sm, paddingBottom: spacing.xl }, row: { gap: spacing.xs, minHeight: 44, backgroundColor: semantic.surface } });
