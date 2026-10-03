// 6.1 我的：头像与昵称，专业课代码 + 阶段 + 距初试天数（→ 6.9）；会员条（免费版显示开通入口）；提分看板卡片（预估分、目标、失分归因占比）；
// 我的资料、错题本、作文本、导出题库、备考设置、邀请研友、兑换码、意见反馈；考后回访开放时显示入口。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { useDashboard } from '@/features/dashboard/api';
import { mineKeys, tierNames, useMe, useOverview, useProfile, useSubjects, ymd } from '@/features/mine/api';
import { stageInfo } from '@/features/onboarding/api';
import { appConfig } from '@/lib/config';
import { api, unwrap } from '@/lib/api';

function Row({ title, desc, onPress }: { title: string; desc?: string; onPress: () => void }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={title} onPress={onPress} style={styles.row}>
      <Text variant="body" style={styles.flex}>
        {title}
      </Text>
      {desc ? <Text variant="caption">{desc}</Text> : null}
      <Text variant="caption"> ›</Text>
    </Pressable>
  );
}

export default function MeTab() {
  const me = useMe();
  const profile = useProfile();
  const subjects = useSubjects();
  const overview = useOverview();
  const survey = useQuery({ queryKey: mineKeys.survey, queryFn: () => unwrap(api.GET('/me/survey')) });
  const list = subjects.data?.items ?? [];
  const main = list.find((s) => !s.is_essay) ?? list[0];
  const dash = useDashboard(main?.id ?? 0);

  if (me.isLoading || profile.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (me.isError || !me.data) return <Screen><ErrorState error={me.error} onRetry={() => void me.refetch()} /></Screen>;
  const m = me.data;
  const p = profile.data;
  const o = overview.data;
  const d = dash.data;
  const shares = d?.loss_shares;
  const lossTotal = shares ? shares.knowledge + shares.norm + shares.time : 0;
  const refresh = () => {
    void me.refetch();
    void overview.refetch();
    void dash.refetch();
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} refreshControl={<RefreshControl refreshing={me.isRefetching} onRefresh={refresh} />}>
        <Pressable accessibilityRole="button" onPress={() => router.push('/settings/prep')} style={styles.head}>
          <View style={styles.avatar}>
            <Text variant="h2" color={colors.white}>
              {(m.nickname || '研').slice(0, 1)}
            </Text>
          </View>
          <View style={styles.flex}>
            <Text variant="h2">{m.nickname || '考研人'}</Text>
            <Text variant="caption">
              {list.map((s) => s.code || s.name).join(' · ')}
              {p ? ` · ${stageInfo[p.stage].name} · ${p.days_to_exam} 天后初试` : ''}
            </Text>
          </View>
        </Pressable>

        <Pressable accessibilityRole="button" onPress={() => router.push('/member')} style={styles.member}>
          <View style={styles.flex}>
            <Text variant="bodyStrong" color={colors.white}>
              {m.membership.is_member ? (m.membership.tier ? tierNames[m.membership.tier] : '会员') : '免费版'}
            </Text>
            <Text variant="caption" color={colors.white}>
              {m.membership.is_member && m.membership.ends_at ? `有效期至 ${ymd(m.membership.ends_at)}` : '开通会员：批改、整卷、资料解析不限'}
            </Text>
          </View>
          {!m.membership.is_member ? (
            <Text variant="bodyStrong" color={colors.amber}>
              开通
            </Text>
          ) : null}
        </Pressable>

        {main ? (
          <Card style={styles.gap}>
            <View style={styles.rowInline}>
              <Text variant="h3" style={styles.flex}>
                提分看板
              </Text>
              <Button title="详情" kind="text" onPress={() => router.push({ pathname: '/dashboard', params: { subjectId: String(main.id) } })} />
            </View>
            {d?.estimate.ready ? (
              <Text variant="body">
                {main.code ?? main.name} 预估 <Text variant="number" color={colors.amber}>{d.estimate.low}–{d.estimate.high}</Text> / {d.estimate.full_score}
                {d.estimate.target_score !== undefined ? ` · 目标 ${d.estimate.target_score}` : ''}
              </Text>
            ) : (
              <Text variant="caption">做完一套导入的真题卷后生成预估分</Text>
            )}
            {shares && lossTotal > 0 ? (
              <Text variant="caption">
                失分：知识没掌握 {Math.round(shares.knowledge * 100)}% · 答题不规范 {Math.round(shares.norm * 100)}% · 时间不够 {Math.round(shares.time * 100)}%
              </Text>
            ) : null}
          </Card>
        ) : null}

        {survey.data?.open && !survey.data.submitted ? (
          <Card style={[styles.gap, styles.survey]}>
            <Text variant="bodyStrong">初试辛苦了！填写考后回访送 {survey.data.reward_days} 天会员</Text>
            <Button title="去填写" onPress={() => router.push('/mine/survey')} />
          </Card>
        ) : null}

        <Card style={styles.list}>
          <Row title="我的资料" desc={o ? `${o.materials} 份 · ${o.questions} 题` : undefined} onPress={() => router.push('/library')} />
          <Row
            title="错题本"
            desc={o ? `${o.wrong} 题` : undefined}
            onPress={() => (main ? router.push({ pathname: '/practice/wrong', params: { subjectId: String(main.id) } }) : router.push('/(tabs)/train'))}
          />
          <Row
            title="作文本"
            desc={o ? (o.essays ? `${o.essays} 篇` : '还没有作文') : undefined}
            onPress={() => {
              const essay = list.find((s) => s.is_essay);
              if (essay) router.push({ pathname: '/essay/book', params: { subjectId: String(essay.id) } });
              else router.push('/(tabs)/train');
            }}
          />
          <Row title="导出题库" desc="PDF / Word" onPress={() => router.push('/mine/export')} />
          <Row title="备考设置" desc="专业课、目标分、初试日期、阶段" onPress={() => router.push('/settings/prep')} />
          <Row title="兑换码" onPress={() => router.push('/mine/redeem')} />
          <Row title="意见反馈" onPress={() => router.push('/mine/feedback')} />
          {survey.data?.submitted ? <Row title="考后回访" desc="补充复试、录取结果" onPress={() => router.push('/mine/survey')} /> : null}
          <Row title="设置" onPress={() => router.push('/settings')} />
        </Card>
        {appConfig.variant !== 'production' ? <Button title="基础组件演示" kind="text" onPress={() => router.push('/dev/components')} /> : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  head: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, paddingTop: spacing.md },
  avatar: { width: 56, height: 56, borderRadius: 28, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.primary },
  member: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 64, padding: spacing.lg, borderRadius: radius.lg, backgroundColor: semantic.primary },
  rowInline: { flexDirection: 'row', alignItems: 'center' },
  survey: { backgroundColor: semantic.amberSoft },
  list: { paddingVertical: spacing.xs },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 52, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
});
