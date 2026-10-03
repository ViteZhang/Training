// 2.1 今日首页的卡片：预估分（T22）、今日训练、阶段主推、我的题库、以为会了、2.1d 今日已完成。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, Card, ProgressBar, Text } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { groupNames, minutes, type Home, type TodayPlan } from './api';

/** 专业课预估分（PRD 11.6，模块 2 调整 1、2）：做完至少一套导入的真题卷后显示区间、差距与依据；没有的写明「做完一套整卷后生成预估分」；
 * 目标分点了能改（2.1f）；「提分看板」→ 6.2。2.1b 题库整理中时写明整理好后做完一套真题卷就能估分；2.1d 显示今天的变化。 */
export function EstimateCard({
  subjects,
  estimates,
  organizing,
  onEditTarget,
}: {
  subjects: Schemas['Subject'][];
  estimates: Schemas['EstimateCard'][];
  organizing?: boolean;
  onEditTarget: () => void;
}) {
  const bySubject = new Map(estimates.map((e) => [e.subject_id, e]));
  const first = subjects[0];
  return (
    <Card style={[styles.card, styles.brand]}>
      <View style={styles.row}>
        <Text variant="bodyStrong" color={semantic.textOnBrand} style={styles.flex}>
          专业课预估分
        </Text>
        {first ? (
          <Pressable
            accessibilityRole="button"
            onPress={() => router.push({ pathname: '/dashboard', params: { subjectId: String(first.id) } })}
            style={styles.target}
          >
            <Text variant="caption" color={colors.amber}>
              提分看板 ›
            </Text>
          </Pressable>
        ) : null}
      </View>
      {subjects.map((s) => {
        const e = bySubject.get(s.id);
        return (
          <View key={s.id} style={styles.estimate}>
            <View style={styles.row}>
              <Text variant="caption" color={colors.amber}>
                {s.code ? `${s.code} ` : ''}
              </Text>
              <Text variant="body" color={semantic.textOnBrand} style={styles.flex}>
                {s.name}
              </Text>
              {s.target_score !== undefined ? (
                <Pressable onPress={onEditTarget} accessibilityRole="button" accessibilityLabel={`修改${s.name}目标分`} style={styles.target}>
                  <Text variant="caption" color={semantic.textOnBrand}>
                    目标 <Text variant="number" color={semantic.textOnBrand}>{s.target_score}</Text>
                  </Text>
                </Pressable>
              ) : (
                <Button title="设目标分" kind="text" onPress={onEditTarget} />
              )}
            </View>
            {e?.ready ? <EstimateLine e={e} /> : (
              <Text variant="caption" color={semantic.textOnBrand}>
                {organizing ? '题库整理好后，做完一套导入的真题卷就能估分' : '做完一套整卷后生成预估分'}
              </Text>
            )}
          </View>
        );
      })}
    </Card>
  );
}

/** 预估分区间、今天的变化、差距与主要差在、依据说明。 */
export function EstimateLine({ e, onBrand = true }: { e: Schemas['EstimateCard']; onBrand?: boolean }) {
  const fg = onBrand ? semantic.textOnBrand : undefined;
  const gap =
    e.gap === undefined ? null : e.gap > 0 ? `还差约 ${e.gap} 分` : '预估上限已到目标';
  const main = e.main_gap_dimension ? `主要差在${e.main_gap_dimension}` : e.main_gap_qtype ? `主要差在${qtypeNames[e.main_gap_qtype]}题` : '';
  return (
    <View style={styles.estimateBody}>
      <View style={styles.rowBase}>
        <Text variant="score" color={colors.amber} accessibilityLabel={`预估分 ${e.low} 到 ${e.high}`}>
          {e.low}–{e.high}
        </Text>
        <Text variant="caption" color={fg}>
          {' '}/ {e.full_score}
        </Text>
        {e.today_change ? (
          <Text variant="caption" color={colors.amber} style={styles.change}>
            今天 {e.today_change > 0 ? `+${e.today_change}` : e.today_change}
          </Text>
        ) : null}
      </View>
      {gap || main ? (
        <Text variant="caption" color={fg}>
          {[gap, main].filter(Boolean).join(' · ')}
        </Text>
      ) : null}
      <Text variant="small" color={fg}>
        {e.is_essay
          ? `依据你最近 ${e.basis_papers} 篇按评分细则批改的真题限时作文估算`
          : `依据你导入的 ${e.basis_papers} 套真题卷实测${e.basis_questions ? `和近 ${e.basis_questions} 道主观题` : ''}估算`}
      </Text>
    </View>
  );
}

