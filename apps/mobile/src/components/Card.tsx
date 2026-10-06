import { radius, semantic } from '@training/ui-tokens';
import { StyleSheet, View, type ViewProps } from 'react-native';

/** 卡片：白底、1px 线色描边、22 圆角、18 内边距（设计稿区块卡片）。tone="fill" 为浅底无描边的提示卡。 */
export function Card({ style, tone = 'surface', ...rest }: ViewProps & { tone?: 'surface' | 'fill' }) {
  return <View style={[styles.card, tone === 'fill' && styles.fill, style]} {...rest} />;
}

const styles = StyleSheet.create({
  card: {
    backgroundColor: semantic.surface,
    borderRadius: radius.card,
    borderWidth: 1,
    borderColor: semantic.border,
    padding: 18,
  },
  fill: { backgroundColor: semantic.fill, borderColor: semantic.fill },
});
