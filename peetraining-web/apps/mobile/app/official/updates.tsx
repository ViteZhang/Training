// 已上线的官方题库（2026-11-10 新增，入口：1.1 选院校页、题库「添加官方题库」页顶部的公告横幅）：
// 按院校分组列出已上线的专业与考试科目、版本和更新时间，7 天内上线或更新的标「新」，可按院校或专业搜索；
// 底部说明没有的院校专业可以自己导入资料，或申请收录（course_apply 开关）。
import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { useMemo, useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, EmptyState, ErrorState, Icon, Loading, Screen, Tag, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { useOfficialBanks } from '@/features/official/OfficialNotice';
import { useFeatureFlag } from '@/lib/flags';
import type { Schemas } from '@training/api-client';

type Bank = Schemas['OfficialBank'];

const md = (iso: string) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
};

/** 按院校分组，组内保持接口顺序（最近更新在前）；有「新」的院校排前面。 */
export function groupBySchool(items: Bank[], q: string) {
  const t = q.trim();
  const groups = new Map<string, Bank[]>();
  for (const b of items) {
    if (t && !b.school.includes(t) && !b.major.includes(t) && !b.subject_name.includes(t) && !b.subject_code.includes(t)) continue;
    groups.set(b.school, [...(groups.get(b.school) ?? []), b]);
  }
  return [...groups.entries()].sort(([, a], [, b]) => Number(b.some((x) => x.is_new)) - Number(a.some((x) => x.is_new)));
}

export default function OfficialUpdates() {
  const { from } = useLocalSearchParams<{ from?: 'onboarding' | 'bank' }>();
  const onboarding = from === 'onboarding';
  const banks = useOfficialBanks();
  const apply = useFeatureFlag('course_apply');
  const [q, setQ] = useState('');
  const groups = useMemo(() => groupBySchool(banks.data?.items ?? [], q), [banks.data, q]);

  if (banks.isPending) return <Screen><Loading rows={5} /></Screen>;
  if (banks.isError) return <Screen><ErrorState error={banks.error} onRetry={() => void banks.refetch()} /></Screen>;

  return (
    <Screen>
      <PageHeader title="已上线的官方题库" onBack={() => router.back()} />
      <ScrollView keyboardShouldPersistTaps="handled" contentContainerStyle={styles.body}>
        <Text variant="caption">官方题库由内容团队整理审核，所有用户免费使用。下面是已经上线的院校专业，标「新」的是近 7 天上线或更新的。</Text>
        <View style={styles.search}>
          <Icon name="search" size={18} color={semantic.textSecondary} />
          <TextInput
            accessibilityLabel="搜索院校或专业"
            placeholder="搜院校、专业或科目"
            placeholderTextColor={colors.faint}
            value={q}
            onChangeText={setQ}
            style={styles.input}
            maxLength={64}
            maxFontSizeMultiplier={layout.maxFontScale}
          />
        </View>
        {groups.length === 0 ? <EmptyState title={q.trim() ? '没有找到' : '暂时还没有官方题库'} desc="可以先导入自己的资料，或申请收录" /> : null}
        {groups.map(([school, list]) => (
          <Card key={school} style={styles.card}>
            <Text variant="h3">{school}</Text>
            {list.map((b, i) => (
              <View key={b.bank_id} style={[styles.row, i > 0 && styles.divider]}>
                <View style={styles.flex}>
                  <Text variant="bodyStrong">{b.subject_code ? `${b.subject_code} ` : ''}{b.subject_name}</Text>
                  <Text variant="small">
                    {[b.major, b.version ? `v${b.version.replace(/^v/i, '')}` : '', md(b.updated_at) ? `${md(b.updated_at)}更新` : ''].filter(Boolean).join(' · ')}
                  </Text>
                </View>
                {b.is_new ? <Tag label="新" tone="mastered" style={styles.center} /> : null}
              </View>
            ))}
          </Card>
        ))}
        <Card style={styles.card}>
          <Text variant="bodyStrong">没有你的院校专业？</Text>
          <Text variant="caption">
            {onboarding ? '可以先在上一页选好院校、添加专业课，下一步导入自己的资料，AI 会整理成你的题库；' : '可以先导入自己的资料，AI 会整理成你的题库；'}
            {apply ? '也可以申请收录，上线后第一时间通知你。' : '也可以在「我的 → 意见反馈」联系我们。'}
          </Text>
          <View style={styles.actions}>
            <Button title={onboarding ? '返回选院校' : '导入资料'} kind="secondary" style={styles.flex} onPress={() => (onboarding ? router.back() : router.push('/import'))} />
            {apply ? <Button title="申请收录" style={styles.flex} onPress={() => router.push({ pathname: '/apply', params: { entry: onboarding ? 'onboarding' : 'bank' } })} /> : null}
          </View>
        </Card>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { padding: spacing.md, gap: spacing.md, paddingBottom: spacing.xxl },
  card: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm },
  divider: { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.divider },
  flex: { flex: 1, gap: 2 },
  center: { alignSelf: 'center' },
  actions: { flexDirection: 'row', gap: spacing.sm, marginTop: spacing.xs },
  search: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: layout.minTouch, paddingHorizontal: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.inputBorder, backgroundColor: semantic.surface },
  input: { flex: 1, fontSize: 15, color: semantic.textPrimary, paddingVertical: spacing.sm },
});
