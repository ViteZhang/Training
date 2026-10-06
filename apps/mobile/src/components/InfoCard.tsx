import { fontFamily, semantic } from '@training/ui-tokens';
import type { ReactNode } from 'react';
import { StyleSheet, View, type ViewStyle } from 'react-native';
import { Card } from './Card';
import { Text } from './Text';

/** 详情页的内容卡：左上角小号灰色标签（「参考答案」「采分点 · 批改依据」），右侧可放说明或标签（设计稿 3.3、3.4）。 */
export function InfoCard({ label, right, children, tone, style }: { label: string; right?: ReactNode; children: ReactNode; tone?: 'surface' | 'fill'; style?: ViewStyle }) {
  return (
    <Card tone={tone} style={[styles.card, style]}>
      <View style={styles.head}>
        <Text variant="small" style={styles.flex}>
          {label}
        </Text>
        {typeof right === 'string' ? <Text variant="small">{right}</Text> : right}
      </View>
      {children}
    </Card>
  );
}

/** 采分点一行：两位序号 + 内容 + 分值。 */
export function RubricLine({ index, content, score }: { index: number; content: string; score?: number }) {
  return (
    <View style={styles.line}>
      <Text variant="caption" style={styles.no}>
        {String(index + 1).padStart(2, '0')}
      </Text>
      <Text variant="body" style={[styles.flex, styles.text]}>
        {content}
      </Text>
      {score !== undefined ? <Text variant="small">{score} 分</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  card: { gap: 10 },
  head: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  flex: { flex: 1 },
  line: { flexDirection: 'row', alignItems: 'baseline', gap: 10, minHeight: 28 },
  no: { fontFamily: fontFamily.numberSemiBold, color: semantic.textSecondary, minWidth: 18 },
  text: { fontSize: 14 },
});
