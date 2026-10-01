import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import type { ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, Text } from '@/components';

/** 引导顶部：5 步进度条（PRD 1：1.1 到 1.5 分别为第 1–5 步）。 */
export function StepHeader({ step, title, desc, onBack }: { step: number; title: string; desc?: string; onBack?: () => void }) {
  return (
    <View style={styles.header}>
      <View style={styles.progressRow}>
        {onBack ? <Button title="返回" kind="text" onPress={onBack} style={styles.back} /> : <View style={styles.back} />}
        <View style={styles.bars} accessibilityLabel={`第 ${step} 步，共 5 步`}>
          {[1, 2, 3, 4, 5].map((i) => (
            <View key={i} style={[styles.bar, i <= step && styles.barOn]} />
          ))}
        </View>
        <Text variant="caption">{step} / 5</Text>
      </View>
      <Text variant="h1" style={styles.title}>
        {title}
      </Text>
      {desc ? (
        <Text variant="body" color={semantic.textSecondary}>
          {desc}
        </Text>
      ) : null}
    </View>
  );
}

/** 可选卡片（单选），选中时夜靛描边。 */
export function OptionCard({ selected, onPress, children, label }: { selected: boolean; onPress: () => void; children: ReactNode; label: string }) {
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ selected }}
      accessibilityLabel={label}
      onPress={onPress}
      style={[styles.option, selected && styles.optionOn]}
    >
      <View style={[styles.radio, selected && styles.radioOn]}>{selected ? <View style={styles.radioDot} /> : null}</View>
      <View style={styles.flex}>{children}</View>
    </Pressable>
  );
}

/** 胶囊选择（如每日时长、满分）。 */
export function Chips<T extends string | number>({ options, value, onChange, format }: { options: T[]; value: T; onChange: (v: T) => void; format?: (v: T) => string }) {
  return (
    <View style={styles.chips}>
      {options.map((o) => {
        const on = o === value;
        return (
          <Pressable key={String(o)} accessibilityRole="radio" accessibilityState={{ selected: on }} onPress={() => onChange(o)} style={[styles.chip, on && styles.chipOn]}>
            <Text variant="bodyStrong" color={on ? '#FFFFFF' : semantic.textPrimary}>
              {format ? format(o) : String(o)}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

/** ± 步进器（目标分）。 */
export function Stepper({ value, min, max, step = 5, onChange, suffix }: { value: number; min: number; max: number; step?: number; onChange: (v: number) => void; suffix?: string }) {
  return (
    <View style={styles.stepper}>
      <Pressable accessibilityRole="button" accessibilityLabel="减少" disabled={value <= min} onPress={() => onChange(Math.max(min, value - step))} style={[styles.stepBtn, value <= min && styles.dim]}>
        <Text variant="h2">−</Text>
      </Pressable>
      <Text variant="number" style={styles.stepValue}>
        {value}
        {suffix ? <Text variant="caption"> {suffix}</Text> : null}
      </Text>
      <Pressable accessibilityRole="button" accessibilityLabel="增加" disabled={value >= max} onPress={() => onChange(Math.min(max, value + step))} style={[styles.stepBtn, value >= max && styles.dim]}>
        <Text variant="h2">＋</Text>
      </Pressable>
    </View>
  );
}

export function Footer({ children }: { children: ReactNode }) {
  return <View style={styles.footer}>{children}</View>;
}

const styles = StyleSheet.create({
  header: { marginTop: spacing.sm, marginBottom: spacing.xl, gap: spacing.sm },
  progressRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.md },
  back: { minWidth: 52, paddingHorizontal: 0 },
  bars: { flex: 1, flexDirection: 'row', gap: 4 },
  bar: { flex: 1, height: 4, borderRadius: 2, backgroundColor: semantic.border },
  barOn: { backgroundColor: colors.indigo },
  title: { marginTop: spacing.md },
  option: { flexDirection: 'row', gap: spacing.md, alignItems: 'center', padding: spacing.lg, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, minHeight: layout.minTouch },
  optionOn: { borderColor: colors.indigo, borderWidth: 1.5 },
  radio: { width: 20, height: 20, borderRadius: 10, borderWidth: 1.5, borderColor: '#BDB8AD', alignItems: 'center', justifyContent: 'center' },
  radioOn: { borderColor: colors.indigo },
  radioDot: { width: 10, height: 10, borderRadius: 5, backgroundColor: colors.indigo },
  flex: { flex: 1 },
  chips: { flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap' },
  chip: { flexGrow: 1, minHeight: 40, minWidth: 64, paddingHorizontal: spacing.md, borderRadius: radius.pill, backgroundColor: '#F2EFE8', alignItems: 'center', justifyContent: 'center' },
  chipOn: { backgroundColor: colors.indigo },
  stepper: { flexDirection: 'row', alignItems: 'center', gap: spacing.lg },
  stepBtn: { width: layout.minTouch, height: layout.minTouch, borderRadius: radius.pill, borderWidth: 1, borderColor: semantic.border, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.surface },
  dim: { opacity: 0.35 },
  stepValue: { minWidth: 72, textAlign: 'center' },
  footer: { paddingVertical: spacing.lg, gap: spacing.sm },
});
