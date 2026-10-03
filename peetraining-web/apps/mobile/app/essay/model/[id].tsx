// 5.7 范文详情：只展示用户自己导入的范文；AI 做结构拆解（开头立意、分论点、升华、结尾），以批注形式标在原文旁，并说明「只有你自己能看到」。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { router, useLocalSearchParams } from 'expo-router';
import { ScrollView, StyleSheet, View } from 'react-native';
import { ErrorState, Loading, Screen, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { useModelEssay } from '@/features/essay/api';

export default function ModelEssayPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const model = useModelEssay(Number(id));
  if (model.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (model.isError || !model.data) return <Screen><ErrorState error={model.error} onRetry={() => void model.refetch()} /></Screen>;
  const m = model.data;
  const st = m.structure;
  const notes = [
    st?.opening ? { label: '开头立意', text: st.opening } : null,
    ...(st?.points ?? []).map((p, i) => ({ label: `分论点${'一二三四五六'[i] ?? i + 1}`, text: p })),
    st?.elevation ? { label: '升华', text: st.elevation } : null,
    st?.ending ? { label: '结尾', text: st.ending } : null,
  ].filter((x): x is { label: string; text: string } => x !== null);
  const paras = m.content.split(/\n+/).filter((p) => p.trim());
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title={`范文 · ${m.title}`} onBack={() => router.back()} />
        {m.topic ? <Text variant="caption">题目：{m.topic}</Text> : null}
        {notes.length > 0 ? (
          <View style={styles.tags}>
            {notes.map((n) => (
              <View key={n.label} style={styles.note}>
                <Text variant="small" color={colors.ink}>
                  {n.label}
                </Text>
                <Text variant="caption" color={colors.ink}>
                  {n.text}
                </Text>
              </View>
            ))}
          </View>
        ) : null}
        {paras.map((p, i) => (
          <Text key={i} variant="body" style={styles.para}>
            {p}
          </Text>
        ))}
        <Text variant="small" color={semantic.textSecondary}>
          来自你导入的范文{m.source_ref ? ` · ${m.source_ref.file_name}${m.source_ref.page ? ` 第 ${m.source_ref.page} 页` : ''}` : ''}。
          黄色批注是 AI 做的结构拆解，只有你自己能看到。
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  tags: { gap: spacing.sm },
  note: { gap: 2, padding: spacing.sm, borderRadius: radius.md, backgroundColor: semantic.amberSoft, borderLeftWidth: 3, borderLeftColor: colors.amber },
  para: { lineHeight: 28 },
});
