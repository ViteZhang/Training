// 2.1 今日首页：按服务端给的状态四选一，优先级：还没导入资料（2.1c）→ 题库整理中（2.1b）→ 今日已完成（2.1d）→ 正常（2.1）。
// 计划、阶段、主推、掌握分布都由服务端算好，这里只展示（CLAUDE.md 必须遵守第 3 条）。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Icon, Loading, ProgressBar, Screen, Tag, Text } from '@/components';
import { useUnreadCount } from '@/features/messages/api';
import { isRunning, useActiveImportJobs } from '@/features/import/api';
import { stageInfo } from '@/features/onboarding/api';
import { useHome, useTodaySummary } from '@/features/today/api';
import { BanksCard, DoneCard, EntryGrid, entryTones, EstimateCard, FalseMasteryCard, PlanCard, PushCard } from '@/features/today/Cards';
import { usePracticeHome } from '@/features/practice/api';
import { useStartRecite } from '@/features/recite/api';
import { StagePromptSheet, TargetSheet } from '@/features/today/Sheets';
import { api, unwrap } from '@/lib/api';

function Top({ days, stage }: { days?: number; stage?: keyof typeof stageInfo }) {
  const unread = useUnreadCount().data?.count ?? 0;
  return (
    <View style={styles.top}>
      <View style={styles.countdown}>
        <Text variant="score" color={colors.ink}>
          {days ?? '—'}
        </Text>
        <Text variant="caption">天后初试</Text>
      </View>
      <View style={styles.topRight}>
        {stage ? <Tag label={stageInfo[stage].name} tone="outline" size="md" /> : null}
        <Pressable accessibilityRole="button" accessibilityLabel={unread > 0 ? `消息，${unread} 条未读` : '消息'} onPress={() => router.push('/messages')} style={styles.bell}>
          <Icon name="bell" />
          {unread > 0 ? <View style={styles.badge} /> : null}
        </Pressable>
      </View>
    </View>
  );
}

/** 2.1c「不知道先导入什么」的一条：色点 + 加粗的资料类型 + 说明。 */
function Tip({ color, title, desc }: { color: string; title: string; desc: string }) {
  return (
    <View style={styles.tip}>
      <View style={[styles.dot, { backgroundColor: color }]} />
      <Text variant="caption" style={styles.flex}>
        <Text variant="caption" color={colors.ink} style={styles.bold}>
          {title}
        </Text>
        ：{desc}
      </Text>
    </View>
  );
}

