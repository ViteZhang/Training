// 1.1 你考哪门专业课：选考试年份；添加专业课（名称必填、代码选填），最多 4 门，至少 1 门才能下一步；
// 免费版最多 3 门，第 4 门提示开通会员；目标院校专业选填。
import { ApiError, type Schemas } from '@training/api-client';
import { fontFamily, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, ConfirmDialog, ErrorState, Icon, Loading, QuotaSheet, Screen, Text, toast } from '@/components';
import { setStep } from '@/features/onboarding/api';
import { loadDraft, saveDraft } from '@/features/onboarding/draft';
import { Footer, StepHeader } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

export default function SubjectStep() {
  const qc = useQueryClient();
  const years = useQuery({ queryKey: ['exam-years'], queryFn: () => unwrap(api.GET('/exam-years')) });
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const draft = loadDraft();
  const [year, setYear] = useState<number | undefined>(draft.examYear);
  const [name, setName] = useState('');
  const [school, setSchool] = useState(draft.targetSchoolMajor ?? '');
  const [quota, setQuota] = useState(false);
  const [removing, setRemoving] = useState<Schemas['Subject'] | null>(null);
  const [busy, setBusy] = useState(false);

  const selectedYear = year ?? years.data?.items[0]?.exam_year;
  const list = subjects.data?.items ?? [];
  const full = list.length >= 4;

  // 设计稿 1.1：一个输入框写「代码 + 名称」，开头是 3 位数字时拆成代码
  const parsed = (() => {
    const t = name.trim();
    const m = /^(\d{3})\s*(.+)$/.exec(t);
    return m ? { code: m[1] ?? '', name: (m[2] ?? '').trim() } : { code: '', name: t };
  })();
  const add = async () => {
    if (!parsed.name) return;
    if (subjects.data && !subjects.data.can_add && !full) {
      setQuota(true);
      return;
    }
    setBusy(true);
    try {
      await unwrap(api.POST('/subjects', { body: { name: parsed.name, code: parsed.code || null, full_score: 150 } }));
      track('subject_add', { from: 'onboarding', has_code: !!parsed.code });
      setName('');
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

        <View style={styles.block}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.label}>
            参加哪一年的考试
          </Text>
          <View style={styles.years} accessibilityRole="radiogroup">
            {years.data?.items.map((y) => {
              const on = y.exam_year === selectedYear;
              return (
                <Pressable key={y.exam_year} accessibilityRole="radio" accessibilityState={{ selected: on }} accessibilityLabel={y.label} onPress={() => setYear(y.exam_year)} style={[styles.year, on && styles.yearOn]}>
                  <Text variant="caption" color={semantic.textPrimary} style={on ? styles.bold : undefined}>
                    {y.label}
                  </Text>
                  <Text variant="small" style={styles.yearSub}>
                    专业课考试 {y.subject_exam_date}
                  </Text>
                </Pressable>
              );
            })}
          </View>
        </View>

        <View style={styles.block}>
          <View style={styles.labelRow}>
            <Text variant="caption" color={semantic.textPrimary} style={styles.label}>
              专业课
            </Text>
            <Text variant="small">最多 4 门</Text>
          </View>
          <View style={styles.list}>
            {list.map((s) => (
              <View key={s.id} style={[styles.subjectRow, styles.divider]}>
                <Text variant="bodyStrong" style={styles.code}>
                  {s.code || '—'}
                </Text>
                <Text variant="body" style={styles.flex}>
                  {s.name}
                </Text>
                <Pressable accessibilityRole="button" accessibilityLabel="删除这门专业课" onPress={() => setRemoving(s)} style={styles.remove}>
                  <Icon name="close" size={18} color={semantic.textSecondary} />
                </Pressable>
              </View>
            ))}
            {!full ? (
              <View style={styles.subjectRow}>
                <TextInput
                  accessibilityLabel="添加专业课"
                  placeholder="代码 + 名称，如 654 语言文学基础"
                  placeholderTextColor="#A8A399"
                  value={name}
                  onChangeText={setName}
                  onSubmitEditing={() => void add()}
                  style={styles.input}
                  maxLength={64}
                  maxFontSizeMultiplier={layout.maxFontScale}
                />
                <Button title="添加" kind={parsed.name ? 'primary' : 'soft'} size="sm" disabled={!parsed.name} loading={busy} onPress={() => void add()} />
              </View>
            ) : null}
          </View>
          <Text variant="small">不知道代码？只写名称也可以，比如「中国古代文学史」</Text>
        </View>

        <View style={styles.block}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.label}>
            目标院校专业 <Text variant="caption">选填</Text>
          </Text>
          <View style={styles.school}>
            <TextInput
              accessibilityLabel="目标院校专业"
              placeholder="未填写"
              placeholderTextColor={semantic.textPrimary}
              value={school}
              onChangeText={setSchool}
              style={styles.schoolInput}
              maxLength={64}
              maxFontSizeMultiplier={layout.maxFontScale}
            />
            <Text variant="small">以后收录了你的院校专业，会给你推送官方题库</Text>
          </View>
        </View>
      </ScrollView>
      <Footer>
        <Button title={list.length === 0 ? '至少添加一门专业课' : '下一步'} disabled={list.length === 0 || !selectedYear} onPress={() => void next()} />
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
  scroll: { paddingBottom: spacing.xl, gap: 20 },
  block: { gap: 10 },
  label: { fontWeight: '500' },
  bold: { fontWeight: '700' },
  labelRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'baseline' },
  years: { flexDirection: 'row', gap: 4, padding: 4, borderRadius: 14, backgroundColor: semantic.fill },
  year: { flex: 1, minHeight: 52, alignItems: 'center', justifyContent: 'center', borderRadius: 10 },
  yearOn: { backgroundColor: semantic.surface },
  yearSub: { fontSize: 11, lineHeight: 15 },
  list: { borderRadius: radius.card, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, overflow: 'hidden' },
  subjectRow: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 56, paddingLeft: 18, paddingRight: 8 },
  divider: { borderBottomWidth: 1, borderBottomColor: semantic.border },
  code: { minWidth: 36, fontFamily: fontFamily.numberSemiBold },
  remove: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  flex: { flex: 1 },
  input: { flex: 1, minWidth: 0, minHeight: 44, fontSize: 15, color: semantic.textPrimary },
  school: { gap: 2, minHeight: 56, justifyContent: 'center', paddingVertical: 10, paddingHorizontal: 18, borderRadius: radius.xl, backgroundColor: semantic.fill },
  schoolInput: { fontSize: 14, minHeight: 24, padding: 0, color: semantic.textPrimary },
});
