import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { StyleSheet, View } from 'react-native';
import { Text } from './Text';

export type TagTone = 'neutral' | 'brand' | 'ai' | 'mastered' | 'info' | 'danger' | 'progress';

const tones: Record<TagTone, { bg: string; fg: string }> = {
  neutral: { bg: '#F2EFE8', fg: colors.gray },
  brand: { bg: semantic.primarySoft, fg: colors.indigo },
  // AI 生成的内容统一用这个样式标注来源（CLAUDE.md 必须遵守第 9 条）
  ai: { bg: semantic.primarySoft, fg: colors.track },
  mastered: { bg: semantic.masteredSoft, fg: colors.green },
  info: { bg: semantic.infoSoft, fg: colors.blue },
  danger: { bg: semantic.dangerSoft, fg: colors.red },
  progress: { bg: semantic.amberSoft, fg: '#8A5A00' },
};

/** 标签：来源、状态、题型等小标记。 */
export function Tag({ label, tone = 'neutral' }: { label: string; tone?: TagTone }) {
  const t = tones[tone];
  return (
    <View style={[styles.tag, { backgroundColor: t.bg }]}>
      <Text variant="small" color={t.fg} style={styles.text}>
        {label}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  tag: { alignSelf: 'flex-start', paddingHorizontal: spacing.sm, paddingVertical: 2, borderRadius: radius.sm },
  text: { fontWeight: '600' },
});
