// 5.9 评分标准：两种——「你的资料」（从用户资料识别出的维度、说明、分值与分档，注明出处，可编辑）和通用标准；可切换；
// 改了标准后新写的作文按新标准批改，已批改的分数不变；按你资料里的细则批改、真题限时完成的作文才计入预估分（PRD 11.13）。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { essayKeys, fmtScore, useRubrics } from '@/features/essay/api';
import { api, unwrap } from '@/lib/api';

type Rubric = Schemas['EssayRubric'];
type Dim = Schemas['EssayDimension'];

function RubricCard({ r, active }: { r: Rubric; active: boolean }) {
  const bands = r.dimensions.flatMap((d) => d.bands ?? []);
  return (
    <Card style={styles.gap}>
      <View style={styles.row}>
        <Text variant="h3" style={styles.flex}>
          {r.source === 'generic' ? '通用标准' : '你的资料'}
        </Text>
        {active ? <Tag label="正在使用" tone="brand" /> : null}
      </View>
      <Text variant="caption">
        {r.name} · 满分 {fmtScore(r.full_score)}
        {r.source_ref ? ` · 从 ${r.source_ref.file_name}${r.source_ref.page ? ` 第 ${r.source_ref.page} 页` : ''}识别` : ''}
        {r.source === 'generic' ? ' · 分数只作参考，不计入预估分' : r.origin === 'user_confirmed' ? ' · 你改过' : ''}
      </Text>
      {r.dimensions.map((d) => (
        <View key={d.name} style={styles.dim}>
          <View style={styles.flex}>
            <Text variant="bodyStrong">{d.name}</Text>
            {d.description ? <Text variant="caption">{d.description}</Text> : null}
          </View>
          <Text variant="number">{fmtScore(d.score)}</Text>
        </View>
      ))}
      {bands.length > 0 ? (
        <View style={styles.gapSm}>
          <Text variant="caption">分档</Text>
          {bands.map((b, i) => (
            <Text key={i} variant="small">
              {b.range} · {b.description}
            </Text>
          ))}
        </View>
      ) : null}
    </Card>
  );
}

function Editor({ r, onCancel, onSave, saving }: { r: Rubric; onCancel: () => void; onSave: (name: string, dims: Dim[]) => void; saving: boolean }) {
  const [name, setName] = useState(r.name);
  const [dims, setDims] = useState<Dim[]>(r.dimensions.map((d) => ({ ...d })));
  const set = (i: number, patch: Partial<Dim>) => setDims((ds) => ds.map((d, j) => (j === i ? { ...d, ...patch } : d)));
  const full = dims.reduce((s, d) => s + (Number(d.score) || 0), 0);
  return (
    <Card style={styles.gap}>
      <Text variant="h3">编辑你的评分标准</Text>
      <TextInput accessibilityLabel="标准名称" value={name} onChangeText={setName} style={styles.input} />
      {dims.map((d, i) => (
        <View key={i} style={styles.editRow}>
          <TextInput accessibilityLabel={`维度 ${i + 1} 名称`} value={d.name} onChangeText={(v) => set(i, { name: v })} style={[styles.input, styles.flex]} />
          <TextInput
            accessibilityLabel={`维度 ${i + 1} 分值`}
            value={String(d.score)}
            keyboardType="numeric"
            onChangeText={(v) => set(i, { score: Number(v.replace(/[^\d.]/g, '')) || 0 })}
            style={[styles.input, styles.score]}
          />
          <Button title="删" kind="text" disabled={dims.length <= 1} onPress={() => setDims((ds) => ds.filter((_, j) => j !== i))} />
        </View>
      ))}
      <Button title="加一个维度" kind="text" disabled={dims.length >= 10} onPress={() => setDims((ds) => [...ds, { name: '', score: 10 }])} />
      <Text variant="caption">满分 = 各维度分值之和：{fmtScore(full)}</Text>
      <View style={styles.row}>
        <Button title="取消" kind="secondary" style={styles.flex} onPress={onCancel} />
        <Button title="保存" style={styles.flex} loading={saving} disabled={!name.trim() || dims.some((d) => !d.name.trim() || d.score <= 0)} onPress={() => onSave(name.trim(), dims)} />
      </View>
    </Card>
  );
}

export default function EssayRubricPage() {
  const { subjectId } = useLocalSearchParams<{ subjectId: string }>();
  const sid = Number(subjectId);
  const qc = useQueryClient();
  const rubrics = useRubrics(sid);
  const [editing, setEditing] = useState(false);
  const done = (r: Schemas['EssayRubrics']) => {
    qc.setQueryData(essayKeys.rubrics(sid), r);
    void qc.invalidateQueries({ queryKey: essayKeys.home(sid) });
  };
  const select = useMutation({
    mutationFn: (source: Schemas['EssayRubricSource']) => unwrap(api.PUT('/subjects/{subjectId}/essay-rubrics/active', { params: { path: { subjectId: sid } }, body: { source } })),
    onSuccess: (r) => {
      done(r);
      toast('已切换，之后新写的作文按它批改');
    },
    onError: (e) => toast(e instanceof Error ? e.message : '切换失败'),
  });
  const save = useMutation({
    mutationFn: (v: { name: string; dims: Dim[] }) =>
      unwrap(api.PUT('/subjects/{subjectId}/essay-rubrics/user', { params: { path: { subjectId: sid } }, body: { name: v.name, dimensions: v.dims } })),
    onSuccess: (r) => {
      done(r);
      setEditing(false);
      toast('已保存，之后新写的作文按新标准批改');
    },
    onError: (e) => toast(e instanceof Error ? e.message : '保存失败'),
  });
  if (rubrics.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (rubrics.isError || !rubrics.data) return <Screen><ErrorState error={rubrics.error} onRetry={() => void rubrics.refetch()} /></Screen>;
  const r = rubrics.data;
  const usingUser = !!r.user && r.active_id === r.user.id;
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        <PageHeader title="评分标准" onBack={() => router.back()} right={r.user && !editing ? <Button title="编辑" kind="text" onPress={() => setEditing(true)} /> : undefined} />
        {r.user && editing ? (
          <Editor r={r.user} onCancel={() => setEditing(false)} saving={save.isPending} onSave={(name, dims) => save.mutate({ name, dims })} />
        ) : null}
        {r.user && !editing ? <RubricCard r={r.user} active={usingUser} /> : null}
        {!r.user ? (
          <Card style={styles.gap}>
            <Text variant="caption">还没有从你的资料里识别出评分细则。导入评分细则后，按你学校的标准批改，分数才计入预估分</Text>
            <Button title="导入评分细则" kind="secondary" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />
          </Card>
        ) : null}
        <RubricCard r={r.generic} active={!usingUser} />
        {r.user ? (
          usingUser ? (
            <Button title="改用通用标准" kind="secondary" loading={select.isPending} onPress={() => select.mutate('generic')} />
          ) : (
            <Button title="改用你资料里的标准" loading={select.isPending} onPress={() => select.mutate('user_material')} />
          )
        ) : null}
        <Text variant="small" color={semantic.textSecondary}>
          改了评分标准后，新写的作文按新标准批改，已批改的分数不变。按你资料里的细则批改、真题限时完成的作文，才会计入预估分。
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  gapSm: { gap: spacing.xs },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  dim: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.xs, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  editRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  input: { minHeight: 44, paddingHorizontal: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 16 },
  score: { width: 72, textAlign: 'center' },
});
