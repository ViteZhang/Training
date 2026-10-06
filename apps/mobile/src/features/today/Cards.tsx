// 2.1 今日首页的卡片：预估分（T22）、今日训练、阶段主推、我的题库、以为会了、2.1d 今日已完成。
import type { Schemas } from '@training/api-client';
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, Card, Icon, ProgressBar, Text } from '@/components';
import type { IconName } from '@/components/Icon';
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
    <Card style={styles.section}>
      <View style={styles.row}>
        <Text variant="bodyStrong" style={[styles.flex, styles.bold]}>
          专业课预估分
        </Text>
        {first ? (
          <Pressable
            accessibilityRole="button"
            onPress={() => router.push({ pathname: '/dashboard', params: { subjectId: String(first.id) } })}
            style={styles.link}
          >
            <Text variant="small">提分看板</Text>
            <Icon name="chevron" size={14} color={semantic.textSecondary} />
          </Pressable>
        ) : null}
      </View>
      {subjects.map((s, i) => {
        const e = bySubject.get(s.id);
        const ready = !!e?.ready;
        return (
          <View key={s.id} style={styles.estimate}>
            {i > 0 ? <View style={styles.divider} /> : null}
            <View style={styles.row}>
              <View style={styles.flex}>
                <Text variant="caption">
                  {s.code ? <Text variant="caption" style={styles.num}>{s.code} </Text> : null}
                  {s.name}
                </Text>
                {!ready ? (
                  <Text variant="small">{organizing ? '题库整理好后，做完一套导入的真题卷就能估分' : '做完一套整卷后生成预估分'}</Text>
                ) : null}
              </View>
              {s.target_score !== undefined && s.target_score !== null ? (
                <Pressable onPress={onEditTarget} accessibilityRole="button" accessibilityLabel={`修改目标分，当前 ${s.target_score}`} style={styles.target}>
                  <Text variant="small">
                    目标 <Text variant="small" style={styles.targetNum}>{s.target_score}</Text>
                  </Text>
                  <Icon name="edit" size={13} color={semantic.textSecondary} />
                </Pressable>
              ) : (
                <Button title="设目标分" kind="soft" size="sm" onPress={onEditTarget} />
              )}
            </View>
            {e && ready ? <EstimateLine e={e} target={s.target_score ?? undefined} /> : null}
          </View>
        );
      })}
    </Card>
  );
}

/** 预估分区间、区间条与目标线、差距与主要差在、依据说明。 */
export function EstimateLine({ e, target }: { e: Schemas['EstimateCard']; target?: number }) {
  const gap = e.gap === undefined ? null : e.gap > 0 ? `还差约 ${e.gap} 分` : '预估上限已到目标';
  const main = e.main_gap_dimension ? `主要差在${e.main_gap_dimension}` : e.main_gap_qtype ? `主要差在${qtypeNames[e.main_gap_qtype]}题` : '';
  const full = e.full_score || 1;
  const low = Math.max(0, Math.min(1, (e.low ?? 0) / full));
  const high = Math.max(low, Math.min(1, (e.high ?? 0) / full));
  return (
    <View style={styles.estimateBody}>
      <View style={styles.rowBase}>
        <Text style={styles.range} accessibilityLabel={`预估分 ${e.low} 到 ${e.high}`}>
          {e.low}–{e.high}
        </Text>
        <Text variant="caption">
          {' '}/ {e.full_score}
        </Text>
      </View>
      <View style={styles.rangeBar} accessibilityElementsHidden>
        <View style={styles.rangeTrack} />
        <View style={[styles.rangeFill, { left: `${low * 100}%`, width: `${Math.max(0.01, high - low) * 100}%` }]} />
        {target ? <View style={[styles.targetLine, { left: `${Math.min(1, target / full) * 100}%` }]} /> : null}
      </View>
      {gap || main || e.today_change ? (
        <View style={styles.dotRow}>
          <View style={[styles.dot, { backgroundColor: semantic.progress }]} />
          <Text variant="small" style={styles.flex}>
            {e.today_change ? (
              <Text variant="small">
                今天 <Text variant="small" color={e.today_change > 0 ? colors.green : colors.red}>{e.today_change > 0 ? `+${e.today_change}` : e.today_change} 分</Text>
                {gap || main ? ' · ' : ''}
              </Text>
            ) : null}
            {[gap, main].filter(Boolean).join(' · ')}
          </Text>
        </View>
      ) : null}
      <Text variant="small" style={styles.basis}>
        {e.is_essay
          ? `依据你最近 ${e.basis_papers} 篇按评分细则批改的真题限时作文估算`
          : `依据你导入的 ${e.basis_papers} 套真题卷实测${e.basis_questions ? `和近 ${e.basis_questions} 道主观题` : ''}估算`}
      </Text>
    </View>
  );
}

