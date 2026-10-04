// 1.1 你考哪门专业课：选考试年份；添加专业课（名称必填、代码选填），最多 4 门，至少 1 门才能下一步；
// 免费版最多 3 门，第 4 门提示开通会员；目标院校专业选填。
import { ApiError, type Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, ConfirmDialog, ErrorState, Loading, QuotaSheet, Screen, Text, toast } from '@/components';
import { setStep } from '@/features/onboarding/api';
import { loadDraft, saveDraft } from '@/features/onboarding/draft';
import { Footer, OptionCard, StepHeader } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

export default function SubjectStep() {
  const qc = useQueryClient();
  const years = useQuery({ queryKey: ['exam-years'], queryFn: () => unwrap(api.GET('/exam-years')) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const draft = loadDraft();
  const [year, setYear] = useState<number | undefined>(draft.examYear);
  const [name, setName] = useState('');
  const [code, setCode] = useState('');
  const [school, setSchool] = useState(draft.targetSchoolMajor ?? '');
  const [quota, setQuota] = useState(false);
  const [removing, setRemoving] = useState<Schemas['Subject'] | null>(null);
  const [busy, setBusy] = useState(false);

  const selectedYear = year ?? years.data?.items[0]?.exam_year;
  const list = subjects.data?.items ?? [];
  const full = list.length >= 4;

  const add = async () => {
    if (!name.trim()) return;
    if (subjects.data && !subjects.data.can_add && !full) {
      setQuota(true);
      return;
    }
    setBusy(true);
    try {
      await unwrap(api.POST('/subjects', { body: { name: name.trim(), code: code.trim() || null, full_score: 150 } }));
      track('subject_add', { from: 'onboarding', has_code: !!code.trim() });
      setName('');
      setCode('');
      await qc.invalidateQueries({ queryKey: ['subjects'] });
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) setQuota(true);
      else toast(e instanceof ApiError ? e.message : '添加失败，请重试');
    } finally {
      setBusy(false);
    }
  };

  const remove = async (s: Schemas['Subject']) => {
    try {
      await unwrap(api.DELETE('/subjects/{subjectId}', { params: { path: { subjectId: s.id } } }));
      await qc.invalidateQueries({ queryKey: ['subjects'] });
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '删除失败，请重试');
    }
  };

  const next = async () => {
    saveDraft({ examYear: selectedYear, targetSchoolMajor: school.trim() || undefined });
    await setStep('1.2');
    router.push('/(onboarding)/target');
  };

  if (years.isLoading || subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (years.isError || subjects.isError) {
    return (
      <Screen>
        <ErrorState error={years.error ?? subjects.error} onRetry={() => void Promise.all([years.refetch(), subjects.refetch()])} />
      </Screen>
    );
  }

  return (
    <Screen>
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.scroll}>
        <StepHeader step={1} title="你考哪门专业课？" desc="导入的资料会按专业课整理。目前支持以名词解释、简答、论述为主的文科专业课。" />

        <Text variant="bodyStrong" style={styles.label}>
          参加哪一年的考试
        </Text>
        <View style={styles.gap}>
          {years.data?.items.map((y) => (
            <OptionCard key={y.exam_year} label={y.label} selected={y.exam_year === selectedYear} onPress={() => setYear(y.exam_year)}>
              <Text variant="bodyStrong">{y.label}</Text>
              <Text variant="caption">专业课考试 {y.subject_exam_date}</Text>
            </OptionCard>
          ))}
        </View>

        <View style={styles.labelRow}>
          <Text variant="bodyStrong">专业课</Text>
          <Text variant="caption">最多 4 门</Text>
        </View>
        <View style={styles.gap}>
          {list.map((s) => (
            <Card key={s.id} style={styles.subjectRow}>
              <View style={styles.flex}>
                {s.code ? <Text variant="caption">{s.code}</Text> : null}
                <Text variant="bodyStrong">{s.name}</Text>
              </View>
              <Button title="删除" kind="text" onPress={() => setRemoving(s)} />
            </Card>
          ))}
          {!full ? (
            <Card style={styles.addCard}>
              <TextInput accessibilityLabel="专业课名称" placeholder="专业课名称，如：中国古代文学史" value={name} onChangeText={setName} style={styles.input} maxLength={64} maxFontSizeMultiplier={layout.maxFontScale} />
              <View style={styles.subjectRow}>
                <TextInput accessibilityLabel="专业课代码" placeholder="代码（选填），如 654" value={code} onChangeText={setCode} style={[styles.input, styles.flex]} maxLength={16} maxFontSizeMultiplier={layout.maxFontScale} />
                <Button title="添加" kind="secondary" disabled={!name.trim()} loading={busy} onPress={() => void add()} />
              </View>
              <Text variant="caption">不知道代码？只写名称也可以，比如「中国古代文学史」</Text>
            </Card>
          ) : null}
        </View>

        <View style={styles.labelRow}>
          <Text variant="bodyStrong">目标院校专业</Text>
          <Text variant="caption">选填</Text>
        </View>
        <TextInput accessibilityLabel="目标院校专业" placeholder="如：海南大学 · 中国语言文学" value={school} onChangeText={setSchool} style={styles.input} maxLength={64} maxFontSizeMultiplier={layout.maxFontScale} />
        <Text variant="caption" style={styles.hint}>
          以后收录了你的院校专业，会给你推送官方题库
        </Text>
      </ScrollView>
      <Footer>
        <Button title="下一步" disabled={list.length === 0 || !selectedYear} onPress={() => void next()} />
        {list.length === 0 ? (
          <Text variant="caption" style={styles.center}>
            至少添加一门专业课
          </Text>
        ) : null}
      </Footer>

      <QuotaSheet
        visible={quota}
        onClose={() => setQuota(false)}
        title="免费版最多添加 3 门专业课"
        desc="开通会员后可以添加第 4 门，资料解析、批改次数也不再受限。"
        onUpgrade={() => {
          setQuota(false);
          router.push('/member');
        }}
        freeOptions={[{ label: '先不加了', onPress: () => setQuota(false) }]}
      />
      <ConfirmDialog
        visible={!!removing}
        title="删除这门专业课？"
        message="会一起删除它的题库、资料和学习记录。"
        confirmText="删除"
        danger
        onCancel={() => setRemoving(null)}
        onConfirm={() => {
          if (removing) void remove(removing);
          setRemoving(null);
        }}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl },
  label: { marginBottom: spacing.sm },
  labelRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'baseline', marginTop: spacing.xl, marginBottom: spacing.sm },
  gap: { gap: spacing.sm },
  subjectRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  addCard: { gap: spacing.sm },
  flex: { flex: 1 },
  input: { minHeight: 48, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, fontSize: 16, color: semantic.textPrimary, backgroundColor: semantic.surface },
  hint: { marginTop: spacing.sm },
  center: { textAlign: 'center' },
});
