// 4.19 选择作答模式：题数、满分、时长；模拟考试（严格计时、不能暂停、按建议用时提醒、交卷后有时间分析）与练习模式（可暂停离开、无提醒）；
// 按阶段推荐；各题型建议用时（PRD 11.9，按你真题的题型分值折算）。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, QuotaSheet, Screen, Tag, Text, toast } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { PageHeader } from '@/features/import/ui';
import { paperKeys, usePaper } from '@/features/paper/api';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

type Mode = Schemas['PaperMode'];

const modeCopy: Record<Mode, { title: string; lines: string[] }> = {
  mock: { title: '模拟考试模式', lines: ['严格计时，不能暂停', '按建议用时提醒，像真实考场一样分配时间', '交卷后有分数报告和时间分析报告'] },
  practice: { title: '练习模式', lines: ['可以暂停、离开，适合分段完成', '没有时间提醒', '交卷后有分数报告'] },
};

export default function PaperModePage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const qc = useQueryClient();
  const paper = usePaper(Number(id));
  const [mode, setMode] = useState<Mode>();
  const [quotaOut, setQuotaOut] = useState(false);
  const start = useMutation({
    mutationFn: (m: Mode) => unwrap(api.POST('/papers/{paperId}/sessions', { params: { path: { paperId: Number(id) } }, body: { mode: m, idempotency_key: Crypto.randomUUID() } })),
    onSuccess: (s) => {
      track('paper_start', { mode: s.mode });
      void qc.invalidateQueries({ queryKey: ['papers'] });
      qc.setQueryData(paperKeys.session(s.id), s);
      router.replace({ pathname: '/paper/session/[id]', params: { id: String(s.id) } });
    },
    onError: (e) => {
      if (e instanceof ApiError && e.isQuotaExceeded) setQuotaOut(true);
      else if (e instanceof ApiError && e.status === 409 && typeof e.detail?.session_id === 'number') {
        toast(e.message);
        router.replace({ pathname: '/paper/session/[id]', params: { id: String(e.detail.session_id) } });
      } else toast(e instanceof Error ? e.message : '没能开始，请重试');
    },
  });
  if (paper.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (paper.isError || !paper.data) return <Screen><ErrorState error={paper.error} onRetry={() => void paper.refetch()} /></Screen>;
  const p = paper.data;
  const chosen = mode ?? p.recommended_mode;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title={p.title} onBack={() => router.back()} />
        <View style={styles.facts}>
          <Text variant="caption">{p.question_count} 题</Text>
          <Text variant="caption">{p.full_score} 分</Text>
          <Text variant="caption">{p.duration_minutes} 分钟</Text>
        </View>
        {p.missing_note ? <Text variant="caption" color={semantic.info}>{p.missing_note}</Text> : null}
        {!p.counts_for_estimate ? <Text variant="caption">AI 组卷的成绩只作参考，不计入预估分</Text> : null}
        {(['mock', 'practice'] as const).map((m) => (
          <Pressable
            key={m}
            accessibilityRole="radio"
            accessibilityState={{ selected: chosen === m }}
            accessibilityLabel={modeCopy[m].title}
            onPress={() => setMode(m)}
            style={[styles.mode, chosen === m && styles.modeOn]}
          >
            <View style={styles.titleRow}>
              <Text variant="h3" style={styles.flex}>
                {modeCopy[m].title}
              </Text>
              {p.recommended_mode === m ? <Tag label="推荐" tone="mastered" /> : null}
            </View>
            {modeCopy[m].lines.map((l) => (
              <Text key={l} variant="small" color={semantic.textPrimary}>
                ·  {l}
              </Text>
            ))}
          </Pressable>
        ))}
        <Card style={styles.timeCard}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
            建议用时 · 按你真题的题型分值折算
          </Text>
          <View style={styles.times}>
          {p.sections.map((s) => (
            <View key={s.qtype} style={styles.time}>
              <Text variant="number">{s.suggested_minutes}′</Text>
              <Text variant="small">{qtypeNames[s.qtype]}</Text>
            </View>
          ))}
          <View style={styles.time}>
            <Text variant="number">{p.check_minutes}′</Text>
            <Text variant="small">检查</Text>
          </View>
          </View>
        </Card>
      </ScrollView>
      <View style={styles.footer}>
        <Button title={chosen === 'mock' ? '开始模拟考试' : '开始练习'} loading={start.isPending} onPress={() => start.mutate(chosen)} />
      </View>
      <QuotaSheet
        visible={quotaOut}
        onClose={() => setQuotaOut(false)}
        title="本周的整卷批改次数用完了"
        desc="免费版每周可批改 1 套整卷，下周一恢复。"
        onUpgrade={() => toast('会员马上上线')}
        freeOptions={[{ label: '先去练题', onPress: () => router.replace('/(tabs)/train') }]}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  facts: { flexDirection: 'row', gap: spacing.lg, marginTop: 4 },
  mode: { gap: 6, padding: 18, borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  modeOn: { borderColor: semantic.textPrimary, borderWidth: 2, padding: 17 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginBottom: 2 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  timeCard: { gap: 10 },
  times: { flexDirection: 'row', justifyContent: 'space-between' },
  time: { flex: 1, gap: 2 },
  footer: { paddingVertical: spacing.md },
});
