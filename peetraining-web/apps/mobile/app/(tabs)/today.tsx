// 2.1 今日首页：按服务端给的状态四选一，优先级：还没导入资料（2.1c）→ 题库整理中（2.1b）→ 今日已完成（2.1d）→ 正常（2.1）。
// 计划、阶段、主推、掌握分布都由服务端算好，这里只展示（CLAUDE.md 必须遵守第 3 条）。
import { semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Icon, Loading, ProgressBar, Screen, Tag, Text } from '@/components';
import { useUnreadCount } from '@/features/messages/api';
import { isRunning, useActiveImportJobs } from '@/features/import/api';
import { stageInfo } from '@/features/onboarding/api';
import { useHome, useTodaySummary } from '@/features/today/api';
import { BanksCard, DoneCard, EstimateCard, FalseMasteryCard, PlanCard, PushCard } from '@/features/today/Cards';
import { StagePromptSheet, TargetSheet } from '@/features/today/Sheets';
import { api, unwrap } from '@/lib/api';

function Top({ days, stage }: { days?: number; stage?: keyof typeof stageInfo }) {
  const unread = useUnreadCount().data?.count ?? 0;
  return (
    <View style={styles.top}>
      <Text variant="score">{days ?? '—'}</Text>
      <Text variant="body">天后初试</Text>
      {stage ? <Tag label={stageInfo[stage].name} tone="brand" /> : null}
      <View style={styles.flex} />
      <Pressable accessibilityRole="button" accessibilityLabel={unread > 0 ? `消息，${unread} 条未读` : '消息'} onPress={() => router.push('/messages')} style={styles.bell}>
        <Icon name="bell" />
        {unread > 0 ? <View style={styles.badge} /> : null}
      </Pressable>
    </View>
  );
}

export default function TodayTab() {
  const home = useHome();
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const organizing = home.data?.state === 'organizing';
  const jobs = useActiveImportJobs();
  const summary = useTodaySummary();
  const [editTarget, setEditTarget] = useState(false);

  if (home.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (home.isError || !home.data) return <Screen><ErrorState error={home.error} onRetry={() => void home.refetch()} /></Screen>;
  const h = home.data;
  const list = subjects.data?.items ?? [];
  const top = <Top days={h.days_to_exam} stage={h.stage} />;
  const refresh = <RefreshControl refreshing={home.isRefetching} onRefresh={() => void home.refetch()} />;
  const prompt = h.stage_prompt ? <StagePromptSheet prompt={h.stage_prompt} current={h.stage} days={h.days_to_exam} /> : null;
  const target = editTarget ? <TargetSheet subjects={list} onClose={() => setEditTarget(false)} /> : null;

  // 2.1c 还没导入资料
  if (h.state === 'no_material') {
    return (
      <Screen>
        <ScrollView contentContainerStyle={styles.scroll} refreshControl={refresh}>
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
          <BanksCard banks={h.banks} />
        </ScrollView>
      </Screen>
    );
  }

  // 2.1b 题库整理中：先练已识别的题（有计划时照常显示）
  const active = jobs.data?.items ?? [];
  const j = organizing ? (active.find(isRunning) ?? active[0]) : undefined;
  if (organizing) {
    const done = j ? j.materials.filter((m) => m.status !== 'pending' && m.status !== 'running').length : 0;
    const recognized = j ? j.counts.questions + j.counts.knowledge_points + (j.counts.essay_items ?? 0) : 0;
    const jobId = j?.id ?? h.organizing_job_id;
    return (
      <Screen>
        <ScrollView contentContainerStyle={styles.scroll} refreshControl={refresh}>
          {top}
          <EstimateCard subjects={list} estimates={h.estimates} organizing onEditTarget={() => setEditTarget(true)} />
          <Card style={styles.card}>
            <Text variant="h3">{!j || isRunning(j) ? '正在整理你的题库' : '题库整理好了，等你核对'}</Text>
            {j ? <ProgressBar value={j.materials.length ? done / j.materials.length : 0} /> : null}
            {j ? (
              <Text variant="body" color={semantic.textSecondary}>
                {j.materials.length} 份资料已完成 {done} 份，识别出 {recognized} 条。
                {isRunning(j) ? '全部整理好后会重新排今天的计划，并通知你核对。' : ''}
              </Text>
            ) : null}
            {jobId ? (
              <Button
                title={!j || isRunning(j) ? '看进度' : '去核对'}
                kind={!j || isRunning(j) ? 'secondary' : 'primary'}
                onPress={() =>
                  !j || isRunning(j)
                    ? router.push({ pathname: '/import/job/[id]', params: { id: String(jobId) } })
                    : router.push({ pathname: '/import/confirm/[id]', params: { id: String(jobId) } })
                }
              />
            ) : null}
          </Card>
          {h.plan && h.plan.items.length > 0 ? <PlanCard plan={h.plan} /> : null}
          <BanksCard banks={h.banks} />
        </ScrollView>
        {target}
      </Screen>
    );
  }

  // 2.1d 今日已完成 / 2.1 正常
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} refreshControl={refresh}>
        {top}
        {h.low_coverage ? (
          <Card style={styles.card}>
            <Text variant="caption" color={semantic.info}>
              还有不少知识点没学过，冲刺期计划里保留了一部分新知识点，先补上再查漏
            </Text>
          </Card>
        ) : null}
        <EstimateCard subjects={list} estimates={h.estimates} onEditTarget={() => setEditTarget(true)} />
        {h.state === 'done' ? <DoneCard home={h} summary={summary.data} /> : h.plan ? <PlanCard plan={h.plan} /> : null}
        {h.state !== 'done' && h.stage_push ? <PushCard push={h.stage_push} /> : null}
        <BanksCard banks={h.banks} />
        <FalseMasteryCard count={h.false_mastery_count} />
        <Button title="导入资料 · 追加题目或资料" kind="text" onPress={() => router.push('/import')} />
      </ScrollView>
      {prompt}
      {target}
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  top: { flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm, marginTop: spacing.lg },
  flex: { flex: 1 },
  bell: { alignSelf: 'center', width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  badge: { position: 'absolute', top: 10, right: 10, width: 8, height: 8, borderRadius: 4, backgroundColor: semantic.danger },
  card: { gap: spacing.sm },
});
