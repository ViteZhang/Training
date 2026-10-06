import { colors, radius, semantic } from '@training/ui-tokens';
import { StyleSheet, View } from 'react-native';
import { Text } from './Text';

export type TagTone = 'neutral' | 'brand' | 'ai' | 'mastered' | 'info' | 'danger' | 'progress' | 'outline';

const tones: Record<TagTone, { bg: string; fg: string; border?: string }> = {
  neutral: { bg: semantic.fill, fg: colors.gray },
  brand: { bg: semantic.primarySoft, fg: colors.indigo },
  // AI 生成的内容统一用这个样式标注来源（CLAUDE.md 必须遵守第 9 条），设计稿为细描边胶囊
  ai: { bg: 'transparent', fg: colors.gray, border: '#D6D2C8' },
  mastered: { bg: semantic.masteredSoft, fg: '#1F6B4A' },
  info: { bg: semantic.infoSoft, fg: colors.blue },
  danger: { bg: semantic.dangerSoft, fg: colors.red },
  progress: { bg: semantic.amberSoft, fg: '#8A5A00' },
  // 阶段标签（「强化期」）、推荐：墨色细描边
  outline: { bg: 'transparent', fg: colors.ink, border: colors.ink },
};

/** 标签：来源、状态、题型等小标记，胶囊形（设计稿 11 号字、2×8 内边距）。size="md" 用于阶段标签。 */
export function Tag({ label, tone = 'neutral', size = 'sm' }: { label: string; tone?: TagTone; size?: 'sm' | 'md' }) {
  const t = tones[tone];
  const md = size === 'md';
  return (
    <View style={[styles.tag, md && styles.md, { backgroundColor: t.bg }, t.border ? { borderWidth: 1, borderColor: t.border } : null]}>
      <Text variant="small" color={t.fg} style={[styles.text, md ? styles.mdText : null]}>
        {label}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  tag: { alignSelf: 'flex-start', paddingHorizontal: 8, paddingVertical: 1, borderRadius: radius.pill },
  md: { paddingHorizontal: 10, paddingVertical: 3 },
  text: { fontSize: 11, lineHeight: 16, fontWeight: '500' },
  mdText: { fontSize: 12, lineHeight: 17 },
});
