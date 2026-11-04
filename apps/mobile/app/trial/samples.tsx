// 1.10 学科通用体验题（PRD v3 增补，设计稿 n_sample）：切换学科（哲学、文学）和方向；每个方向列出已上线的体验题（题型 + 分值）；
// 选一道进入 1.9a 作答，之后同体验一道题流程；「示例题只用于体验和入门练习，不计入预估分」。
// 专业不在已覆盖学科时，AI 按用户的专业课现场出一道，标「AI 出题」（ai_live_question 开关）。与体验一道题共用次数。
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { AIGenerating, Card, EmptyState, ErrorState, Icon, Loading, NavBar, Screen, Segmented, Tag, Text, toast } from '@/components';
import { Chips } from '@/features/onboarding/ui';
import { officialKeys, qtypeScore, useTrialStatus, type SampleQuestion } from '@/features/official/api';
import { track } from '@/lib/analytics';
import { api, unwrap } from '@/lib/api';
import { useFeatureFlag } from '@/lib/flags';

export default function Samples() {
  const { subjectId } = useLocalSearchParams<{ subjectId?: string }>();
  const samples = useQuery({ queryKey: officialKeys.samples, queryFn: () => unwrap(api.GET('/sample-questions')) });
  const status = useTrialStatus();
  const liveOn = useFeatureFlag('ai_live_question');
  const [discipline, setDiscipline] = useState<string>();
  const [direction, setDirection] = useState<number>();
  const [busy, setBusy] = useState<'live' | number>();

  const start = async (q: SampleQuestion, type: 'sample' | 'ai_live') => {
    setBusy(q.id);
    try {
      const t = await unwrap(api.POST('/trials', { body: { sample_question_id: q.id } }));
      track('trial_start', { type, input: 'sample' });
      router.replace({ pathname: '/trial/[id]', params: { id: String(t.id), subjectId } });
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '开始失败，请重试');
    } finally {
      setBusy(undefined);
    }
  };
  const live = async () => {
    if (!subjectId) return;
    setBusy('live');
    try {
      const q = await unwrap(api.POST('/sample-questions/live', { body: { subject_id: Number(subjectId) } }));
      await start(q, 'ai_live');
    } catch (e) {
      toast(e instanceof ApiError ? e.message : 'AI 出题失败，请重试');
      setBusy(undefined);
    }
  };

  if (samples.isLoading || status.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (samples.isError) return <Screen><ErrorState error={samples.error} onRetry={() => void samples.refetch()} /></Screen>;
  if (busy === 'live') return <Screen><AIGenerating title="AI 正在按你的专业课出一道题" steps={['读参考教材', '出题', '整理采分点']} current={1} eta="约 15 秒" /></Screen>;

  const list = samples.data?.items ?? [];
  const cur = list.find((d) => d.name === discipline) ?? list[0];
  const dir = cur?.directions.find((d) => d.id === direction) ?? cur?.directions[0];
  const remaining = status.data?.remaining ?? 0;
  const used = remaining <= 0;

  return (
    <Screen>
      <NavBar title="示例题体验" onBack={() => router.back()} right={<Tag label={`剩 ${remaining} 次`} tone="info" />} />
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="h1">选你的方向，挑一道试试</Text>
        <Text variant="caption">示例题由团队编写，只用于体验和入门练习，不计入预估分。</Text>
        {used ? <Text variant="body" color={semantic.danger}>体验次数已用完，导入你自己的资料就能继续练。</Text> : null}
        {cur ? (
          <>
            <Segmented options={list.map((d) => ({ key: d.name, label: d.name }))} value={cur.name} onChange={(v) => { setDiscipline(v); setDirection(undefined); }} />
            <Chips options={cur.directions.map((d) => d.id)} value={dir?.id ?? 0} onChange={setDirection} format={(id) => cur.directions.find((d) => d.id === id)?.name ?? ''} />
            <View style={styles.list}>
              {(dir?.questions ?? []).map((q, i) => (
                <Pressable key={q.id} accessibilityRole="button" disabled={used || !!busy} onPress={() => void start(q, 'sample')} style={[styles.row, i > 0 && styles.divider, used && styles.dim]}>
                  <View style={styles.flex}>
                    <Text variant="bodyStrong">{q.stem}</Text>
                    <Text variant="small">{qtypeScore(q.qtype, q.score)}{q.textbook ? ` · ${q.textbook}` : ''}</Text>
                  </View>
                  <Icon name="chevron" size={16} color={semantic.textSecondary} />
                </Pressable>
              ))}
            </View>
          </>
        ) : (
          <EmptyState title="示例题还在准备" desc="可以先用你手里的一道题体验，或导入自己的资料" />
        )}
        {liveOn && subjectId ? (
          <Card tone="fill" style={styles.live}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">我的专业不在这里</Text>
              <Text variant="small">AI 按你的专业课和参考教材现场出一道，标「AI 出题」</Text>
            </View>
            <Pressable accessibilityRole="button" disabled={used} onPress={() => void live()} style={styles.liveBtn}>
              <Text variant="bodyStrong" color={used ? semantic.disabledText : semantic.info}>出一道</Text>
            </Pressable>
          </Card>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xxl, gap: spacing.md },
  list: { borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, overflow: 'hidden' },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 64, paddingHorizontal: spacing.lg, paddingVertical: spacing.md },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  dim: { opacity: 0.5 },
  flex: { flex: 1, gap: 2 },
  live: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  liveBtn: { minWidth: 64, minHeight: 44, alignItems: 'center', justifyContent: 'center' },
});