const groupDot: Record<string, string> = { new: colors.indigo, review: colors.blue, weak: colors.amber, recite: '#8C80E0' };

/** 今日训练：分组题数与预计用时，「开始训练」进入训练（T17）。 */
export function PlanCard({ plan, early }: { plan: TodayPlan; early?: boolean }) {
  const doneGroups = plan.groups.filter((g) => g.done >= g.count).length;
  const weakName = plan.stage === 'foundation' ? groupNames.weak : '题型专项';
  const count = plan.groups.reduce((n, g) => n + g.count, 0);
  if (early) {
    // 2.1b 题库整理中：先从已识别的题里练起来
    return (
      <Card style={[styles.section, styles.gap12]}>
        <View style={[styles.row, styles.baseline]}>
          <Text variant="h3" style={[styles.flex, styles.h16]}>
            先练起来
          </Text>
          <Text variant="caption">约 {minutes(plan.total_minutes)} 分钟</Text>
        </View>
        <Text variant="caption" style={styles.lh}>
          从已识别的题里挑了 {count} 道，先熟悉一下怎么刷、怎么批改
        </Text>
        <Button title={`先练 ${count} 道`} onPress={() => router.push('/(tabs)/train')} />
      </Card>
    );
  }
  return (
    <Card style={[styles.section, styles.gap12]}>
      <View style={[styles.row, styles.baseline]}>
        <Text variant="h3" style={[styles.flex, styles.h16]}>
          今日训练
        </Text>
        <Text variant="caption">
          {doneGroups} / {plan.groups.length} · 约 {minutes(plan.total_minutes)} 分钟
        </Text>
      </View>
      {plan.items.length === 0 ? (
        <Text variant="caption">今天没有可安排的内容。导入更多题目或资料后，计划会自动补上。</Text>
      ) : (
        <View style={styles.groups}>
          {plan.groups.map((g) => {
            const name = g.group === 'weak' ? weakName : groupNames[g.group];
            return (
              <View key={g.group} style={styles.group} accessibilityLabel={`${name} ${g.count}`}>
                <Text style={styles.groupNum}>{g.done > 0 ? `${g.done}/${g.count}` : g.count}</Text>
                <View style={styles.dotRow}>
                  <View style={[styles.dot, { backgroundColor: groupDot[g.group] ?? colors.gray }]} />
                  <Text variant="small">{name}</Text>
                </View>
              </View>
            );
          })}
        </View>
      )}
      {plan.shortfall_minutes ? (
        <Text variant="small">题量还不够排满每天 {plan.budget_minutes} 分钟，导入更多题目可以补上</Text>
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

/** 阶段主推：基础期新知识点进度、强化期本周题型专项、冲刺期本周整卷、考前期模拟考试。设计稿为浅琥珀底整条可点。 */
export function PushCard({ push }: { push: NonNullable<Home['stage_push']> }) {
  const title = push.qtype ? `${push.title} · ${qtypeNames[push.qtype]}` : push.title;
  const action = push.kind !== 'new_kp_progress';
  return (
    <Pressable
      accessibilityRole={action ? 'button' : undefined}
      disabled={!action}
      onPress={() => router.push('/(tabs)/train')}
      style={styles.push}
    >
      <Icon name="pen" size={20} color={pushInk} />
      <View style={[styles.flex, styles.gap2]}>
        <Text variant="caption" color={pushInk} style={styles.bold}>
          {title}
        </Text>
        {push.progress !== undefined ? <ProgressBar value={push.progress} height={4} /> : null}
        <Text variant="small" color={pushInk}>
          {push.desc}
        </Text>
      </View>
      {action ? (
        <View style={styles.pushBtn}>
          <Text variant="caption" color={colors.ink} style={styles.bold}>
            去练
          </Text>
        </View>
      ) : null}
    </Pressable>
  );
}

const pushInk = '#8A4B12';

const dist: { key: keyof Home['banks'][number]['mastery_distribution']; name: string; color: string; text?: string }[] = [
  { key: 'mastered', name: '已掌握', color: colors.indigo },
  { key: 'consolidating', name: '待巩固', color: colors.amber, text: pushInk },
  { key: 'learning', name: '学习中', color: '#CFCAC0' },
  { key: 'unlearned', name: '未学习', color: semantic.border },
];

/** 我的题库（模块 2 调整 5）：按专业课显示题数和掌握进度；还没导入的直接给导入入口。 */
export function BanksCard({ banks, jobId }: { banks: Home['banks']; jobId?: number | null }) {
  return (
    <Card style={[styles.section, styles.gap14]}>
      <View style={styles.row}>
        <Text variant="h3" style={[styles.flex, styles.h16]}>
          我的题库
        </Text>
        <Pressable accessibilityRole="button" onPress={() => router.push('/(tabs)/bank')} style={styles.link}>
          <Text variant="caption">全部 ›</Text>
        </Pressable>
      </View>
      {banks.map((b, i) => {
        const total = dist.reduce((n, d) => n + b.mastery_distribution[d.key], 0);
        const empty = b.question_count === 0 && b.kp_count === 0;
        const status = b.organizing
          ? `整理中${b.recognized_count ? ` · 已识别 ${b.recognized_count} 条` : ''}`
          : empty
            ? '还没导入资料'
            : `${b.question_count} 题 · ${b.kp_count} 个知识点`;
        const name = (
          <Text variant="bodyStrong">
            {b.code ? <Text variant="bodyStrong" style={styles.code}>{b.code} </Text> : null}
            {b.name}
          </Text>
        );
        return (
          <View key={b.subject_id} style={styles.gap14}>
            {i > 0 ? <View style={styles.divider} /> : null}
            {total > 0 ? (
              <View style={styles.gap8}>
                <View style={[styles.row, styles.baseline]}>
                  <View style={styles.flex}>{name}</View>
                  <Text variant="small">{status}</Text>
                </View>
                <View style={styles.bar} accessibilityLabel="掌握进度">
                  {dist.map((d) =>
                    b.mastery_distribution[d.key] > 0 ? <View key={d.key} style={{ flex: b.mastery_distribution[d.key], backgroundColor: d.color }} /> : null,
                  )}
                </View>
                <View style={styles.legend}>
                  {dist.map((d) => (
                    <Text key={d.key} variant="small" style={styles.legendText} color={d.text ?? semantic.textSecondary}>
                      {d.name} <Text variant="small" color={d.text ?? semantic.textPrimary}>{b.mastery_distribution[d.key]}</Text>
                    </Text>
                  ))}
                </View>
              </View>
            ) : (
              <View style={styles.row}>
                <View style={[styles.flex, styles.gap2]}>
                  {name}
                  <Text variant="small">{status}</Text>
                </View>
                {b.organizing && jobId ? (
                  <Button title="看进度" kind="soft" size="sm" onPress={() => router.push({ pathname: '/import/job/[id]', params: { id: String(jobId) } })} />
                ) : empty && !b.organizing ? (
                  <Button title="导入" kind="soft" size="sm" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(b.subject_id) } })} />
                ) : null}
              </View>
            )}
          </View>
        );
      })}
    </Card>
  );
}

