// 6.2 提分看板：按专业课切换；预估分趋势（按周）与目标线；失分归因近 30 天三类占比，点击看相关错题；
// 各板块掌握度 × 用户真题里的分值占比（与 3.8 一致）；「以为会了」列表，可一键加入今日训练；最近整卷与模拟考试成绩。
// 作文课显示各维度平均分（T23）。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, ProgressBar, Screen, Text } from '@/components';
import { useAddFalseMastery, useDashboard } from '@/features/dashboard/api';
import { PageHeader } from '@/features/import/ui';
import { lossNames, modeNames } from '@/features/paper/api';
import { EstimateLine } from '@/features/today/Cards';
import { api, unwrap } from '@/lib/api';

type Week = Schemas['EstimateWeek'];

/** 预估分趋势：每周一条区间柱（低–高），目标线横穿；按满分缩放。 */
function Trend({ weeks, full, target }: { weeks: Week[]; full: number; target?: number }) {
  const H = 120;
  const y = (v: number) => (Math.min(Math.max(v, 0), full) / full) * H;
  return (
    <View>
      <View style={[styles.chart, { height: H }]}>
        {target !== undefined ? (
          <View style={[styles.targetLine, { bottom: y(target) }]}>
            <Text variant="small" style={styles.targetLabel}>
              目标 {target}
            </Text>
          </View>
        ) : null}
        {weeks.map((w) => (
          <View key={w.week_start} style={styles.col} accessibilityLabel={`${w.week_start} 周 ${w.low} 到 ${w.high}`}>
            <View style={[styles.range, { bottom: y(w.low), height: Math.max(y(w.high) - y(w.low), 4) }]} />
          </View>
        ))}
      </View>
      <View style={styles.cols}>
        {weeks.map((w, i) => (
          <Text key={w.week_start} variant="small" style={styles.colLabel}>
            W{i + 1}
          </Text>
        ))}
      </View>
    </View>
  );
}

