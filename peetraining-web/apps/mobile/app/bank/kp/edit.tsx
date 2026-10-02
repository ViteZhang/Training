// 3.6 编辑知识点：名称、原文表述、采分点、归属；只影响自己的题库。改了采分点后相关题目之后按新采分点批改，AI 解读重新生成。
import { ApiError, type Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { bankKeys, useKP, type KPDetail, type KnowledgeNode } from '@/features/bank/api';
import { PageHeader, Segments } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

type Point = Schemas['RubricPointInput'];

/** 可选的上级节点：知识点只能放在板块或章节下。 */
function parents(nodes: KnowledgeNode[], depth = 0): { id: number; label: string }[] {
  return nodes.flatMap((n) =>
    n.level === 'point' ? [] : [{ id: n.id, label: `${depth ? '　' : ''}${n.name}` }, ...parents(n.children, depth + 1)],
  );
}

function Form({ kp, subjectId }: { kp: KPDetail; subjectId?: number }) {
  const qc = useQueryClient();
  const [name, setName] = useState(kp.name);
  const [orig, setOrig] = useState(kp.original_text ?? '');
  const [points, setPoints] = useState<Point[]>(kp.rubric_points.map((r) => ({ content: r.content, keywords: r.keywords })));
  const [parent, setParent] = useState<number | undefined>(kp.parent_id);
  const [busy, setBusy] = useState(false);
  const tree = useQuery({
    queryKey: ['bank', subjectId ?? 0, 'tree', 'all'],
    queryFn: () => unwrap(api.GET('/subjects/{subjectId}/knowledge-tree', { params: { path: { subjectId: subjectId! }, query: { filter: 'all' } } })),
    enabled: !!subjectId && kp.level === 'point',
  });
  const options = parents(tree.data?.sections ?? []);

  const save = async () => {
    setBusy(true);
    try {
      const rubricChanged = JSON.stringify(points) !== JSON.stringify(kp.rubric_points.map((r) => ({ content: r.content, keywords: r.keywords })));
      const saved = await unwrap(
        api.PATCH('/knowledge-points/{kpId}', {
          params: { path: { kpId: kp.id } },
          body: {
            name: name.trim(),
            original_text: orig,
            parent_id: parent !== kp.parent_id ? parent : undefined,
            rubric_points: rubricChanged ? points.filter((p) => p.content.trim()) : undefined,
            needs_review: false,
          },
        }),
      );
      qc.setQueryData(bankKeys.kp(kp.id), saved);
      await qc.invalidateQueries({ queryKey: ['bank'] });
      router.back();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没保存成功，请重试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <PageHeader title="编辑知识点" onBack={() => router.back()} right={<Button title="保存" kind="text" loading={busy} disabled={!name.trim()} onPress={() => void save()} />} />
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        <Text variant="bodyStrong">名称</Text>
        <TextInput accessibilityLabel="名称" value={name} onChangeText={setName} maxLength={128} style={styles.input} maxFontSizeMultiplier={layout.maxFontScale} />
        {kp.level === 'point' ? (
          <>
            <Text variant="bodyStrong">原文表述</Text>
            <TextInput accessibilityLabel="原文表述" value={orig} onChangeText={setOrig} multiline style={[styles.input, styles.multi]} maxFontSizeMultiplier={layout.maxFontScale} />
            <Text variant="bodyStrong">采分点</Text>
            {points.map((p, i) => (
              <View key={i} style={styles.row}>
                <TextInput
                  accessibilityLabel={`第 ${i + 1} 个采分点`}
                  value={p.content}
                  onChangeText={(v) => setPoints(points.map((x, j) => (j === i ? { ...x, content: v } : x)))}
                  style={[styles.input, styles.flex]}
                  maxFontSizeMultiplier={layout.maxFontScale}
                />
                <Button title="删" kind="text" onPress={() => setPoints(points.filter((_, j) => j !== i))} />
              </View>
            ))}
            <Button title="+ 添加采分点" kind="text" onPress={() => setPoints([...points, { content: '' }])} />
            {options.length > 0 ? (
              <>
                <Text variant="bodyStrong">归属</Text>
                <Segments value={String(parent ?? '')} onChange={(v) => setParent(Number(v))} options={options.map((o) => ({ key: String(o.id), label: o.label.trim() }))} />
              </>
            ) : null}
          </>
        ) : null}
        <Text variant="caption" color={semantic.textSecondary}>
          只影响你自己的题库。改了采分点后，相关题目之后的批改按新采分点进行，AI 解读会重新生成。
        </Text>
      </ScrollView>
    </>
  );
}

export default function EditKP() {
  const { id, subjectId } = useLocalSearchParams<{ id: string; subjectId?: string }>();
  const kp = useKP(Number(id));
  if (kp.isLoading) return <Screen><Loading rows={5} /></Screen>;
  if (kp.isError || !kp.data) return <Screen><ErrorState error={kp.error} onRetry={() => void kp.refetch()} /></Screen>;
  return (
    <Screen>
      <Form kp={kp.data} subjectId={subjectId ? Number(subjectId) : undefined} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.sm },
  input: { minHeight: 44, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, paddingVertical: spacing.xs, fontSize: 16, color: semantic.textPrimary, backgroundColor: semantic.surface },
  multi: { minHeight: 96, textAlignVertical: 'top' },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs },
  flex: { flex: 1 },
});
