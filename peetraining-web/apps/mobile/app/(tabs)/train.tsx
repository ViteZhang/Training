// 4.1 训练首页：按阶段排序（强化期：今日训练 → 本周题型专项 → 按题型练 → 整卷 → 背诵 / 错题本 / 答题规范）；
// 冲刺期、考前期把整卷置顶。题目都来自用户的题库，不够时 AI 出变式题。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, Switch, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Icon, Loading, ProgressBar, Screen, Segmented, Tag, Text, toast } from '@/components';
import type { IconName } from '@/components/Icon';
import { entryTones } from '@/features/today/Cards';
import { qtypeNames } from '@/features/import/api';
import { stageInfo } from '@/features/onboarding/api';
import { usePracticeHome, useStartPractice } from '@/features/practice/api';
import { PendingGradingsCard } from '@/features/practice/grading';
import { flush, useAIFillPref, usePending } from '@/features/practice/offline';
import { useStartRecite } from '@/features/recite/api';
import { api, unwrap } from '@/lib/api';

function Row({ title, desc, onPress, icon, tone, first }: { title: string; desc: string; onPress: () => void; icon: IconName; tone: { bg: string; fg: string }; first?: boolean }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={title} onPress={onPress} style={[styles.row, !first && styles.divider]}>
      <View style={[styles.badge, { backgroundColor: tone.bg }]}>
        <Icon name={icon} size={18} color={tone.fg} />
      </View>
      <View style={[styles.flex, styles.gap2]}>
        <Text variant="bodyStrong">{title}</Text>
        <Text variant="small">{desc}</Text>
      </View>
      <Icon name="chevron" size={16} color={semantic.textSecondary} />
    </Pressable>
  );
}

