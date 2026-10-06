import { semantic } from '@training/ui-tokens';
import { Pressable, StyleSheet, View } from 'react-native';
import { Icon, type IconName } from './Icon';
import { Text } from './Text';

export interface ActionItem {
  label: string;
  icon: IconName;
  onPress: () => void;
  danger?: boolean;
}

/** 操作菜单（设计稿 3.5 知识点更多操作）：图标 + 文字的列表行，行间细线，危险操作红色。 */
export function ActionList({ items }: { items: ActionItem[] }) {
  return (
    <View>
      {items.map((it, i) => (
        <Pressable key={it.label} accessibilityRole="button" onPress={it.onPress} style={[styles.row, i > 0 && styles.divider]}>
          <Icon name={it.icon} size={20} color={it.danger ? semantic.danger : semantic.textPrimary} />
          <Text variant="body" color={it.danger ? semantic.danger : semantic.textPrimary}>
            {it.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: 14, minHeight: 54 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
});