/** 首页快捷入口（设计稿 2.1 底部 2×2）：背诵、错题本、整卷、导入资料。 */
export function EntryGrid({ items }: { items: { icon: IconName; title: string; desc: string; tone: { bg: string; fg: string }; onPress: () => void }[] }) {
  return (
    <View style={styles.grid}>
      {items.map((it) => (
        <Pressable key={it.title} accessibilityRole="button" accessibilityLabel={`${it.title}，${it.desc}`} onPress={it.onPress} style={styles.tile}>
          <View style={[styles.tileIcon, { backgroundColor: it.tone.bg }]}>
            <Icon name={it.icon} size={20} color={it.tone.fg} />
          </View>
          <View style={[styles.flex, styles.gap2]}>
            <Text variant="caption" color={colors.ink} style={styles.bold}>
              {it.title}
            </Text>
            <Text variant="small" numberOfLines={1}>
              {it.desc}
            </Text>
          </View>
        </Pressable>
      ))}
    </View>
  );
}

export const entryTones = {
  recite: { bg: '#EEEBFB', fg: '#3E3190' },
  wrong: { bg: semantic.dangerSoft, fg: colors.red },
  paper: { bg: semantic.masteredSoft, fg: '#1F6B4A' },
  neutral: { bg: semantic.fill, fg: colors.ink },
  info: { bg: semantic.infoSoft, fg: colors.blue },
  amber: { bg: semantic.amberSoft, fg: pushInk },
};

/** 以为会了：自评掌握但实测偏低，为 0 时隐藏。 */
export function FalseMasteryCard({ count }: { count: number }) {
  if (count <= 0) return null;
  return (
    <Pressable onPress={() => router.push('/(tabs)/bank')} accessibilityRole="button" style={styles.falseMastery}>
      <View style={[styles.dot, { backgroundColor: semantic.progress }]} />
      <Text variant="caption" color={colors.ink} style={styles.flex}>
        <Text variant="caption" color={colors.ink} style={styles.bold}>
          {count} 个知识点「以为会了」
        </Text>
        <Text variant="caption"> · 自评掌握但实测偏低</Text>
      </Text>
      <Icon name="chevron" size={16} color={semantic.textSecondary} />
    </Pressable>
  );
}

