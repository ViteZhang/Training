// 6.1 我的：头像与昵称，专业课代码 + 阶段 + 距初试天数（→ 6.9）；会员条（免费版显示开通入口）；提分看板卡片（预估分、目标、失分归因占比）；
// 我的资料、错题本、作文本、导出题库、备考设置、邀请研友、兑换码、意见反馈；考后回访开放时显示入口。
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Icon, Loading, Screen, Text } from '@/components';
import { EntryGrid, entryTones } from '@/features/today/Cards';
import { useDashboard } from '@/features/dashboard/api';
import { mineKeys, tierNames, useMe, useOverview, useProfile, useSubjects, ymd } from '@/features/mine/api';
import { stageInfo } from '@/features/onboarding/api';
import { appConfig } from '@/lib/config';
import { api, unwrap } from '@/lib/api';
import { useFeatureFlag } from '@/lib/flags';

function Row({ title, desc, value, onPress, first }: { title: string; desc?: string; value?: string; onPress: () => void; first?: boolean }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={title} onPress={onPress} style={[styles.row, !first && styles.divider]}>
      <View style={styles.flex}>
        <Text variant="body">{title}</Text>
        {desc ? <Text variant="small">{desc}</Text> : null}
      </View>
      {value ? <Text variant="small">{value}</Text> : null}
      <Icon name="chevron" size={16} color={semantic.textSecondary} />
    </Pressable>
  );
}

