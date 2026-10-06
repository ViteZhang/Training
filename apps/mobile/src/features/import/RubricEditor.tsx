import type { Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { Icon, Text } from '@/components';
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
        <Text variant="caption" color={semantic.textPrimary} style={[styles.flex, styles.medium]}>
          采分点 · 批改依据
        </Text>
        <Text variant="caption" color={mismatch ? semantic.danger : semantic.mastered} accessibilityLabel={`合计 ${total} 分`}>
          合计 {total}
          {score !== undefined ? ` / ${score}` : ''} 分
        </Text>
      </View>
      <View style={styles.list}>
        {points.map((p, i) => (
          <View key={i} style={[styles.row, i > 0 && styles.divider]}>
            <TextInput
              accessibilityLabel={`第 ${i + 1} 个采分点`}
              value={p.content}
              placeholder="采分点内容"
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
            <Text variant="caption" color={semantic.textPrimary} style={styles.medium}>
              分
            </Text>
            <Pressable accessibilityRole="button" accessibilityLabel={`删除第 ${i + 1} 个采分点`} onPress={() => onChange(points.filter((_, j) => j !== i))} style={styles.remove}>
              <Icon name="close" size={16} color={semantic.textSecondary} />
            </Pressable>
          </View>
        ))}
        <Pressable accessibilityRole="button" onPress={() => onChange([...points, { content: '', score: 0 }])} style={[styles.row, styles.add, points.length > 0 && styles.divider]}>
          <Text variant="body">＋ 添加采分点</Text>
        </Pressable>
      </View>
      {mismatch ? (
        <Text variant="small" color={semantic.danger}>
          采分点分值合计要等于题目分值 {score} 分
        </Text>
      ) : (
        <Text variant="small">确认后 AI 批改会逐条对照</Text>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: 10 },
  head: { flexDirection: 'row', alignItems: 'baseline' },
  medium: { fontWeight: '500' },
  list: { borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, overflow: 'hidden' },
  row: { flexDirection: 'row', alignItems: 'center', gap: 4, minHeight: 52, paddingLeft: 16, paddingRight: 4 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  add: { minHeight: 48 },
  flex: { flex: 1 },
  input: { minHeight: 44, paddingVertical: spacing.xs, fontSize: 14, color: semantic.textPrimary },
  score: { width: 36, textAlign: 'right', fontWeight: '600' },
  remove: { width: 40, height: 44, alignItems: 'center', justifyContent: 'center' },
});
