// 1.3 备考安排：显示距初试天数；按天数推荐阶段，用户可改；每日时长 30 / 45 / 60 / 90 分钟，默认 45。
import { colors, fontFamily, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { setStep, stageInfo, stages, type StageKey } from '@/features/onboarding/api';
import { loadDraft } from '@/features/onboarding/draft';
import { Chips, Footer, OptionCard, StepHeader } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

export default function SetupStep() {
  const years = useQuery({ queryKey: ['exam-years'], queryFn: () => unwrap(api.GET('/exam-years')) });
  const draft = loadDraft();
  const year = years.data?.items.find((y) => y.exam_year === draft.examYear) ?? years.data?.items[0];
  const [stage, setStage] = useState<StageKey | undefined>();
  const [minutes, setMinutes] = useState<30 | 45 | 60 | 90>(45);
  const [busy, setBusy] = useState(false);
  const chosen = stage ?? year?.suggested_stage;

  const next = async () => {
    if (!year || !chosen) return;
    setBusy(true);
    try {
      await unwrap(
        api.PUT('/profile', {
          body: { exam_year: year.exam_year, stage: chosen, daily_minutes: minutes, target_school_major: draft.targetSchoolMajor ?? null },
        }),
      );
      await setStep('1.4');
      router.push('/(onboarding)/import');
    } catch {
      toast('保存失败，请重试');
    } finally {
      setBusy(false);
    }
  };

  if (years.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (years.isError || !year) return <Screen><ErrorState error={years.error} onRetry={() => void years.refetch()} /></Screen>;

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <StepHeader step={3} title="备考安排" desc="按离初试的天数推荐了阶段，它决定每天练什么。之后随时能改。" onBack={() => router.back()} />
        <Card style={styles.countdown}>
          <Text variant="caption" color="#C4C0E0">
            距 {year.label}初试
          </Text>
          <View style={styles.daysRow}>
            <Text style={styles.days}>{year.days_to_exam}</Text>
            <Text variant="body" color="#FFFFFF">
              天
            </Text>
          </View>
          <Text variant="caption" color="#C4C0E0">
            初试日期以教育部公告为准
          </Text>
        </Card>

        <Text variant="bodyStrong" style={styles.label}>
          现在处于哪个阶段
        </Text>
        <View style={styles.gap}>
          {stages.map((k) => (
            <OptionCard key={k} label={stageInfo[k].name} selected={k === chosen} onPress={() => setStage(k)}>
              <View style={styles.stageRow}>
                <Text variant="bodyStrong">{stageInfo[k].name}</Text>
                {k === year.suggested_stage ? <Tag label="推荐" tone="progress" /> : null}
                <Text variant="caption" style={styles.range}>
                  {stageInfo[k].range}
                </Text>
              </View>
              <Text variant="caption">{stageInfo[k].desc}</Text>
            </OptionCard>
          ))}
        </View>

        <Text variant="bodyStrong" style={styles.label}>
          每天能练多久
        </Text>
        <Chips options={[30, 45, 60, 90] as const} value={minutes} onChange={setMinutes} format={(n) => `${n} 分钟`} />
      </ScrollView>
      <Footer>
        <Button title="下一步" loading={busy} disabled={!chosen} onPress={() => void next()} />
      </Footer>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl },
  countdown: { backgroundColor: colors.indigo, borderColor: colors.indigo, gap: spacing.xs },
  daysRow: { flexDirection: 'row', alignItems: 'baseline', gap: spacing.xs },
  days: { fontFamily: fontFamily.numberBold, fontSize: 48, lineHeight: 52, color: colors.amber },
  label: { marginTop: spacing.xl, marginBottom: spacing.sm },
  gap: { gap: spacing.sm },
  stageRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  range: { marginLeft: 'auto' },
});
