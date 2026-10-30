import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Icon, Text } from '@/components';

/** 年份由服务端提供；研考年份可能比初试所在的自然年大一年。 */
export function ExamYearSelect({ items, value, onChange }: { items: Schemas['ExamYear'][]; value?: number; onChange: (year: number) => void }) {
  const [open, setOpen] = useState(false);
  const selected = items.find((item) => item.exam_year === value);
  return (
    <View style={styles.gap}>
      <Pressable accessibilityRole="button" accessibilityLabel="选择考试年份" accessibilityState={{ expanded: open, disabled: !items.length }} disabled={!items.length} onPress={() => setOpen(true)} style={styles.field}>
        <Text variant="bodyStrong" style={styles.flex}>{selected?.label ?? (value ? `${value} 研考` : '暂无可选考试年份')}</Text>
        <Icon name="down" size={18} />
      </Pressable>
      {selected ? <Text variant="small">专业课考试日期：{selected.subject_exam_date}</Text> : null}
      <BottomSheet visible={open} onClose={() => setOpen(false)} title="选择考试年份">
        <Text variant="caption" style={styles.hint}>按研考年份选择，下方显示实际考试日期。</Text>
        <ScrollView style={styles.list}>
          {items.map((item) => (
            <Pressable key={item.exam_year} accessibilityRole="radio" accessibilityLabel={item.label} accessibilityState={{ selected: item.exam_year === value }} onPress={() => { onChange(item.exam_year); setOpen(false); }} style={styles.option}>
              <View style={styles.flex}>
                <Text variant="bodyStrong">{item.label}</Text>
                <Text variant="small">专业课考试 {item.subject_exam_date}</Text>
              </View>
              {item.exam_year === value ? <Icon name="check" /> : null}
            </Pressable>
          ))}
        </ScrollView>
        <Button title="取消" kind="text" onPress={() => setOpen(false)} />
      </BottomSheet>
    </View>
  );
}
const styles = StyleSheet.create({
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  field: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 56, padding: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.inputBorder, backgroundColor: semantic.surface },
  hint: { marginVertical: spacing.sm },
  list: { maxHeight: 340 },
  option: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: 64, paddingVertical: spacing.md, borderBottomWidth: 1, borderBottomColor: semantic.border },
});