export default function DashboardPage() {
  const params = useLocalSearchParams<{ subjectId?: string }>();
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const list = subjects.data?.items ?? [];
  const [picked, setPicked] = useState<number>();
  const sid = picked ?? (Number(params.subjectId) || list[0]?.id || 0);
  const dash = useDashboard(sid);
  const add = useAddFalseMastery(sid);

  if (subjects.isLoading || (sid > 0 && dash.isLoading)) return <Screen><Loading rows={6} /></Screen>;
  if (subjects.isError) return <Screen><ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /></Screen>;
  if (list.length === 0) {
    return (
      <Screen>
        <PageHeader title="提分看板" onBack={() => router.back()} />
        <EmptyState title="还没有专业课" desc="先在备考设置里添加专业课并导入资料" actionText="去导入" onAction={() => router.push('/import')} />
      </Screen>
    );
  }
  if (dash.isError || !dash.data) return <Screen><ErrorState error={dash.error} onRetry={() => void dash.refetch()} /></Screen>;
  const d = dash.data;
  const e = d.estimate;
  const lossTotal = d.loss_points.knowledge + d.loss_points.norm + d.loss_points.time;

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="提分看板" onBack={() => router.back()} />
        {list.length > 1 ? (
          <View style={styles.tabs}>
            {list.map((s) => (
              <Pressable
                key={s.id}
                accessibilityRole="tab"
                accessibilityState={{ selected: s.id === sid }}
                onPress={() => setPicked(s.id)}
                style={[styles.tab, s.id === sid && styles.tabOn]}
              >
                <Text variant="caption" color={s.id === sid ? semantic.textOnBrand : undefined}>
                  {s.code ? `${s.code} ` : ''}
                  {s.name}
                </Text>
              </Pressable>
            ))}
          </View>
        ) : null}

        <Card style={styles.gap}>
          <Text variant="h3">预估分趋势</Text>
          {e.ready ? (
            <>
              <EstimateLine e={e} onBrand={false} />
              {d.trend.length > 0 ? <Trend weeks={d.trend} full={e.full_score} target={e.target_score} /> : null}
            </>
          ) : (
            <>
              <Text variant="caption">做完一套导入的真题卷后生成预估分，AI 组卷的成绩不计入</Text>
              <Button title="去做整卷" kind="secondary" onPress={() => router.push({ pathname: '/paper/list', params: { subjectId: String(sid) } })} />
            </>
          )}
        </Card>

        <Card style={styles.gap}>
          <Text variant="h3">失分归因 · 近 30 天</Text>
          {lossTotal > 0 ? (
            (['knowledge', 'norm', 'time'] as const).map((k) => (
              <Pressable
                key={k}
                accessibilityRole="button"
                onPress={() => router.push({ pathname: '/practice/wrong', params: { subjectId: String(sid), group: 'loss' } })}
                style={styles.lossRow}
              >
                <Text variant="body" style={styles.flex}>
                  {lossNames[k]}
                </Text>
                <Text variant="bodyStrong">{Math.round(d.loss_shares[k] * 100)}% ›</Text>
              </Pressable>
            ))
          ) : (
            <Text variant="caption">近 30 天还没有主观题批改，做几道主观题后显示</Text>
          )}
        </Card>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="h3" style={styles.flex}>
              各板块掌握度
            </Text>
            {d.sections_ready ? <Text variant="small">× 你真题里的分值占比</Text> : null}
          </View>
          {d.sections.length === 0 ? <Text variant="caption">题库里还没有板块</Text> : null}
          {d.sections.map((s) => (
            <View key={s.id} style={styles.section}>
              <View style={styles.row}>
                <Text variant="body" style={styles.flex}>
                  {s.name}
                </Text>
                <Text variant="bodyStrong">{Math.round(s.mastery)}%</Text>
                {d.sections_ready ? <Text variant="small">占{Math.round(s.share * 100)}%</Text> : null}
              </View>
              <ProgressBar value={s.mastery / 100} />
            </View>
          ))}
          {!d.sections_ready && d.sections.length > 0 ? <Text variant="small">导入 2 套以上真题卷后显示各板块的分值占比</Text> : null}
        </Card>

        {d.false_mastery.length > 0 ? (
          <Card style={[styles.gap, styles.falseCard]}>
            <Text variant="bodyStrong">{d.false_mastery.length} 个「以为会了」</Text>
            <Text variant="caption">自评掌握但最近作答正确率偏低：{d.false_mastery.map((k) => k.name).join('、')}</Text>
            <Button title="加入今日训练" kind="secondary" loading={add.isPending} onPress={() => add.mutate()} />
          </Card>
        ) : null}

        {d.essay_dims.length > 0 ? (
          <Card style={styles.gap}>
            <Text variant="h3">作文各维度平均分</Text>
            {d.essay_dims.map((x) => (
              <View key={x.name} style={styles.section}>
                <View style={styles.row}>
                  <Text variant="body" style={styles.flex}>
                    {x.name}
                  </Text>
                  <Text variant="bodyStrong">
                    {x.score} / {x.max}
                  </Text>
                </View>
                <ProgressBar value={x.max ? x.score / x.max : 0} />
              </View>
            ))}
            <Button title="去作文本" kind="text" onPress={() => router.push({ pathname: '/essay/book', params: { subjectId: String(sid) } })} />
          </Card>
        ) : null}

        {d.recent_papers.length > 0 ? (
          <Card style={styles.list}>
            <Text variant="h3" style={styles.listHead}>
              最近整卷与模拟考试
            </Text>
            {d.recent_papers.map((p) => (
              <Pressable
                key={p.session_id}
                accessibilityRole="button"
                accessibilityLabel={p.title}
                onPress={() => router.push({ pathname: '/paper/report/[id]', params: { id: String(p.session_id) } })}
                style={styles.paperRow}
              >
                <View style={styles.flex}>
                  <Text variant="body">{p.title}</Text>
                  <Text variant="caption">
                    {modeNames[p.mode]}
                    {p.counts_for_estimate ? '' : ' · 不计入预估分'}
                  </Text>
                </View>
                <Text variant="bodyStrong">
                  {p.score} / {p.full_score}
                </Text>
              </Pressable>
            ))}
          </Card>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  tabs: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  tab: { minHeight: 36, justifyContent: 'center', paddingHorizontal: spacing.md, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border },
  tabOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  chart: { flexDirection: 'row', alignItems: 'flex-end', gap: spacing.sm, marginTop: spacing.sm },
  col: { flex: 1, height: '100%' },
  range: { position: 'absolute', left: '25%', right: '25%', borderRadius: 4, backgroundColor: colors.amber },
  targetLine: { position: 'absolute', left: 0, right: 0, borderTopWidth: 1, borderStyle: 'dashed', borderTopColor: semantic.primary },
  targetLabel: { position: 'absolute', right: 0, top: -16 },
  cols: { flexDirection: 'row', gap: spacing.sm },
  colLabel: { flex: 1, textAlign: 'center' },
  lossRow: { flexDirection: 'row', alignItems: 'center', minHeight: 44, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  section: { gap: 4, paddingVertical: 2 },
  falseCard: { backgroundColor: semantic.dangerSoft },
  list: { paddingVertical: spacing.sm },
  listHead: { paddingVertical: spacing.xs },
  paperRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 56, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
});
