// 1.11 申请收录（PRD v3 增补，设计稿 n_apply）：院校、专业、专业课名称、考试年份必填（从 1.1 带入），专业课代码选填；
// 同一用户同一门课只能申请一次，重复提交提示「你已申请过」。入口：1.1c、题库页（3.1）、我的 → 备考设置（6.9）。
import { ApiError } from '@training/api-client';
import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, ErrorState, Loading, NavBar, Screen, Segmented, Text, toast } from '@/components';
import { loadDraft } from '@/features/onboarding/draft';
import { track } from '@/lib/analytics';
import { api, unwrap } from '@/lib/api';

type Entry = 'onboarding' | 'bank' | 'settings';

export default function Apply() {
  const params = useLocalSearchParams<{ entry?: Entry; subjectId?: string }>();
  const draft = loadDraft();
  const years = useQuery({ queryKey: ['exam-years'], queryFn: () => unwrap(api.GET('/exam-years')) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const profile = useQuery({ queryKey: ['profile'], queryFn: () => unwrap(api.GET('/profile')) });
  const subject = subjects.data?.items.find((s) => s.id === Number(params.subjectId));
  const fromDraft = draft.noOfficial?.[0];
  const [school, setSchool] = useState<string>();
  const [major, setMajor] = useState<string>();
  const [name, setName] = useState<string>();
  const [code, setCode] = useState<string>();
  const [year, setYear] = useState<number>();
  const [busy, setBusy] = useState(false);

  if (years.isLoading || subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (years.isError) return <Screen><ErrorState error={years.error} onRetry={() => void years.refetch()} /></Screen>;

  // 从 1.1 带入：先用这门专业课记下的院校与专业，再用引导草稿。
  const v = {
    school: school ?? subject?.school ?? draft.school ?? '',
    major: major ?? subject?.major ?? fromDraft?.major ?? draft.major ?? '',
    name: name ?? subject?.name ?? fromDraft?.name ?? '',
    code: code ?? subject?.code ?? fromDraft?.code ?? '',
    year: year ?? draft.examYear ?? profile.data?.exam_year ?? years.data?.items[0]?.exam_year,
  };
  const ok = !!v.school.trim() && !!v.major.trim() && !!v.name.trim() && !!v.year;

  const submit = async () => {
    if (!ok || !v.year) return;
    setBusy(true);
    const label = `${v.school} · ${v.major} · ${v.name}`;
    try {
      const r = await unwrap(
        api.POST('/course-applications', {
          body: { school: v.school.trim(), major: v.major.trim(), subject_name: v.name.trim(), subject_code: v.code.trim() || undefined, exam_year: v.year, entry: params.entry ?? 'onboarding' },
        }),
      );
      track('apply_submit', { entry: params.entry ?? 'onboarding', code: v.code.trim() || undefined });
      router.replace({ pathname: '/apply/done', params: { count: String(r.applicant_count), label, entry: params.entry ?? 'onboarding' } });
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        toast('你已申请过这门课');
        router.replace({ pathname: '/apply/done', params: { count: String(e.detail?.applicant_count ?? ''), label, entry: params.entry ?? 'onboarding', again: '1' } });
      } else {
        toast(e instanceof ApiError ? e.message : '提交失败，请重试');
      }
    } finally {
      setBusy(false);
    }
  };

  const field = (label: string, value: string, set: (s: string) => void, placeholder = '', hint?: string) => (
    <View style={styles.field}>
      <Text variant="caption" color={semantic.textPrimary} style={styles.label}>
        {label} {hint ? <Text variant="caption">{hint}</Text> : null}
      </Text>
      <TextInput
        accessibilityLabel={label}
        value={value}
        onChangeText={set}
        placeholder={placeholder}
        placeholderTextColor={colors.faint}
        style={styles.input}
        maxLength={64}
        maxFontSizeMultiplier={layout.maxFontScale}
      />
    </View>
  );

  return (
    <Screen>
      <NavBar title="申请收录" onBack={() => router.back()} />
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.scroll}>
        <Text variant="h1">申请收录你的专业课</Text>
        <Text variant="caption">同一门课申请的人越多，越早收录。</Text>
        {field('院校', v.school, setSchool, '院校全称')}
        {field('专业', v.major, setMajor)}
        {field('专业课名称', v.name, setName)}
        {field('专业课代码', v.code, setCode, '如 6xx', '选填')}
        <View style={styles.field}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.label}>考试年份</Text>
          <Segmented options={(years.data?.items ?? []).map((y) => ({ key: y.exam_year, label: `${y.exam_year} 研考` }))} value={v.year ?? 0} onChange={setYear} />
        </View>
      </ScrollView>
      <View style={styles.footer}>
        <Button title="提交申请" disabled={!ok} loading={busy} onPress={() => void submit()} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.lg },
  field: { gap: spacing.sm },
  label: { fontWeight: '500' },
  input: { minHeight: 50, paddingHorizontal: 16, borderRadius: radius.lg - 2, borderWidth: 1, borderColor: semantic.inputBorder, fontSize: 15, color: semantic.textPrimary },
  footer: { paddingTop: spacing.md, paddingBottom: spacing.xxl },
});
