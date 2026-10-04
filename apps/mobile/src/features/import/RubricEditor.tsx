import type { Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { StyleSheet, TextInput, View } from 'react-native';
import { Button, Text } from '@/components';
import { rubricTotal } from './api';

type Point = Schemas['RubricPointInput'];

/** 采分点编辑（1.7b、3.6）：逐条改内容与分值、增删；显示合计 / 题目分值，不相等时标红。 */
export function RubricEditor({ points, score, onChange }: { points: Point[]; score?: number; onChange: (p: Point[]) => void }) {
  const total = rubricTotal(points);
  const mismatch = score !== undefined && points.length > 0 && total !== score;
  const set = (i: number, patch: Partial<Point>) => onChange(points.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  return (
    <View style={styles.wrap}>
      <View style={styles.head}>
        <Text variant="bodyStrong" style={styles.flex}>
          采分点 · 批改依据
        </Text>
        <Text variant="caption" color={mismatch ? semantic.danger : semantic.textSecondary} accessibilityLabel={`合计 ${total} 分`}>
          合计 {total}
          {score !== undefined ? ` / ${score}` : ''} 分
        </Text>
      </View>
      {points.map((p, i) => (
        <View key={i} style={styles.row}>
          <TextInput
            accessibilityLabel={`第 ${i + 1} 个采分点`}
            value={p.content}
            onChangeText={(v) => set(i, { content: v })}
            multiline
            style={[styles.input, styles.flex]}
            maxFontSizeMultiplier={layout.maxFontScale}
          />
          <TextInput
            accessibilityLabel={`第 ${i + 1} 个采分点的分值`}
            value={p.score === undefined ? '' : String(p.score)}
            onChangeText={(v) => {
              const n = Number(v.replace(/[^\d.]/g, ''));
              set(i, { score: v.trim() === '' || Number.isNaN(n) ? undefined : n });
            }}
            keyboardType="decimal-pad"
            style={[styles.input, styles.score]}
            maxFontSizeMultiplier={layout.maxFontScale}
          />
          <Button title="删" kind="text" accessibilityLabel={`删除第 ${i + 1} 个采分点`} onPress={() => onChange(points.filter((_, j) => j !== i))} />
        </View>
      ))}
      <Button title="添加采分点" kind="secondary" onPress={() => onChange([...points, { content: '', score: 0 }])} />
      {mismatch ? (
        <Text variant="caption" color={semantic.danger}>
          采分点分值合计要等于题目分值 {score} 分
        </Text>
      ) : (
        <Text variant="caption">确认后 AI 批改会逐条对照</Text>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: spacing.sm },
  head: { flexDirection: 'row', alignItems: 'baseline' },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs },
  flex: { flex: 1 },
  input: { minHeight: 44, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.sm, paddingVertical: spacing.xs, fontSize: 15, color: semantic.textPrimary, backgroundColor: semantic.surface },
  score: { width: 56, textAlign: 'center' },
});
