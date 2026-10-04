import type { Schemas } from '@training/api-client';
import { semantic } from '@training/ui-tokens';
import { StyleSheet } from 'react-native';
import { Text } from '@/components';

/** 把高亮与低置信度位置渲染成分段文字（位置按码点计）。 */
export function Marked({ text, highlights, low }: { text: string; highlights: Schemas['TextRange'][]; low: Schemas['TextRange'][] }) {
  const chars = Array.from(text);
  const kind = (i: number) => (highlights.some((r) => i >= r.start && i < r.end) ? 'hl' : low.some((r) => i >= r.start && i < r.end) ? 'low' : '');
  const parts: { t: string; k: string }[] = [];
  chars.forEach((c, i) => {
    const k = kind(i);
    const last = parts[parts.length - 1];
    if (last && last.k === k) last.t += c;
    else parts.push({ t: c, k });
  });
  return (
    <Text variant="body" style={styles.text}>
      {parts.map((p, i) => (
        <Text key={i} variant="body" style={p.k === 'hl' ? styles.hl : p.k === 'low' ? styles.low : undefined}>
          {p.t}
        </Text>
      ))}
    </Text>
  );
}


const styles = StyleSheet.create({
  text: { lineHeight: 28 },
  hl: { backgroundColor: semantic.amberSoft },
  low: { textDecorationLine: 'underline', textDecorationStyle: 'dashed', textDecorationColor: semantic.danger },
});
