import type { Schemas } from '@training/api-client';
import { colors, radius, semantic } from '@training/ui-tokens';
import { StyleSheet, View } from 'react-native';
import { Text } from '@/components';
import { stateNames } from './api';

const tones: Record<Schemas['MasteryState'], { bg: string; fg: string; border?: string }> = {
  unlearned: { bg: 'transparent', fg: colors.gray, border: semantic.border },
  learning: { bg: semantic.fill, fg: colors.gray },
  consolidating: { bg: semantic.amberSoft, fg: '#8A4B12' },
  mastered: { bg: colors.indigo, fg: colors.white },
};

/** 掌握状态胶囊（设计稿 3.1、2.2）：未学习描边、学习中浅底、待巩固琥珀、已掌握夜靛实底。 */
export function MasteryPill({ state, suffix }: { state: Schemas['MasteryState']; suffix?: string }) {
  const t = tones[state];
  return (
    <View style={[styles.pill, { backgroundColor: t.bg }, t.border ? { borderWidth: 1, borderColor: t.border } : null]}>
      <Text variant="small" color={t.fg}>
        {stateNames[state]}
        {suffix ?? ''}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  pill: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill },
});