/** 首页底部快捷入口（设计稿 2.1）：背诵、错题本、整卷、导入资料，数字取训练首页与整卷列表。 */
function Entries({ subjectId }: { subjectId?: number }) {
  const practice = usePracticeHome(subjectId);
  const papers = useQuery({
    queryKey: ['papers', subjectId ?? 0],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/papers', { params: { path: { subjectId: subjectId! } } })),
    enabled: !!subjectId,
  });
  const recite = useStartRecite();
  const p = practice.data;
  const real = papers.data?.real_exam ?? [];
  const sid = subjectId ? String(subjectId) : undefined;
  return (
    <EntryGrid
      items={[
        {
          icon: 'book',
          title: '背诵',
          desc: p?.recite_due !== undefined ? (p.recite_due > 0 ? `${p.recite_due} 条待背` : '今天没有到期的') : '挖空 · 默写',
          tone: entryTones.recite,
          onPress: () => subjectId && recite.mutate({ subject_id: subjectId }),
        },
        {
          icon: 'wrong',
          title: '错题本',
          desc: p?.wrong_book ? (p.wrong_book.total > 0 ? `${p.wrong_book.total} 题` : '还没有错题') : '按失分原因查看',
          tone: entryTones.wrong,
          onPress: () => router.push({ pathname: '/practice/wrong', params: { subjectId: sid } }),
        },
        {
          icon: 'paper',
          title: '整卷',
          desc: papers.data?.real_exam ? (real.length > 0 ? `已做 ${real.filter((x) => x.status === 'done').length} / ${real.length} 套` : '还没有真题卷') : '真题卷 · 模拟考试',
          tone: entryTones.paper,
          onPress: () => router.push({ pathname: '/paper/list', params: { subjectId: sid } }),
        },
        { icon: 'import', title: '导入资料', desc: '追加题目或资料', tone: entryTones.neutral, onPress: () => router.push('/import') },
      ]}
    />
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
          <Card style={styles.emptyCard}>
            <View style={styles.emptyIcon}>
              <Icon name="import" size={26} />
            </View>
            <Text variant="h3">导入第一份资料，开始今天的训练</Text>
            <Text variant="caption" style={styles.center}>
              真题汇编、习题册、讲义、笔记都可以，AI 会整理成能刷、能批改的题库
            </Text>
            <Button title="导入资料" block style={styles.emptyBtn} onPress={() => router.push('/import')} />
          </Card>
          <Card tone="fill" style={styles.tips}>
            <Text variant="bodyStrong" style={styles.bold}>
              不知道先导入什么？
            </Text>
            <Tip color={colors.green} title="历年真题卷最合适" desc="有题有答案，导入就能刷；做完一套整卷还能算出预估分" />
            <Tip color={colors.blue} title="只有讲义或笔记" desc="AI 会拆出知识点和采分点，再按知识点出题" />
            <Tip color="#8C80E0" title="纸质习题册" desc="直接拍照，一张拍一页" />
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
          <View style={styles.dashed}>
            <Text variant="bodyStrong" style={styles.bold}>
              专业课预估分
            </Text>
            <Text variant="caption">题库整理好后，做完一套导入的真题卷就能估分</Text>
          </View>
          <Card tone="fill" style={styles.card}>
            <View style={styles.dots} accessibilityElementsHidden>
              <View style={[styles.dot, { backgroundColor: colors.ink }]} />
              <View style={[styles.dot, { backgroundColor: '#8C877D' }]} />
              <View style={[styles.dot, { backgroundColor: '#CFCAC0' }]} />
            </View>
            <Text variant="h3">{!j || isRunning(j) ? '正在整理你的题库' : '题库整理好了，等你核对'}</Text>
            {j ? <ProgressBar value={j.materials.length ? done / j.materials.length : 0} height={4} color={colors.ink} /> : null}
            {j ? (
              <Text variant="caption" style={styles.lh}>
                {j.materials.length} 份资料已完成 {done} 份，识别出 <Text variant="caption" color={colors.ink}>{recognized}</Text> 条。
                {isRunning(j) ? '全部整理好后会重新排今天的计划，并通知你核对。' : ''}
              </Text>
            ) : null}
            {jobId && j && !isRunning(j) ? (
              <Button title="去核对" onPress={() => router.push({ pathname: '/import/confirm/[id]', params: { id: String(jobId) } })} />
            ) : null}
          </Card>
          {h.plan && h.plan.items.length > 0 ? <PlanCard plan={h.plan} early /> : null}
          <BanksCard banks={h.banks} jobId={jobId} />
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
          <View style={styles.notice}>
            <View style={[styles.dot, { backgroundColor: colors.blue }]} />
            <Text variant="small" color={colors.ink} style={styles.flex}>
              还有不少知识点没学过，冲刺期计划里保留了一部分新知识点，先补上再查漏
            </Text>
          </View>
        ) : null}
        <EstimateCard subjects={list} estimates={h.estimates} onEditTarget={() => setEditTarget(true)} />
        {h.state === 'done' ? <DoneCard home={h} summary={summary.data} /> : h.plan ? <PlanCard plan={h.plan} /> : null}
        {h.state !== 'done' && h.stage_push ? <PushCard push={h.stage_push} /> : null}
        <BanksCard banks={h.banks} />
        <FalseMasteryCard count={h.false_mastery_count} />
        <Entries subjectId={list.find((x) => !x.is_essay)?.id} />
      </ScrollView>
      {prompt}
      {target}
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xxl, gap: spacing.md },
  top: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.sm },
  countdown: { flexDirection: 'row', alignItems: 'baseline', gap: 6 },
  topRight: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  center: { textAlign: 'center', lineHeight: 21 },
  lh: { lineHeight: 21 },
  bell: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center', marginRight: -10 },
  badge: { position: 'absolute', top: 10, right: 11, width: 8, height: 8, borderRadius: 4, backgroundColor: semantic.danger, borderWidth: 2, borderColor: semantic.background },
  card: { gap: 10 },
  emptyCard: { alignItems: 'center', gap: 10, paddingTop: 26 },
  emptyIcon: { width: 56, height: 56, borderRadius: 18, backgroundColor: semantic.fill, alignItems: 'center', justifyContent: 'center' },
  emptyBtn: { marginTop: spacing.sm },
  tips: { gap: 10 },
  tip: { flexDirection: 'row', gap: 10 },
  dot: { width: 8, height: 8, borderRadius: 4, marginTop: 6 },
  dots: { flexDirection: 'row', gap: 6 },
  dashed: { gap: 4, paddingVertical: 16, paddingHorizontal: 18, borderRadius: radius.card, borderWidth: 1, borderStyle: 'dashed', borderColor: '#D6D2C8' },
  notice: { flexDirection: 'row', gap: 10, padding: 14, borderRadius: radius.xl, backgroundColor: semantic.infoSoft },
});
