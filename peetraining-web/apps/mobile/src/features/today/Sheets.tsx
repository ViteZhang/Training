// 2.1e 进入新阶段提示、2.1f 修改目标分。
import type { Schemas } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Text, toast } from '@/components';
import { stageInfo } from '@/features/onboarding/api';
import { Stepper } from '@/features/onboarding/ui';
import { mixOrder, stageDesc, useAnswerStagePrompt, useSetTarget } from './api';

const pct = (m: Record<string, number>) => mixOrder.map((g) => Math.round((m[g.key] ?? 0) * 100)).join(' · ');

export function StagePromptSheet({ prompt, current, days }: { prompt: Schemas['StagePrompt']; current: Schemas['Stage']; days: number }) {
  const answer = useAnswerStagePrompt();
  const to = stageInfo[prompt.to].name;
  const respond = (accept: boolean) =>
    answer.mutate(accept, { onError: () => toast('没保存成功，请重试') });
  return (
    <BottomSheet visible onClose={() => respond(false)} dismissible={false} title={prompt.reason === 'early_sprint' ? `准备得不错，可以提前进入${to}` : `离初试还有 ${days} 天，进入${to}`}>
      <View style={styles.body}>
        <Text variant="body" color={semantic.textSecondary}>
          {stageDesc[prompt.to]}
        </Text>
        <Text variant="bodyStrong">今日计划的构成</Text>
        <View style={styles.mix}>
          <View style={styles.mixRow}>
            <Text variant="caption" style={styles.mixName}>
              {stageInfo[current].name}
            </Text>
            <Text variant="body">{pct(prompt.mix.current)}</Text>
          </View>
          <View style={styles.mixRow}>
            <Text variant="caption" style={styles.mixName}>
              {to}
            </Text>
            <Text variant="bodyStrong">{pct(prompt.mix.next)}</Text>
          </View>
          <Text variant="small">{mixOrder.map((g) => g.name).join(' · ')}　单位 %</Text>
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
              <Text variant="body">
                {s.code ? `${s.code} ` : ''}
                {s.name}
              </Text>
              {v !== null ? (
                <Stepper value={v} min={0} max={s.full_score} onChange={(n) => setDrafts((d) => ({ ...d, [s.id]: n }))} suffix={`/ ${s.full_score}`} />
              ) : (
                <Button title="设个目标分" kind="secondary" onPress={() => setDrafts((d) => ({ ...d, [s.id]: Math.round((s.full_score * 0.7) / 5) * 5 }))} />
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
  mix: { gap: spacing.xs },
  mixRow: { flexDirection: 'row', alignItems: 'center' },
  mixName: { width: 64 },
  subject: { gap: spacing.xs },
});
