import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import type { ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { BackButton, Text } from '@/components';

/** 引导顶部：5 步进度条（PRD 1：1.1 到 1.5 分别为第 1–5 步）。 */
export function StepHeader({ step, title, desc, onBack }: { step: number; title: string; desc?: string; onBack?: () => void }) {
  return (
    <View style={styles.header}>
      <View style={styles.progressRow}>
        {onBack ? <BackButton onPress={onBack} /> : <View style={styles.back} />}
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
        <Text variant="caption" style={styles.desc}>
          {desc}
        </Text>
      ) : null}
    </View>
  );
}

/** 可选卡片（单选），选中时 2px 夜靛描边（设计稿 1.3、1.4）。 */
export function OptionCard({ selected, onPress, children, label }: { selected: boolean; onPress: () => void; children: ReactNode; label: string }) {
  return (
    <Pressable
      accessibilityRole="radio"
      accessibilityState={{ selected }}
      accessibilityLabel={label}
      onPress={onPress}
      style={[styles.option, selected && styles.optionOn]}
    >
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
            <Text variant="caption" color={on ? '#FFFFFF' : semantic.textPrimary}>
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
        <Text variant="h2" style={styles.stepSign}>−</Text>
      </Pressable>
      <View style={styles.stepValue}>
        <Text variant="score" color={colors.ink}>
          {value}
        </Text>
        {suffix ? <Text variant="caption"> {suffix}</Text> : null}
      </View>
      <Pressable accessibilityRole="button" accessibilityLabel="增加" disabled={value >= max} onPress={() => onChange(Math.min(max, value + step))} style={[styles.stepBtn, value >= max && styles.dim]}>
        <Text variant="h2" style={styles.stepSign}>+</Text>
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
  back: { width: 32 },
  bars: { flex: 1, flexDirection: 'row', gap: 4 },
  bar: { flex: 1, height: 3, borderRadius: 2, backgroundColor: semantic.border },
  barOn: { backgroundColor: colors.ink },
  title: { marginTop: spacing.lg },
  desc: { fontSize: 14, lineHeight: 21 },
  option: { flexDirection: 'row', gap: spacing.md, alignItems: 'center', paddingVertical: 14, paddingHorizontal: 18, borderRadius: radius.xl, borderWidth: 2, borderColor: semantic.border, backgroundColor: semantic.surface, minHeight: layout.minTouch },
  optionOn: { borderColor: colors.indigo },
  flex: { flex: 1 },
  chips: { flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap' },
  chip: { minHeight: 40, minWidth: 64, paddingHorizontal: 14, borderRadius: radius.pill, backgroundColor: semantic.fill, alignItems: 'center', justifyContent: 'center' },
  chipOn: { backgroundColor: colors.indigo },
  stepper: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: spacing.sm },
  stepBtn: { width: layout.minTouch, height: layout.minTouch, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.fill },
  stepSign: { fontWeight: '400' },
  dim: { opacity: 0.35 },
  stepValue: { flex: 1, flexDirection: 'row', alignItems: 'baseline', justifyContent: 'center' },
  footer: { paddingVertical: spacing.lg, gap: spacing.sm },
});