/** 今日训练：分组题数与预计用时，「开始训练」进入训练（T17）。 */
export function PlanCard({ plan }: { plan: TodayPlan }) {
  const doneGroups = plan.groups.filter((g) => g.done >= g.count).length;
  const weakName = plan.stage === 'foundation' ? groupNames.weak : '题型专项';
  return (
    <Card style={styles.card}>
      <View style={styles.row}>
        <Text variant="h3" style={styles.flex}>
          今日训练
        </Text>
        <Text variant="caption">
          {doneGroups} / {plan.groups.length} · 约 {minutes(plan.total_minutes)} 分钟
        </Text>
      </View>
      {plan.items.length === 0 ? (
        <Text variant="body" color={semantic.textSecondary}>
          今天没有可安排的内容。导入更多题目或资料后，计划会自动补上。
        </Text>
      ) : (
        <View style={styles.groups}>
          {plan.groups.map((g) => (
            <View key={g.group} style={styles.group} accessibilityLabel={`${g.group === 'weak' ? weakName : groupNames[g.group]} ${g.count}`}>
              <Text variant="number">{g.done > 0 ? `${g.done}/${g.count}` : g.count}</Text>
              <Text variant="caption">{g.group === 'weak' ? weakName : groupNames[g.group]}</Text>
            </View>
          ))}
        </View>
      )}
      {plan.shortfall_minutes ? (
        <Text variant="caption">题量还不够排满每天 {plan.budget_minutes} 分钟，导入更多题目可以补上</Text>
      ) : null}
      {plan.done_minutes > 0 ? <ProgressBar value={plan.total_minutes ? plan.done_minutes / plan.total_minutes : 0} /> : null}
      {plan.items.length > 0 ? (
        <Button title={plan.done_minutes > 0 ? '继续训练' : '开始训练'} onPress={() => router.push('/(tabs)/train')} />
      ) : (
        <Button title="导入资料" kind="secondary" onPress={() => router.push('/import')} />
      )}
    </Card>
  );
}

/** 阶段主推：基础期新知识点进度、强化期本周题型专项、冲刺期本周整卷、考前期模拟考试。 */
export function PushCard({ push }: { push: NonNullable<Home['stage_push']> }) {
  const title = push.qtype ? `${push.title} · ${qtypeNames[push.qtype]}` : push.title;
  return (
    <Card style={styles.card}>
      <Text variant="bodyStrong">{title}</Text>
      {push.progress !== undefined ? <ProgressBar value={push.progress} /> : null}
      <Text variant="caption">{push.desc}</Text>
      {push.kind !== 'new_kp_progress' ? <Button title="去练" kind="secondary" onPress={() => router.push('/(tabs)/train')} /> : null}
    </Card>
  );
}

const dist: { key: keyof Home['banks'][number]['mastery_distribution']; name: string; color: string }[] = [
  { key: 'mastered', name: '已掌握', color: semantic.mastered },
  { key: 'consolidating', name: '待巩固', color: semantic.progress },
  { key: 'learning', name: '学习中', color: semantic.info },
  { key: 'unlearned', name: '未学习', color: semantic.border },
];

