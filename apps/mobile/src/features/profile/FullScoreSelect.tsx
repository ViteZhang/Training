import { radius, semantic, spacing } from '@training/ui-tokens';
import { Pressable, StyleSheet, View } from 'react-native';
import { Icon, Text } from '@/components';

export type FullScore = 100 | 150 | 300;
/** 满分单独成行，默认值直接展示为选中，无需再点击才能继续。 */
export function FullScoreSelect({ value, onChange }: { value: FullScore; onChange: (full: FullScore) => void }) {
  return (
    <View style={styles.gap}>
      <Text variant="caption">试卷满分（默认 150 分，可修改）</Text>
      <View style={styles.row} accessibilityRole="radiogroup">
        {([100, 150, 300] as const).map((full) => {
          const selected = value === full;
          return (
            <Pressable key={full} accessibilityRole="radio" accessibilityLabel={`满分 ${full}`} accessibilityState={{ selected }} onPress={() => onChange(full)} style={[styles.option, selected && styles.selected]}>
              <Text variant="bodyStrong" num color={selected ? semantic.textOnBrand : semantic.textPrimary}>{full} 分</Text>
              {selected ? <Icon name="check" size={16} color={semantic.textOnBrand} /> : null}
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}
const styles = StyleSheet.create({
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm },
  option: { flexDirection: 'row', alignItems: 'center', justifyContent: 'center', gap: 6, minHeight: 44, minWidth: 88, paddingHorizontal: spacing.md, paddingVertical: spacing.sm, borderRadius: radius.pill, borderWidth: 1, borderColor: semantic.inputBorder, backgroundColor: semantic.surface },
  selected: { backgroundColor: semantic.primary, borderColor: semantic.primary },
});