export default function TrainTab() {
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [picked, setPicked] = useState<number>();
  const list = subjects.data?.items ?? [];
  const subject = list.find((s) => s.id === picked) ?? list.find((s) => !s.is_essay) ?? list[0];
  const home = usePracticeHome(subject?.id);
  const start = useStartPractice();
  const recite = useStartRecite();
  const pending = usePending((s) => s.count);
  const [aiFill, setAiFill] = useAIFillPref();

  // 有离线作答没交时，进入训练页就试着补交。
  useEffect(() => {
    if (pending > 0) void flush().then((n) => n > 0 && toast(`已补交 ${n} 道离线作答`));
  }, [pending]);

  if (subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (subjects.isError) return <Screen><ErrorState error={subjects.error} onRetry={() => void subjects.refetch()} /></Screen>;
  if (!subject) return <Screen><EmptyState title="还没有专业课" actionText="去添加" onAction={() => router.push('/settings/prep')} /></Screen>;
  const h = home.data;
  const sid = subject.id;
  const essay = list.find((x) => x.is_essay && x.id !== sid);

  const paper = (
    <Pressable key="paper" accessibilityRole="button" onPress={() => router.push({ pathname: '/paper/list', params: { subjectId: String(sid) } })} style={styles.paper}>
      <View style={[styles.badge, { backgroundColor: entryTones.paper.bg }]}>
        <Icon name="paper" size={18} color={entryTones.paper.fg} />
      </View>
      <View style={[styles.flex, styles.gap2]}>
        <Text variant="bodyStrong" style={styles.title16}>
          整卷 · 模拟考试
        </Text>
        <Text variant="small">按你导入的真题卷限时作答</Text>
      </View>
      <Icon name="chevron" size={16} color={semantic.textSecondary} />
    </Pressable>
  );

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} refreshControl={<RefreshControl refreshing={home.isRefetching} onRefresh={() => void home.refetch()} />}>
        <View style={styles.header}>
          <Text variant="h1">训练</Text>
          {h ? <Tag label={stageInfo[h.stage].name} tone="outline" size="md" /> : null}
        </View>
        {list.length > 1 ? (
          <Segmented options={list.map((x) => ({ key: x.id, label: `${x.code ? `${x.code} ` : ''}${x.name}` }))} value={sid} onChange={setPicked} />
        ) : null}
        {pending > 0 ? (
          <Card style={styles.card}>
            <Text variant="caption" color={semantic.info}>
              有 {pending} 道离线作答等联网后提交
            </Text>
          </Card>
        ) : null}

        <PendingGradingsCard />
        {subject.is_essay ? (
          <Card style={styles.list}>
            <Row first title="作文训练" desc="真题、AI 命题或自拟题目，写完按评分标准批改" icon="pen" tone={entryTones.amber} onPress={() => router.push({ pathname: '/essay', params: { subjectId: String(sid) } })} />
          </Card>
        ) : null}
        {home.isLoading ? <Loading rows={4} /> : home.isError ? <ErrorState error={home.error} onRetry={() => void home.refetch()} /> : !h ? null : h.total_questions === 0 && subject.is_essay ? null : h.total_questions === 0 ? (
          <EmptyState title="这门课还没有题目" desc="导入真题、习题或讲义后就能练" actionText="导入资料" onAction={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />
        ) : (
          <>
            {h.paper_first ? paper : null}
            {h.in_progress && h.in_progress.kind !== 'daily' ? (
              <Card style={styles.card}>
                <Text variant="bodyStrong">继续上次的{h.in_progress.title}</Text>
                <ProgressBar value={h.in_progress.total ? h.in_progress.done / h.in_progress.total : 0} />
                <Text variant="caption">
                  已做 {h.in_progress.done} / {h.in_progress.total} 题
                </Text>
                <Button title="继续" kind="secondary" onPress={() => router.push({ pathname: '/practice/[id]', params: { id: String(h.in_progress!.session_id) } })} />
              </Card>
            ) : null}

            <View style={styles.today}>
              <View style={[styles.rowInline, styles.baseline]}>
                <Text variant="h3" color={colors.white} style={styles.flex}>
                  今日训练
                </Text>
                {h.today ? (
                  <Text variant="caption" color="#C4C0E0">
                    {h.today.done} / {h.today.total} · 约 {Math.round(h.today.minutes)} 分钟
                  </Text>
                ) : null}
              </View>
              {h.today && h.today.total > 0 ? (
                <Pressable
                  accessibilityRole="button"
                  disabled={start.isPending && start.variables?.kind === 'daily'}
                  onPress={() =>
                    h.today?.session_id
                      ? router.push({ pathname: '/practice/[id]', params: { id: String(h.today.session_id) } })
                      : start.mutate({ subject_id: sid, kind: 'daily', ai_fill: aiFill })
                  }
                  style={styles.todayBtn}
                >
                  <Text variant="bodyStrong" color={colors.indigo} style={styles.bold}>
                    {h.today.session_id ? '继续训练' : '开始训练'}
                  </Text>
                </Pressable>
              ) : (
                <Text variant="caption" color="#C4C0E0">
                  这门课今天没有安排题目，可以按题型练或自定义练习
                </Text>
              )}
              {h.today && !h.today.session_id ? (
                <View style={styles.rowInline}>
                  <Text variant="small" color="#C4C0E0" style={styles.flex}>
                    题量不够时 AI 按你的知识点补变式题
                  </Text>
                  <Switch accessibilityLabel="今日训练 AI 补题" value={aiFill} onValueChange={setAiFill} trackColor={{ true: colors.amber, false: colors.track }} />
                </View>
              ) : null}
            </View>

            {h.type_drill?.qtype ? (
              <Pressable accessibilityRole="button" onPress={() => start.mutate({ subject_id: sid, kind: 'type_drill', qtype: h.type_drill!.qtype })} style={styles.push}>
                <View style={[styles.flex, styles.gap2]}>
                  <Text variant="caption" color={pushInk} style={styles.bold}>
                    本周题型专项 · {qtypeNames[h.type_drill.qtype]} {h.type_drill.done_this_week} / {h.type_drill.weekly_target}
                  </Text>
                  <Text variant="small" color={pushInk}>
                    先看规范写法，再练 10 道你题库里的{qtypeNames[h.type_drill.qtype]}
                  </Text>
                </View>
                <View style={styles.pushBtn}>
                  <Text variant="caption" color={colors.ink} style={styles.bold}>
                    去练
                  </Text>
                </View>
              </Pressable>
            ) : null}

            <View style={styles.section}>
              <View style={styles.rowInline}>
                <Text variant="h3" style={[styles.flex, styles.title16]}>
                  按题型练
                </Text>
                <Button title="自定义 ›" kind="text" size="sm" style={styles.link} onPress={() => router.push({ pathname: '/practice/custom', params: { subjectId: String(sid) } })} />
              </View>
              <View style={styles.grid}>
                {h.qtype_counts.map((q) => (
                  <Pressable
                    key={q.qtype}
                    accessibilityRole="button"
                    accessibilityLabel={`练${qtypeNames[q.qtype]}`}
                    onPress={() => start.mutate({ subject_id: sid, kind: 'type_drill', qtype: q.qtype })}
                    style={styles.tile}
                  >
                    <Text variant="caption" color={colors.ink} style={styles.medium} numberOfLines={1}>
                      {qtypeNames[q.qtype]}
                    </Text>
                    <Text variant="small" style={styles.tiny}>
                      {q.count} 题
                    </Text>
                  </Pressable>
                ))}
              </View>
            </View>

            {h.paper_first ? null : paper}
            <Card style={styles.list}>
              <Row first title="背诵" icon="book" tone={entryTones.recite} desc={h.recite_due > 0 ? `${h.recite_due} 条待背 · 挖空 · 默写` : '今天没有到期要背的'} onPress={() => recite.mutate({ subject_id: sid })} />
              <Row
                title="错题本"
                icon="wrong"
                tone={entryTones.amber}
                desc={h.wrong_book.total > 0 ? `${h.wrong_book.total} 题 · ${h.wrong_book.due} 题到了复习日` : '还没有错题'}
                onPress={() => router.push({ pathname: '/practice/wrong', params: { subjectId: String(sid) } })}
              />
              <Row
                title="答题规范"
                icon="edit"
                tone={entryTones.info}
                desc="名词解释、简答、论述怎么写才拿分"
                onPress={() => router.push({ pathname: '/practice/norm', params: { subjectId: String(sid), qtype: h.type_drill?.qtype ?? 'term' } })}
              />
              {essay ? (
                <Row
                  title={`${essay.code ? `${essay.code} ` : ''}${essay.name}`}
                  icon="pen"
                  tone={entryTones.amber}
                  desc="真题、AI 命题或自拟题目，写完按评分标准批改"
                  onPress={() => router.push({ pathname: '/essay', params: { subjectId: String(essay.id) } })}
                />
              ) : null}
            </Card>
            <Text variant="small" style={styles.center}>
              题目都来自你的题库；不够练时 AI 会按你的知识点出变式题，标「AI 出题」
            </Text>
          </>
        )}
      </ScrollView>
    </Screen>
  );
}

