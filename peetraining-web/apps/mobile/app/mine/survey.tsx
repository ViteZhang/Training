// 6.14 考后回访：初试后推送；每门专业课填实际成绩，旁边显示考前预估（没有时显示「考前没有预估分」）；复试结果（进复试 / 没进 / 还不知道）；
// 录取结果（已录取 / 调剂 / 未录取 / 待定，可稍后补）；可选同意作为匿名上岸案例；提交后赠送 30 天会员（每人一次）。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { Checkbox, PageHeader } from '@/features/import/ui';
import { mineKeys } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';

const retests: { key: Schemas['RetestResult']; label: string }[] = [
  { key: 'in', label: '进复试' },
  { key: 'out', label: '没进' },
  { key: 'unknown', label: '还不知道' },
];
const admissions: { key: Schemas['Admission']; label: string }[] = [
  { key: 'admitted', label: '已录取' },
  { key: 'adjusted', label: '调剂' },
  { key: 'rejected', label: '未录取' },
  { key: 'pending', label: '待定' },
];

function Choice<T extends string>({ options, value, onChange }: { options: { key: T; label: string }[]; value?: T; onChange: (v: T) => void }) {
  return (
    <View style={styles.choices}>
      {options.map((o) => (
        <Pressable key={o.key} accessibilityRole="radio" accessibilityState={{ selected: value === o.key }} onPress={() => onChange(o.key)} style={[styles.choice, value === o.key && styles.choiceOn]}>
          <Text variant="caption" color={value === o.key ? semantic.textOnBrand : undefined}>
            {o.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

function Form({ sv }: { sv: Schemas['Survey'] }) {
  const qc = useQueryClient();
  const [scores, setScores] = useState<Record<number, string>>(() => Object.fromEntries(sv.subjects.map((s) => [s.subject_id, s.actual !== undefined ? String(s.actual) : ''])));
  const [retest, setRetest] = useState<Schemas['RetestResult'] | undefined>(sv.retest_result);
  const [admission, setAdmission] = useState<Schemas['Admission'] | undefined>(sv.admission);
  const [share, setShare] = useState(sv.share_consent);
  const submit = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST('/me/survey', {
          body: {
            scores: sv.subjects.filter((s) => scores[s.subject_id]?.trim()).map((s) => ({ subject_id: s.subject_id, score: Number(scores[s.subject_id]) })),
            retest_result: retest!,
            admission,
            share_consent: share,
          },
        }),
      ),
    onSuccess: (v) => {
      qc.setQueryData(mineKeys.survey, v);
      void qc.invalidateQueries({ queryKey: mineKeys.me });
      toast(sv.submitted ? '已更新' : `感谢填写，${sv.reward_days} 天会员已到账`);
      router.back();
    },
    onError: (e) => toast(e instanceof Error ? e.message : '提交失败，请重试'),
  });
  const filled = sv.subjects.some((s) => scores[s.subject_id]?.trim());
  return (
    <>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        <PageHeader title="考后回访" onBack={() => router.back()} />
        <Text variant="h2">初试辛苦了！</Text>
        <Text variant="caption">
          告诉我们你的成绩，帮我们把预估做得更准。{sv.submitted ? '可以随时补充复试和录取结果。' : `填写即送 ${sv.reward_days} 天会员`}
        </Text>
        {sv.subjects.map((s) => (
          <Card key={s.subject_id} style={styles.subject}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">{s.name}</Text>
              <Text variant="caption">{s.low !== undefined ? `考前预估 ${s.low}–${s.high}` : '考前没有预估分'}</Text>
            </View>
            <TextInput
              accessibilityLabel={`${s.name}实际成绩`}
              value={scores[s.subject_id]}
              editable={!sv.submitted}
              onChangeText={(v) => setScores((m) => ({ ...m, [s.subject_id]: v.replace(/[^\d.]/g, '') }))}
              keyboardType="numeric"
              placeholder={`/ ${s.full_score}`}
              placeholderTextColor={semantic.textSecondary}
              style={styles.score}
            />
          </Card>
        ))}
        <Text variant="bodyStrong">复试</Text>
        <Choice options={retests} value={retest} onChange={setRetest} />
        <Text variant="bodyStrong">最终录取（可稍后补充）</Text>
        <Choice options={admissions} value={admission} onChange={setAdmission} />
        <Checkbox checked={share} onChange={setShare} label="愿意把我的经历作为匿名上岸案例（不展示姓名和联系方式）" />
      </ScrollView>
      <Button title={sv.submitted ? '更新' : '提交'} disabled={!retest || !filled} loading={submit.isPending} onPress={() => submit.mutate()} />
    </>
  );
}

export default function SurveyPage() {
  const survey = useQuery({ queryKey: mineKeys.survey, queryFn: () => unwrap(api.GET('/me/survey')) });
  if (survey.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (survey.isError || !survey.data) return <Screen><ErrorState error={survey.error} onRetry={() => void survey.refetch()} /></Screen>;
  const sv = survey.data;
  if (!sv.open) {
    return (
      <Screen>
        <PageHeader title="考后回访" onBack={() => router.back()} />
        <EmptyState title="初试后开放" desc="考完初试后来填写实际成绩，送 30 天会员" />
      </Screen>
    );
  }
  return (
    <Screen>
      <Form key={String(sv.submitted)} sv={sv} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  flex: { flex: 1 },
  subject: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  score: { width: 88, minHeight: 44, textAlign: 'center', borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 18 },
  choices: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  choice: { minHeight: 40, justifyContent: 'center', paddingHorizontal: spacing.md, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border },
  choiceOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
});
