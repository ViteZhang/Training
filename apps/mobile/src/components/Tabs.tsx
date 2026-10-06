import { semantic } from '@training/ui-tokens';
import { Pressable, StyleSheet, View } from 'react-native';
import { Text } from './Text';

/** 下划线分栏（设计稿 3.1「知识点 · 题目 · 资料」）：选中项加粗、墨色下划线，数字跟在后面。 */
export function UnderlineTabs<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { key: T; label: string; count?: number }[];
  value: T;
  onChange: (v: T) => void;
}) {
  return (
    <View style={styles.bar} accessibilityRole="tablist">
      {options.map((o) => {
        const on = o.key === value;
        return (
          <Pressable key={o.key} accessibilityRole="tab" accessibilityState={{ selected: on }} onPress={() => onChange(o.key)} style={[styles.tab, on && styles.tabOn]}>
            <Text variant="body" color={on ? semantic.textPrimary : semantic.textSecondary} style={on ? styles.bold : undefined}>
              {o.label}
              {o.count !== undefined ? <Text variant="small"> {o.count}</Text> : null}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  bar: { flexDirection: 'row', gap: 22, borderBottomWidth: 1, borderBottomColor: semantic.border },
  tab: { minHeight: 44, justifyContent: 'center', borderBottomWidth: 2, borderBottomColor: 'transparent', marginBottom: -1 },
  tabOn: { borderBottomColor: semantic.textPrimary },
  bold: { fontWeight: '700' },
});