/** 我的题库（模块 2 调整 5）：按专业课显示题数和掌握进度；还没导入的直接给导入入口。 */
export function BanksCard({ banks }: { banks: Home['banks'] }) {
  return (
    <Card style={styles.card}>
      <View style={styles.row}>
        <Text variant="bodyStrong" style={styles.flex}>
          我的题库
        </Text>
        <Button title="全部 ›" kind="text" onPress={() => router.push('/(tabs)/bank')} />
      </View>
      {banks.map((b) => {
        const total = dist.reduce((n, d) => n + b.mastery_distribution[d.key], 0);
        const empty = b.question_count === 0 && b.kp_count === 0;
        return (
          <View key={b.subject_id} style={styles.bank}>
            <View style={styles.row}>
              <View style={styles.flex}>
                <Text variant="body">
                  {b.code ? `${b.code} ` : ''}
                  {b.name}
                </Text>
                <Text variant="caption">
                  {b.organizing ? `整理中${b.recognized_count ? ` · 已识别 ${b.recognized_count} 条` : ''}` : empty ? '还没导入资料' : `${b.question_count} 题 · ${b.kp_count} 个知识点`}
                </Text>
              </View>
              {empty && !b.organizing ? (
                <Button title="导入" kind="text" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(b.subject_id) } })} />
              ) : null}
            </View>
            {total > 0 ? (
              <>
                <View style={styles.bar} accessibilityLabel="掌握进度">
                  {dist.map((d) =>
                    b.mastery_distribution[d.key] > 0 ? <View key={d.key} style={{ flex: b.mastery_distribution[d.key], backgroundColor: d.color }} /> : null,
                  )}
                </View>
                <View style={styles.legend}>
                  {dist.map((d) => (
                    <Text key={d.key} variant="small">
                      {d.name} {b.mastery_distribution[d.key]}
                    </Text>
                  ))}
                </View>
              </>
            ) : null}
          </View>
        );
      })}
    </Card>
  );
}

/** 以为会了：自评掌握但实测偏低，为 0 时隐藏。 */
export function FalseMasteryCard({ count }: { count: number }) {
  if (count <= 0) return null;
  return (
    <Pressable onPress={() => router.push('/(tabs)/bank')} accessibilityRole="button" style={styles.falseMastery}>
      <Text variant="bodyStrong" color={semantic.danger}>
        {count} 个知识点「以为会了」
      </Text>
      <Text variant="caption"> · 自评掌握但实测偏低</Text>
    </Pressable>
  );
}

/** 2.1d 今日已完成：连续打卡、今天的题数与用时、明天预计。 */
export function DoneCard({ home, summary }: { home: Home; summary?: Schemas['TodaySummary'] }) {
  return (
    <Card style={styles.card}>
      <Text variant="h3">今天的训练完成了</Text>
      <Text variant="body">
        已连续打卡 <Text variant="number">{home.streak_days}</Text> 天
      </Text>
      {summary ? (
        <View style={styles.groups}>
          <Stat value={summary.question_count} unit="道题" />
          <Stat value={minutes(summary.minutes)} unit="分钟" />
          <Stat value={`+${summary.new_mastered}`} unit="个已掌握" />
        </View>
      ) : null}
      <View style={styles.row}>
        <Button title="今日总结" kind="secondary" style={styles.flex} onPress={() => router.push('/plan/summary')} />
        <Button title="再练一组" style={styles.flex} onPress={() => router.push('/(tabs)/train')} />
      </View>
      {home.tomorrow_minutes ? (
        <Text variant="caption">明天预计约 {minutes(home.tomorrow_minutes)} 分钟</Text>
      ) : null}
    </Card>
  );
}

export function Stat({ value, unit }: { value: number | string; unit: string }) {
  return (
    <View style={styles.group}>
      <Text variant="number">{value}</Text>
      <Text variant="caption">{unit}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  card: { gap: spacing.sm },
  brand: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  estimate: { gap: spacing.xs, paddingVertical: spacing.xs },
  estimateBody: { gap: 2 },
  rowBase: { flexDirection: 'row', alignItems: 'baseline', flexWrap: 'wrap' },
  change: { marginLeft: spacing.sm },
  target: { minHeight: 44, justifyContent: 'center' },
  groups: { flexDirection: 'row', gap: spacing.sm },
  group: { flex: 1, alignItems: 'center', paddingVertical: spacing.sm, backgroundColor: semantic.background, borderRadius: radius.md },
  bank: { gap: spacing.xs, paddingVertical: spacing.xs },
  bar: { flexDirection: 'row', height: 6, borderRadius: 3, overflow: 'hidden', backgroundColor: semantic.border },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.md },
  falseMastery: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', minHeight: 44, padding: spacing.md, borderRadius: radius.lg, backgroundColor: semantic.dangerSoft },
});
