// 2.1 今日首页。本卡（T12）做两种状态：2.1b 题库整理中、2.1c 还没导入资料；正常状态与今日已完成在 T16。
// 首页按状态四选一，优先级：还没导入资料 → 题库整理中 → 今日已完成 → 正常（PRD 模块 2）。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, ProgressBar, Screen, Tag, Text } from '@/components';
import { isRunning, useActiveImportJobs } from '@/features/import/api';
import { stageInfo } from '@/features/onboarding/api';
import { api, unwrap } from '@/lib/api';

function Top({ days, stage }: { days?: number; stage?: keyof typeof stageInfo }) {
  return (
    <View style={styles.top}>
      <Text variant="score">{days ?? '—'}</Text>
      <Text variant="body">天后初试</Text>
      {stage ? <Tag label={stageInfo[stage].name} tone="brand" /> : null}
    </View>
  );
}

export default function TodayTab() {
  const profile = useQuery({ queryKey: ['profile'], queryFn: () => unwrap(api.GET('/profile')) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const jobs = useActiveImportJobs();

  if (subjects.isLoading || jobs.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (subjects.isError || jobs.isError) {
    return <Screen><ErrorState error={subjects.error ?? jobs.error} onRetry={() => void Promise.all([subjects.refetch(), jobs.refetch()])} /></Screen>;
  }
  const list = subjects.data?.items ?? [];
  const active = jobs.data?.items ?? [];
  const hasContent = list.some((s) => s.question_count > 0 || s.kp_count > 0);
  const top = <Top days={profile.data?.days_to_exam} stage={profile.data?.stage} />;

  // 2.1c 还没导入资料
  if (!hasContent && active.length === 0) {
    return (
      <Screen>
        <ScrollView contentContainerStyle={styles.scroll}>
          {top}
          <Card style={styles.card}>
            <Text variant="h2">导入第一份资料，开始今天的训练</Text>
            <Text variant="body" color={semantic.textSecondary}>
              真题汇编、习题册、讲义、笔记都可以，AI 会整理成能刷、能批改的题库
            </Text>
            <Button title="导入资料" onPress={() => router.push('/import')} />
          </Card>
          <Card style={styles.card}>
            <Text variant="bodyStrong">不知道先导入什么？</Text>
            <Text variant="caption">· 历年真题卷最合适：有题有答案，导入就能刷；做完一套整卷还能算出预估分</Text>
            <Text variant="caption">· 只有讲义或笔记：AI 会拆出知识点和采分点，再按知识点出题</Text>
            <Text variant="caption">· 纸质习题册：直接拍照，一张拍一页</Text>
          </Card>
          <SubjectList subjects={list} importing={[]} />
        </ScrollView>
      </Screen>
    );
  }

  // 2.1b 题库整理中
  const running = active.filter(isRunning);
  const reviewing = active.filter((j) => j.status === 'reviewing');
  if (running.length > 0 || reviewing.length > 0) {
    const j = running[0] ?? reviewing[0]!;
    const done = j.materials.filter((m) => m.status !== 'pending' && m.status !== 'running').length;
    const recognized = j.counts.questions + j.counts.knowledge_points + (j.counts.essay_items ?? 0);
    const importing = active.map((x) => x.subject_id);
    return (
      <Screen>
        <ScrollView contentContainerStyle={styles.scroll}>
          {top}
          <Card style={styles.card}>
            <Text variant="bodyStrong">专业课预估分</Text>
            <Text variant="caption">题库整理好后，做完一套导入的真题卷就能估分</Text>
          </Card>
          <Card style={styles.card}>
            <Text variant="h3">{isRunning(j) ? '正在整理你的题库' : '题库整理好了，等你核对'}</Text>
            <ProgressBar value={j.materials.length ? done / j.materials.length : 0} />
            <Text variant="body" color={semantic.textSecondary}>
              {j.materials.length} 份资料已完成 {done} 份，识别出 {recognized} 条。
              {isRunning(j) ? '全部整理好后会重新排今天的计划，并通知你核对。' : ''}
            </Text>
            <Button
              title={isRunning(j) ? '看进度' : '去核对'}
              kind={isRunning(j) ? 'secondary' : 'primary'}
              onPress={() =>
                isRunning(j)
                  ? router.push({ pathname: '/import/job/[id]', params: { id: String(j.id) } })
                  : router.push({ pathname: '/import/confirm/[id]', params: { id: String(j.id) } })
              }
            />
          </Card>
          <SubjectList subjects={list} importing={importing} />
        </ScrollView>
      </Screen>
    );
  }

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        {top}
        <EmptyState title="今日计划开发中" desc="2.1 今日首页（T16）" />
        <SubjectList subjects={list} importing={[]} />
      </ScrollView>
    </Screen>
  );
}

/** 「我的题库」：每门课的题数；没导入的课给「导入」。 */
function SubjectList({ subjects, importing }: { subjects: { id: number; name: string; code?: string; question_count: number; kp_count: number; material_count: number }[]; importing: number[] }) {
  return (
    <Card style={styles.card}>
      <Text variant="bodyStrong">我的题库</Text>
      {subjects.map((s) => {
        const empty = s.material_count === 0 && s.question_count === 0;
        return (
          <View key={s.id} style={styles.subject}>
            <View style={styles.flex}>
              <Text variant="body">
                {s.code ? `${s.code} ` : ''}
                {s.name}
              </Text>
              <Text variant="caption">
                {importing.includes(s.id) ? '整理中' : empty ? '还没导入资料' : `${s.question_count} 道题 · ${s.kp_count} 个知识点`}
              </Text>
            </View>
            {empty && !importing.includes(s.id) ? (
              <Button title="导入" kind="text" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(s.id) } })} />
            ) : null}
          </View>
        );
      })}
    </Card>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  top: { flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm, marginTop: spacing.lg },
  card: { gap: spacing.sm },
  subject: { flexDirection: 'row', alignItems: 'center', minHeight: 48 },
  flex: { flex: 1 },
});