export default function MeTab() {
  const me = useMe();
  const profile = useProfile();
  const subjects = useSubjects();
  const overview = useOverview();
  const inviteOn = useFeatureFlag('invite');
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
        <View style={styles.head}>
          <Pressable accessibilityRole="button" accessibilityLabel="备考设置" onPress={() => router.push('/settings/prep')} style={[styles.headMain, styles.flex]}>
            <View style={styles.avatar}>
              <Text variant="h2" color={colors.white}>
                {(m.nickname || '研').slice(0, 1)}
              </Text>
            </View>
            <View style={styles.flex}>
              <Text variant="h2">{m.nickname || '考研人'}</Text>
              <Text variant="small">
                {list.map((x) => x.code || x.name).join(' · ')}
                {p ? ` · ${stageInfo[p.stage].name} · ${p.days_to_exam} 天后初试` : ''}
              </Text>
            </View>
          </Pressable>
          <Pressable accessibilityRole="button" accessibilityLabel="设置" onPress={() => router.push('/settings')} style={styles.gear}>
            <Icon name="gear" size={22} />
          </Pressable>
        </View>

        <Pressable accessibilityRole="button" onPress={() => router.push('/member')} style={styles.member}>
          <View style={styles.flex}>
            <Text variant="bodyStrong" color={colors.white} style={styles.bold}>
              {m.membership.is_member ? (m.membership.tier ? tierNames[m.membership.tier] : '会员') : '免费版'}
            </Text>
            <Text variant="small" color="#C4C0E0">
              {m.membership.is_member && m.membership.ends_at ? `有效期至 ${ymd(m.membership.ends_at)}` : '开通会员：批改、整卷、资料解析不限'}
            </Text>
          </View>
          {!m.membership.is_member ? (
            <View style={styles.open}>
              <Text variant="caption" color={colors.indigo} style={styles.bold}>
                开通
              </Text>
            </View>
          ) : null}
        </Pressable>

        {main ? (
          <Card style={styles.gap}>
            <Pressable accessibilityRole="button" onPress={() => router.push({ pathname: '/dashboard', params: { subjectId: String(main.id) } })} style={styles.rowInline}>
              <Text variant="h3" style={[styles.flex, styles.h16]}>
                提分看板
              </Text>
              <Text variant="small">详情</Text>
              <Icon name="chevron" size={14} color={semantic.textSecondary} />
            </Pressable>
            {d?.estimate.ready ? (
              <View style={styles.rowBase}>
                <Text variant="small">{main.code ?? main.name} 预估 </Text>
                <Text style={styles.range}>
                  {d.estimate.low}–{d.estimate.high}
                </Text>
                <Text variant="small" style={styles.flex}>
                  {' '}/ {d.estimate.full_score}
                </Text>
                {d.estimate.target_score !== undefined && d.estimate.target_score !== null ? (
                  <Text variant="small">
                    目标 <Text variant="small" color="#8A4B12" style={styles.bold}>{d.estimate.target_score}</Text>
                  </Text>
                ) : null}
              </View>
            ) : (
              <Text variant="small">做完一套导入的真题卷后生成预估分</Text>
            )}
            {shares && lossTotal > 0 ? (
              <>
                <View style={styles.stack}>
                  <View style={{ flex: shares.knowledge, backgroundColor: colors.amber }} />
                  <View style={{ flex: shares.norm, backgroundColor: '#B26A00' }} />
                  <View style={{ flex: shares.time, backgroundColor: colors.ink }} />
                </View>
                <Text variant="small">
                  失分：知识没掌握 {Math.round(shares.knowledge * 100)}% · 答题不规范 {Math.round(shares.norm * 100)}% · 时间不够 {Math.round(shares.time * 100)}%
                </Text>
              </>
            ) : null}
          </Card>
        ) : null}

        {survey.data?.open && !survey.data.submitted ? (
          <Card style={[styles.gap, styles.survey]}>
            <Text variant="bodyStrong">初试辛苦了！填写考后回访送 {survey.data.reward_days} 天会员</Text>
            <Button title="去填写" onPress={() => router.push('/mine/survey')} />
          </Card>
        ) : null}

        <EntryGrid
          items={[
            { icon: 'paper', title: '我的资料', desc: o ? `${o.materials} 份 · ${o.questions} 题` : '导入的资料', tone: entryTones.paper, onPress: () => router.push('/library') },
            {
              icon: 'wrong',
              title: '错题本',
              desc: o ? `${o.wrong} 题` : '按失分原因查看',
              tone: entryTones.wrong,
              onPress: () => (main ? router.push({ pathname: '/practice/wrong', params: { subjectId: String(main.id) } }) : router.push('/(tabs)/train')),
            },
            {
              icon: 'pen',
              title: '作文本',
              desc: o ? (o.essays ? `${o.essays} 篇` : '还没有作文') : '作文与批改',
              tone: entryTones.amber,
              onPress: () => {
                const essay = list.find((x) => x.is_essay);
                if (essay) router.push({ pathname: '/essay/book', params: { subjectId: String(essay.id) } });
                else router.push('/(tabs)/train');
              },
            },
            { icon: 'download', title: '导出题库', desc: 'PDF / Word', tone: entryTones.neutral, onPress: () => router.push('/mine/export') },
          ]}
        />

        <Card style={styles.list}>
          <Row first title="备考设置" desc="专业课、目标分、初试日期、阶段" onPress={() => router.push('/settings/prep')} />
          {inviteOn ? <Row title="邀请研友" desc="双方各得会员天数" onPress={() => router.push('/mine/invite')} /> : null}
          <Row title="兑换码" onPress={() => router.push('/mine/redeem')} />
          <Row title="意见反馈" onPress={() => router.push('/mine/feedback')} />
          {survey.data?.submitted ? <Row title="考后回访" value="补充复试、录取结果" onPress={() => router.push('/mine/survey')} /> : null}
        </Card>
        {appConfig.variant !== 'production' ? <Button title="基础组件演示" kind="text" onPress={() => router.push('/dev/components')} /> : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  gap: { gap: 10 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  h16: { fontSize: 16 },
  head: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingTop: spacing.sm },
  headMain: { flexDirection: 'row', alignItems: 'center', gap: 14 },
  gear: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center', marginRight: -10 },
  avatar: { width: 56, height: 56, borderRadius: 28, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.primary },
  member: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 66, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, backgroundColor: semantic.primary },
  open: { minHeight: 32, paddingHorizontal: 14, borderRadius: 16, justifyContent: 'center', backgroundColor: colors.white },
  rowInline: { flexDirection: 'row', alignItems: 'center', gap: 2 },
  rowBase: { flexDirection: 'row', alignItems: 'baseline' },
  range: { fontFamily: fontFamily.numberSemiBold, fontSize: 22, lineHeight: 28, color: colors.indigo },
  stack: { flexDirection: 'row', gap: 2, height: 6, borderRadius: 3, overflow: 'hidden' },
  survey: { backgroundColor: semantic.amberSoft },
  list: { paddingVertical: 0 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 56, paddingVertical: 8 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
});