const pushInk = '#8A4B12';

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xxl },
  header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', marginTop: spacing.sm },
  card: { gap: spacing.sm },
  list: { paddingVertical: 0, paddingHorizontal: 0, overflow: 'hidden' },
  row: { flexDirection: 'row', alignItems: 'center', gap: 14, minHeight: 62, paddingHorizontal: 16 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  badge: { width: 36, height: 36, borderRadius: 12, alignItems: 'center', justifyContent: 'center' },
  rowInline: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  baseline: { alignItems: 'baseline' },
  flex: { flex: 1 },
  gap2: { gap: 2 },
  bold: { fontWeight: '700' },
  medium: { fontWeight: '500' },
  center: { textAlign: 'center' },
  title16: { fontSize: 16, fontWeight: '700' },
  tiny: { fontSize: 11 },
  link: { paddingHorizontal: 0 },
  today: { gap: 12, padding: 18, borderRadius: radius.card, backgroundColor: colors.indigo },
  todayBtn: { minHeight: 44, borderRadius: 22, alignItems: 'center', justifyContent: 'center', backgroundColor: colors.white },
  push: { flexDirection: 'row', alignItems: 'center', gap: 12, paddingVertical: 14, paddingHorizontal: 16, borderRadius: radius.xl, backgroundColor: semantic.amberSoft },
  pushBtn: { minHeight: 32, paddingHorizontal: 12, borderRadius: 16, justifyContent: 'center', backgroundColor: colors.white },
  section: { gap: 10, marginTop: 6 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  tile: { flexBasis: '22%', flexGrow: 1, minHeight: 54, alignItems: 'center', justifyContent: 'center', gap: 1, borderRadius: radius.lg, backgroundColor: semantic.fill },
  paper: { flexDirection: 'row', alignItems: 'center', gap: 14, padding: 16, marginTop: 6, borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
});
