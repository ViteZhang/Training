import { radius, semantic } from '@training/ui-tokens';
import { Pressable, StyleSheet, View } from 'react-native';
import { Text } from './Text';

/** 分段切换：浅底轨道 + 白色选中块（设计稿 0.4、3.1、6.2 等页面顶部的切换）。 */
export function Segmented<T extends string | number>({
  options,
  value,
  onChange,
}: {
  options: { key: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
}) {
  return (
    <View style={styles.track} accessibilityRole="tablist">
      {options.map((o) => {
        const on = o.key === value;
        return (
          <Pressable
            key={String(o.key)}
            accessibilityRole="tab"
            accessibilityState={{ selected: on }}
            onPress={() => onChange(o.key)}
            style={[styles.item, on && styles.itemOn]}
          >
            <Text variant="caption" color={on ? semantic.textPrimary : semantic.textSecondary} style={on ? styles.textOn : undefined}>
              {o.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  track: { flexDirection: 'row', padding: 4, borderRadius: radius.md + 2, backgroundColor: semantic.fill },
  item: { flex: 1, minHeight: 36, alignItems: 'center', justifyContent: 'center', borderRadius: 10 },
  itemOn: {
    backgroundColor: semantic.surface,
    shadowColor: '#000',
    shadowOpacity: 0.06,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
    elevation: 1,
  },
  textOn: { fontWeight: '500' },
});
