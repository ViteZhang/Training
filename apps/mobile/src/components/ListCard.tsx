import { semantic } from '@training/ui-tokens';
import { Children, isValidElement, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';
import { Card } from './Card';

/** 列表卡（设计稿 6.10 设置等）：行与行之间画细线，第一行上方不画。 */
export function ListCard({ children }: { children: ReactNode }) {
  const rows = Children.toArray(children).filter(isValidElement);
  return (
    <Card style={styles.card}>
      {rows.map((row, i) => (
        <View key={row.key ?? i} style={i > 0 ? styles.divider : undefined}>
          {row}
        </View>
      ))}
    </Card>
  );
}

const styles = StyleSheet.create({
  card: { paddingVertical: 0 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
});
