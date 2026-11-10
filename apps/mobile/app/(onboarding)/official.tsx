// 1.1a 匹配到官方题库（PRD v3 增补，设计稿 n_official_found）：院校专业、官方题库版本、真题年份数、知识点数、模拟卷套数，
// 「所有用户免费使用，不需要上传任何资料」；同时选了没有官方题库的课时在下方列出，点进 1.1c。
// 主按钮「添加官方题库」→ 添加到对应的专业课 → 1.2 设目标分（不再先做一道真题，1.1b 不进）；次按钮「不用，我导入自己的资料」→ 1.4。
// 已经添加过（上次点过、接口返回 409）当作成功，直接往下走。
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Icon, Loading, Screen, Tag, Text, toast } from '@/components';
import { useCatalogCourses } from '@/features/official/api';
import { setStep } from '@/features/onboarding/api';
import { loadDraft, saveDraft } from '@/features/onboarding/draft';
import { Footer, StepHeader } from '@/features/onboarding/ui';
import { track } from '@/lib/analytics';
import { api, unwrap } from '@/lib/api';

export default function OfficialFound() {
  const qc = useQueryClient();
  const draft = loadDraft();
  const courses = useCatalogCourses(draft.school, draft.examYear);
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const [busy, setBusy] = useState(false);

  const picked = (courses.data?.items ?? []).filter((c) => (draft.picked ?? []).includes(c.id));
  const official = picked.filter((c) => c.official_status === 'available' && c.official);
  const others = draft.noOfficial ?? [];

  const start = async () => {
    setBusy(true);
    try {
      let added = 0;
      for (const c of official) {
        const bank = c.official!;
        const subject = subjects.data?.items.find((s) => s.catalog_course_id === c.id);
        if (!subject) continue;
        if (!bank.added_subject_id) {
          try {
            await unwrap(api.PUT('/official-banks/{bankId}/subscription', { params: { path: { bankId: bank.bank_id } }, body: { subject_id: subject.id } }));
            track('official_add', { entry: 'onboarding' });
          } catch (e) {
            // 上次已经加上了（列表还没刷新），不算失败
            if (!(e instanceof ApiError && e.status === 409)) throw e;
          }
        }
        added++;
      }
      if (!added) throw new Error('没有可添加的官方题库');
      saveDraft({ after: '/(tabs)/bank' });
      await setStep('1.2');
      router.push('/(onboarding)/target');
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '添加失败，请重试');
    } finally {
      await Promise.all([qc.invalidateQueries({ queryKey: ['catalog'] }), qc.invalidateQueries({ queryKey: ['subjects'] })]);
      setBusy(false);
    }
  };

  const importOwn = async () => {
    await setStep('1.4');
    router.push('/(onboarding)/import');
  };

  if (courses.isLoading || subjects.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (courses.isError) return <Screen><ErrorState error={courses.error} onRetry={() => void courses.refetch()} /></Screen>;

  const head = [draft.school, draft.major, draft.examYear ? `${draft.examYear} 研考` : ''].filter(Boolean).join(' · ');
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <StepHeader step={1} title="你的课有官方题库" desc={head} onBack={() => router.back()} />
        {official.map((c) => {
          const o = c.official!;
          return (
            <Card key={c.id} style={styles.card}>
              <View style={styles.titleRow}>
                <View style={styles.flex}>
                  <Text variant="h3">{c.subject_code ? `${c.subject_code} ` : ''}{c.subject_name}</Text>
                  <Text variant="small">官方题库 · v{o.version} · 团队编写审核</Text>
                </View>
                <Tag label="官方" tone="mastered" />
              </View>
              <View style={styles.stats}>
                <Stat value={o.exam_year_count} label="年真题" />
                <Stat value={o.kp_count} label="知识点" />
                <Stat value={o.paper_count} label="套模拟卷" />
              </View>
              <View style={styles.items}>
                {['每道真题都有参考答案和采分点', '考情分析、知识框架、背诵内容', '所有用户免费使用，不需要上传任何资料'].map((t) => (
                  <View key={t} style={styles.item}>
                    <Icon name="check" size={16} color={semantic.mastered} />
                    <Text variant="caption" color={semantic.textPrimary}>{t}</Text>
                  </View>
                ))}
              </View>
            </Card>
          );
        })}
        {others.map((o) => (
          <Pressable key={o.subjectId} accessibilityRole="button" onPress={() => router.push('/(onboarding)/no-official')} style={styles.other}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">{o.code ? `${o.code} ` : ''}{o.name}</Text>
              <Text variant="small">暂无官方题库 · 加完后可以体验 AI 批改或申请收录</Text>
            </View>
            <Icon name="chevron" size={16} color={semantic.textSecondary} />
          </Pressable>
        ))}
      </ScrollView>
      <Footer>
        <Button title="添加官方题库" loading={busy} disabled={official.length === 0} onPress={() => void start()} />
        <Button title="不用，我导入自己的资料" kind="text" disabled={busy} onPress={() => void importOwn()} />
      </Footer>
    </Screen>
  );
}

function Stat({ value, label }: { value: number; label: string }) {
  return (
    <View style={styles.stat}>
      <Text variant="number" num>{value}</Text>
      <Text variant="small">{label}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.lg },
  card: { gap: spacing.lg },
  titleRow: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.md },
  flex: { flex: 1, gap: 2 },
  stats: { flexDirection: 'row', gap: spacing.sm },
  stat: { flex: 1, alignItems: 'center', paddingVertical: spacing.md, borderRadius: radius.md, backgroundColor: semantic.fill },
  items: { gap: spacing.sm },
  item: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  other: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 64, padding: spacing.lg, borderRadius: radius.card, backgroundColor: semantic.fill },
});