/** 2.1d 今日已完成：连续打卡、今天的题数与用时、明天预计。 */
export function DoneCard({ home, summary }: { home: Home; summary?: Schemas['TodaySummary'] }) {
  return (
    <>
      <Card style={[styles.section, styles.gap14]}>
        <View style={styles.row}>
          <View style={[styles.tileIcon, styles.doneIcon]}>
            <Icon name="check" size={20} color={colors.green} />
          </View>
          <View style={[styles.flex, styles.gap2]}>
            <Text variant="h3">今天的训练完成了</Text>
            <Text variant="caption">
              已连续打卡 <Text variant="caption" color={colors.ink} style={styles.num}>{home.streak_days}</Text> 天
            </Text>
          </View>
        </View>
        {summary ? (
          <View style={styles.groups}>
            <Stat value={summary.question_count} unit="道题" />
            <Stat value={minutes(summary.minutes)} unit="分钟" />
            <Stat value={`+${summary.new_mastered}`} unit="个已掌握" color={colors.green} />
          </View>
        ) : null}
        <View style={styles.row}>
          <Button title="今日总结" kind="secondary" style={styles.flex} onPress={() => router.push('/plan/summary')} />
          <Button title="再练一组" style={styles.flex2} onPress={() => router.push('/(tabs)/train')} />
        </View>
      </Card>
      {home.tomorrow_minutes ? (
        <View style={styles.falseMastery}>
          <View style={[styles.dot, { backgroundColor: colors.blue }]} />
          <View style={styles.gap2}>
            <Text variant="caption" color={colors.ink} style={styles.bold}>
              明天预计
            </Text>
            <Text variant="small">约 {minutes(home.tomorrow_minutes)} 分钟</Text>
          </View>
        </View>
      ) : null}
    </>
  );
}

export function Stat({ value, unit, color }: { value: number | string; unit: string; color?: string }) {
  return (
    <View style={styles.group}>
      <Text style={[styles.groupNum, color ? { color } : null]}>
        {value}
      </Text>
      <Text variant="small">{unit}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  section: { gap: 10, paddingVertical: 16 },
  gap12: { gap: 12 },
  gap14: { gap: 14 },
  gap8: { gap: 8 },
  gap2: { gap: 2 },
  card: { gap: spacing.sm },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 21 },
  h16: { fontSize: 16 },
  num: { fontFamily: fontFamily.numberSemiBold },
  code: { fontFamily: fontFamily.numberSemiBold, fontWeight: '600' },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  baseline: { alignItems: 'baseline' },
  flex: { flex: 1 },
  flex2: { flex: 1.4 },
  doneIcon: { width: 42, height: 42, borderRadius: 14, backgroundColor: semantic.masteredSoft },
  link: { flexDirection: 'row', alignItems: 'center', minHeight: 32, gap: 2 },
  divider: { height: 1, backgroundColor: semantic.border },
  estimate: { gap: 4 },
  estimateBody: { gap: 8 },
  rowBase: { flexDirection: 'row', alignItems: 'baseline', flexWrap: 'wrap' },
  range: { fontFamily: fontFamily.numberSemiBold, fontSize: 28, lineHeight: 34, letterSpacing: -1, color: colors.indigo },
  rangeBar: { height: 22, justifyContent: 'center' },
  rangeTrack: { position: 'absolute', left: 0, right: 0, top: 8, height: 6, borderRadius: 3, backgroundColor: semantic.border },
  rangeFill: { position: 'absolute', top: 8, height: 6, borderRadius: 3, backgroundColor: colors.indigo },
  targetLine: { position: 'absolute', top: 2, width: 2, height: 18, marginLeft: -1, backgroundColor: colors.amber },
  dotRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  basis: { fontSize: 11, lineHeight: 16 },
  target: { flexDirection: 'row', alignItems: 'center', gap: 4, minHeight: 32, paddingHorizontal: 10, marginRight: -10 },
  targetNum: { fontFamily: fontFamily.numberSemiBold, color: pushInk },
  groups: { flexDirection: 'row', gap: spacing.sm },
  group: { flex: 1, gap: 4 },
  groupNum: { fontFamily: fontFamily.numberSemiBold, fontSize: 24, lineHeight: 30, color: colors.ink },
  push: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, backgroundColor: semantic.amberSoft },
  pushBtn: { minHeight: 32, paddingHorizontal: 12, borderRadius: 16, backgroundColor: semantic.surface, justifyContent: 'center' },
  bar: { flexDirection: 'row', height: 6, borderRadius: 3, overflow: 'hidden', gap: 2, backgroundColor: semantic.border },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: 12 },
  legendText: { fontSize: 11 },
  falseMastery: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 44, paddingVertical: 12, paddingHorizontal: 16, borderRadius: radius.xl, backgroundColor: semantic.fill },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 12 },
  tile: { flexBasis: '47%', flexGrow: 1, flexDirection: 'row', alignItems: 'center', gap: 12, padding: 14, minHeight: 72, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  tileIcon: { width: 38, height: 38, borderRadius: 12, alignItems: 'center', justifyContent: 'center' },
});
