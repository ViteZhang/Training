import { radius, semantic, spacing } from '@training/ui-tokens';
import { StyleSheet, View, type ViewProps } from 'react-native';

/** 卡片：白底、线色描边、18 圆角（设计稿常用值）。 */
export function Card({ style, ...rest }: ViewProps) {
  return <View style={[styles.card, style]} {...rest} />;
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: semantic.surface,
    borderRadius: radius.xl,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: semantic.border,
    padding: spacing.lg,
  },
});
