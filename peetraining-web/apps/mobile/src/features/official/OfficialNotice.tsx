// 「已上线官方题库」公告横幅（2026-11-10 新增）：引导 1.1 选院校页、题库「添加官方题库」页顶部各放一条，
// 显示已上线的院校专业数，7 天内有上线或更新的标「新」；点进「已上线的官方题库」列表。受 official_bank 开关控制，没有官方题库时不显示。
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import { router } from 'expo-router';
import { Pressable, StyleSheet, View } from 'react-native';
import { Icon, Tag, Text } from '@/components';
import { api, unwrap } from '@/lib/api';
import { useFeatureFlag } from '@/lib/flags';

export const officialBanksKey = ['official-banks'] as const;

export function useOfficialBanks(enabled = true) {
  return useQuery({ queryKey: officialBanksKey, queryFn: () => unwrap(api.GET('/official-banks')), enabled });
}

/** 已上线的院校专业数（同一院校同一专业有多门课只算一个）。 */
export function countMajors(items: { school: string; major: string }[]) {
  return new Set(items.map((b) => `${b.school}\u0000${b.major}`)).size;
}

export function OfficialNotice({ from }: { from: 'onboarding' | 'bank' }) {
  const on = useFeatureFlag('official_bank');
  const banks = useOfficialBanks(on);
  const items = banks.data?.items ?? [];
  if (!on || items.length === 0) return null;
  const fresh = items.filter((b) => b.is_new).length;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="查看已上线的官方题库"
      onPress={() => router.push({ pathname: '/official/updates', params: { from } })}
      style={styles.banner}
    >
      <Icon name="bell" size={18} color={semantic.mastered} />
      <View style={styles.flex}>
        <Text variant="bodyStrong">已有 {countMajors(items)} 个院校专业上线官方题库</Text>
        <Text variant="small">{fresh > 0 ? `近 7 天新上线或更新 ${fresh} 门课` : '看看有没有你的院校专业'}</Text>
      </View>
      {fresh > 0 ? <Tag label="新" tone="mastered" style={styles.center} /> : null}
      <Icon name="chevron" size={16} color={semantic.textSecondary} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  banner: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 56, paddingHorizontal: spacing.md, paddingVertical: spacing.sm, borderRadius: radius.card, backgroundColor: semantic.masteredSoft },
  flex: { flex: 1, gap: 2 },
  center: { alignSelf: 'center' },
});
