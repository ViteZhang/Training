import { radius, semantic } from '@training/ui-tokens';
import { Pressable, StyleSheet, View } from 'react-native';
import { Text } from '@/components';
import type { QuestionFileSource } from './files';

const options: { key: QuestionFileSource; label: string }[] = [
  { key: 'exam', label: '历年真题' },
  { key: 'exercise', label: '习题' },
  { key: 'unconfirmed', label: '来源待确认' },
];

export function QuestionSourceChoices({
  value,
  label,
  onChange,
}: {
  value?: QuestionFileSource;
  label: string;
  onChange: (source: QuestionFileSource) => void;
}) {
  return (
    <View style={styles.picker}>
      <Text variant="small" color={semantic.textSecondary}>{label}</Text>
      <View style={styles.options} accessibilityRole="radiogroup">
        {options.map((option) => (
          <Pressable
            key={option.key}
            accessibilityRole="radio"
            accessibilityLabel={option.label}
            accessibilityState={{ checked: value === option.key }}
            onPress={() => onChange(option.key)}
            style={[styles.option, value === option.key && styles.optionOn]}
          >
            <Text variant="small" color={value === option.key ? semantic.textOnBrand : semantic.textPrimary}>
              {option.label}
            </Text>
          </Pressable>
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  picker: { gap: 6, paddingVertical: 10 },
  options: { flexDirection: 'row', gap: 6 },
  option: { flex: 1, minHeight: 44, paddingHorizontal: 6, alignItems: 'center', justifyContent: 'center', borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  optionOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
});
