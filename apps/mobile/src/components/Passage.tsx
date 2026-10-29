// 多题共用的阅读材料与按专业分组的标记（服务端迁移 00020，2026-10-25 用户反馈）。
import type { Schemas } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Tag } from './Tag';
import { Text } from './Text';

/**
 * 共用材料卡：作答页、题目详情、核对页放在题干上方。默认显示前几行，「展开全文」看全部。
 * count 有值时标题写「共用材料 · N 题」（核对页给一组小题加的头部）。
 */
export function PassageCard({ text, count, lines = 4, defaultOpen = false }: { text: string; count?: number; lines?: number; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  const long = text.length > lines * 22;
  return (
    <View style={styles.card} accessibilityLabel="阅读材料">
      <View style={styles.head}>
        <Text variant="small" color={semantic.textPrimary} style={styles.title}>
          {count ? `共用材料 · ${count} 题` : '阅读材料'}
        </Text>
        <View style={styles.flex} />
        {long ? (
          <Pressable accessibilityRole="button" onPress={() => setOpen(!open)} hitSlop={10}>
            <Text variant="small" color={colors.link}>
              {open ? '收起' : '展开全文'}
            </Text>
          </Pressable>
        ) : null}
      </View>
      <Text variant="body" numberOfLines={open || !long ? undefined : lines} selectable>
        {text}
      </Text>
    </View>
  );
}

/** 分组标签：列表里只写组名，详情里带上适用专业。 */
export function GroupTag({ group, full, size }: { group?: Schemas['QuestionGroup'] | null; full?: boolean; size?: 'sm' | 'md' }) {
  if (!group) return null;
  const label = full && group.majors ? `${group.label} · ${group.majors}` : group.label;
  return <Tag label={label} tone="info" size={size} />;
}

/** 详情页题干上方的一行说明：只有报考这些专业的考生要答这道题。 */
export function GroupNote({ group }: { group?: Schemas['QuestionGroup'] | null }) {
  if (!group) return null;
  return (
    <Text variant="small" color={semantic.textSecondary}>
      按专业分组的题：{group.label}{group.majors ? `，${group.majors}专业的考生作答` : ''}
    </Text>
  );
}

const styles = StyleSheet.create({
  card: { gap: spacing.xs, paddingVertical: 12, paddingHorizontal: 14, borderRadius: radius.xl, backgroundColor: semantic.fill },
  head: { flexDirection: 'row', alignItems: 'center', minHeight: 20 },
  title: { fontWeight: '500' },
  flex: { flex: 1 },
});
