// 2.1e 进入新阶段提示、2.1f 修改目标分。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Tag, Text, toast } from '@/components';
import { stageInfo } from '@/features/onboarding/api';
import { Stepper } from '@/features/onboarding/ui';
import { mixOrder, stageDesc, useAnswerStagePrompt, useSetTarget } from './api';

const pct = (m: Record<string, number>) => mixOrder.map((g) => Math.round((m[g.key] ?? 0) * 100)).join(' · ');

const mixColors: Record<string, string> = { new: colors.indigo, review: colors.blue, weak: colors.amber, recite: '#8C80E0' };

/** 构成对比的一行：阶段名 + 百分比 + 分段条。 */
function MixRow({ name, mix, strong }: { name: string; mix: Record<string, number>; strong?: boolean }) {
  return (
    <View style={styles.mixBlock}>
      <View style={styles.mixRow}>
        <Text variant="small" color={strong ? colors.ink : undefined} style={[styles.flex, strong && styles.bold]}>
          {name}
        </Text>
        <Text variant="small" color={strong ? colors.ink : undefined} style={strong ? styles.bold : undefined}>
          {pct(mix)}
        </Text>
      </View>
      <View style={styles.mixBar}>
        {mixOrder.map((g) => ((mix[g.key] ?? 0) > 0 ? <View key={g.key} style={{ flex: mix[g.key], backgroundColor: mixColors[g.key] }} /> : null))}
      </View>
    </View>
  );
}

export function StagePromptSheet({ prompt, current, days }: { prompt: Schemas['StagePrompt']; current: Schemas['Stage']; days: number }) {
  const answer = useAnswerStagePrompt();
  const to = stageInfo[prompt.to].name;
  const respond = (accept: boolean) =>
    answer.mutate(accept, { onError: () => toast('没保存成功，请重试') });
  return (
    <BottomSheet visible onClose={() => respond(false)} dismissible={false}>
      <View style={styles.body}>
        <Tag label={to} tone="outline" size="md" />
        <Text variant="h2">{prompt.reason === 'early_sprint' ? `准备得不错，可以提前进入${to}` : `离初试还有 ${days} 天，进入${to}`}</Text>
        <Text variant="caption" color={colors.ink} style={styles.lh}>
          {stageDesc[prompt.to]}
        </Text>
        <View style={styles.mix}>
          <Text variant="small">今日计划的构成</Text>
          <MixRow name={stageInfo[current].name} mix={prompt.mix.current} />
          <MixRow name={to} mix={prompt.mix.next} strong />
          <View style={styles.legend}>
            {mixOrder.map((g) => (
              <View key={g.key} style={styles.legendItem}>
                <View style={[styles.dot, { backgroundColor: mixColors[g.key] }]} />
                <Text variant="small">{g.name}</Text>
              </View>
            ))}
            <Text variant="small">单位 %</Text>
          </View>
        </View>
        <Button title="好的" loading={answer.isPending && answer.variables === true} onPress={() => respond(true)} />
        <Button title={`暂不切换，留在${stageInfo[current].name}`} kind="text" onPress={() => respond(false)} />
      </View>
    </BottomSheet>
  );
}

export function TargetSheet({ subjects, onClose }: { subjects: Schemas['Subject'][]; onClose: () => void }) {
  const setTarget = useSetTarget();
  const [drafts, setDrafts] = useState<Record<number, number | null>>(() => Object.fromEntries(subjects.map((s) => [s.id, s.target_score ?? null])));
  const save = async () => {
    try {
      for (const s of subjects) {
        const v = drafts[s.id] ?? null;
        if (v !== (s.target_score ?? null)) await setTarget.mutateAsync({ subjectId: s.id, target: v });
      }
      onClose();
    } catch {
      toast('保存失败，请重试');
    }
  };
  return (
    <BottomSheet visible onClose={onClose} title="修改目标分">
      <View style={styles.body}>
        <Text variant="caption">改完后，首页会按新目标算还差多少</Text>
        {subjects.map((s) => {
          const v = drafts[s.id] ?? null;
          return (
            <View key={s.id} style={styles.subject}>
              <Text variant="bodyStrong" style={styles.bold}>
                {s.code ? `${s.code} ` : ''}
                {s.name}
              </Text>
              {v !== null ? (
                <Stepper value={v} min={0} max={s.full_score} onChange={(n) => setDrafts((d) => ({ ...d, [s.id]: n }))} suffix={`/ ${s.full_score}`} />
              ) : (
                <Button title="设个目标分" kind="soft" size="sm" style={styles.start} onPress={() => setDrafts((d) => ({ ...d, [s.id]: Math.round((s.full_score * 0.7) / 5) * 5 }))} />
              )}
            </View>
          );
        })}
        <Button title="保存" loading={setTarget.isPending} onPress={() => void save()} />
        <Button title="取消" kind="text" onPress={onClose} />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  body: { gap: spacing.md },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 22 },
  mix: { gap: 10, padding: 16, borderRadius: radius.xl, backgroundColor: semantic.fill },
  mixBlock: { gap: 6 },
  mixRow: { flexDirection: 'row', alignItems: 'center' },
  mixBar: { flexDirection: 'row', gap: 2, height: 8, borderRadius: 4, overflow: 'hidden' },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: 10, alignItems: 'center' },
  legendItem: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  dot: { width: 8, height: 8, borderRadius: 4 },
  subject: { gap: spacing.md, padding: 16, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border },
  start: { alignSelf: 'flex-start' },
});
