// 4.1 训练首页：按阶段排序（强化期：今日训练 → 本周题型专项 → 按题型练 → 整卷 → 背诵 / 错题本 / 答题规范）；
// 冲刺期、考前期把整卷置顶。题目都来自用户的题库，不够时 AI 出变式题。
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, Switch, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, ProgressBar, Screen, Text, toast } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { stageInfo } from '@/features/onboarding/api';
import { usePracticeHome, useStartPractice } from '@/features/practice/api';
import { PendingGradingsCard } from '@/features/practice/grading';
import { flush, useAIFillPref, usePending } from '@/features/practice/offline';
import { api, unwrap } from '@/lib/api';

function Row({ title, desc, onPress }: { title: string; desc: string; onPress: () => void }) {
  return (
    <Pressable accessibilityRole="button" onPress={onPress} style={styles.row}>
      <View style={styles.flex}>
        <Text variant="bodyStrong">{title}</Text>
        <Text variant="caption">{desc}</Text>
      </View>
      <Text variant="body" color={semantic.textSecondary}>
        ›
      </Text>
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

  const paper = (
    <Row key="paper" title="整卷 · 模拟考试" desc="按你导入的真题卷限时作答" onPress={() => toast('整卷练习马上上线')} />
  );

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} refreshControl={<RefreshControl refreshing={home.isRefetching} onRefresh={() => void home.refetch()} />}>
        <View style={styles.header}>
          <Text variant="h1">训练</Text>
          {h ? <Text variant="caption">{stageInfo[h.stage].name}</Text> : null}
        </View>
        {list.length > 1 ? (
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.subjects}>
            {list.map((s) => (
              <Pressable key={s.id} accessibilityRole="tab" accessibilityState={{ selected: s.id === sid }} onPress={() => setPicked(s.id)} style={[styles.subject, s.id === sid && styles.subjectOn]}>
                <Text variant="bodyStrong" color={s.id === sid ? semantic.textOnBrand : semantic.textPrimary}>
                  {s.code ? `${s.code} ` : ''}
                  {s.name}
                </Text>
              </Pressable>
            ))}
          </ScrollView>
        ) : null}
        {pending > 0 ? (
          <Card style={styles.card}>
            <Text variant="caption" color={semantic.info}>
              有 {pending} 道离线作答等联网后提交
            </Text>
          </Card>
        ) : null}

        <PendingGradingsCard />
        {home.isLoading ? <Loading rows={4} /> : home.isError ? <ErrorState error={home.error} onRetry={() => void home.refetch()} /> : !h ? null : h.total_questions === 0 ? (
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

            <Card style={styles.card}>
              <View style={styles.rowInline}>
                <Text variant="h3" style={styles.flex}>
                  今日训练
                </Text>
                {h.today ? (
                  <Text variant="caption">
                    {h.today.done} / {h.today.total} · 约 {Math.round(h.today.minutes)} 分钟
                  </Text>
                ) : null}
              </View>
              {h.today && h.today.total > 0 ? (
                <Button
                  title={h.today.session_id ? '继续训练' : '开始训练'}
                  loading={start.isPending && start.variables?.kind === 'daily'}
                  onPress={() =>
                    h.today?.session_id
                      ? router.push({ pathname: '/practice/[id]', params: { id: String(h.today.session_id) } })
                      : start.mutate({ subject_id: sid, kind: 'daily', ai_fill: aiFill })
                  }
                />
              ) : (
                <Text variant="caption">这门课今天没有安排题目，可以按题型练或自定义练习</Text>
              )}
              {h.today && !h.today.session_id ? (
                <View style={styles.rowInline}>
                  <Text variant="caption" style={styles.flex}>
                    题量不够时 AI 按你的知识点补变式题
                  </Text>
                  <Switch accessibilityLabel="今日训练 AI 补题" value={aiFill} onValueChange={setAiFill} trackColor={{ true: semantic.primary }} />
                </View>
              ) : null}
            </Card>

            {h.type_drill?.qtype ? (
              <Card style={styles.card}>
                <View style={styles.rowInline}>
                  <Text variant="bodyStrong" style={styles.flex}>
                    本周题型专项 · {qtypeNames[h.type_drill.qtype]}
                  </Text>
                  <Text variant="caption">
                    {h.type_drill.done_this_week} / {h.type_drill.weekly_target}
                  </Text>
                </View>
                <Text variant="caption">每周专练一种题型，练 10 道你题库里的{qtypeNames[h.type_drill.qtype]}</Text>
                <Button title="去练" kind="secondary" onPress={() => start.mutate({ subject_id: sid, kind: 'type_drill', qtype: h.type_drill!.qtype })} />
              </Card>
            ) : null}

            <Card style={styles.card}>
              <View style={styles.rowInline}>
                <Text variant="bodyStrong" style={styles.flex}>
                  按题型练
                </Text>
                <Button title="自定义 ›" kind="text" onPress={() => router.push({ pathname: '/practice/custom', params: { subjectId: String(sid) } })} />
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
                    <Text variant="bodyStrong">{qtypeNames[q.qtype]}</Text>
                    <Text variant="caption">{q.count} 题</Text>
                  </Pressable>
                ))}
              </View>
            </Card>

            <Card style={styles.list}>
              {h.paper_first ? null : paper}
              <Row title="背诵" desc={`${h.recite_due} 条待背 · 挖空 · 默写`} onPress={() => toast('背诵马上上线')} />
              <Row
                title="答题规范"
                desc="名词解释、简答、论述怎么写才拿分"
                onPress={() => router.push({ pathname: '/practice/norm', params: { subjectId: String(sid), qtype: h.type_drill?.qtype ?? 'term' } })}
              />
              <Row
                title="错题本"
                desc={h.wrong_book.total > 0 ? `${h.wrong_book.total} 题 · ${h.wrong_book.due} 题到了复习日` : '还没有错题'}
                onPress={() => router.push({ pathname: '/practice/wrong', params: { subjectId: String(sid) } })}
              />
            </Card>
            <Text variant="small" color={semantic.textSecondary}>
              题目都来自你的题库；不够练时 AI 会按你的知识点出变式题，标「AI 出题」
            </Text>
          </>
        )}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  header: { flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm, marginTop: spacing.lg },
  subjects: { gap: spacing.sm },
  subject: { minHeight: 44, justifyContent: 'center', paddingHorizontal: spacing.lg, borderRadius: radius.pill, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  subjectOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  card: { gap: spacing.sm },
  list: { paddingVertical: 0 },
  row: { flexDirection: 'row', alignItems: 'center', minHeight: 56, borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: semantic.border },
  rowInline: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  tile: { width: '48%', minHeight: 56, padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.background },
});
