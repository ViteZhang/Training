// 5.9 评分标准：两种——「你的资料」（从用户资料识别出的维度、说明、分值与分档，注明出处，可编辑）和通用标准；可切换；
// 改了标准后新写的作文按新标准批改，已批改的分数不变；按你资料里的细则批改、真题限时完成的作文才计入预估分（PRD 11.13）。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Switch, TextInput, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { essayKeys, fmtScore, useRubrics } from '@/features/essay/api';
import { api, unwrap } from '@/lib/api';

type Rubric = Schemas['EssayRubric'];
type Dim = Schemas['EssayDimension'];

/** 正在使用的评分标准（设计稿 5.9）：大标题 + 来源 + 维度卡 + 分档。 */
function RubricMain({ r, subjectLabel }: { r: Rubric; subjectLabel?: string }) {
  const bands = r.dimensions.flatMap((d) => d.bands ?? []);
  return (
    <View style={styles.gap}>
      <Text variant="h2">
        {subjectLabel ? `${subjectLabel} · ` : ''}满分 {fmtScore(r.full_score)}
      </Text>
      <View style={styles.row}>
        <Tag label={r.source === 'generic' ? '通用标准' : '你的资料'} tone={r.source === 'generic' ? 'neutral' : 'mastered'} />
        <Text variant="small" style={styles.flex}>
          {r.name} · 满分 {fmtScore(r.full_score)}
          {r.source_ref ? ` · 从 ${r.source_ref.file_name}${r.source_ref.page ? ` 第 ${r.source_ref.page} 页` : ''}识别` : ''}
          {r.source === 'generic' ? ' · 分数只作参考，不计入预估分' : r.origin === 'user_confirmed' ? ' · 你改过' : ''}
        </Text>
      </View>
      <Card style={styles.dims}>
        {r.dimensions.map((d, i) => (
          <View key={d.name} style={[styles.dim, i > 0 && styles.divider]}>
            <View style={[styles.flex, styles.gap2]}>
              <Text variant="body">{d.name}</Text>
              {d.description ? <Text variant="small">{d.description}</Text> : null}
            </View>
            <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
              {fmtScore(d.score)}
            </Text>
          </View>
        ))}
      </Card>
      {bands.length > 0 ? (
        <View style={styles.gap}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
            分档
          </Text>
          <View style={styles.bands}>
            {bands.map((bd, i) => (
              <View key={i} style={styles.band}>
                <Text variant="small">{bd.description}</Text>
                <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
                  {bd.range}
                </Text>
              </View>
            ))}
          </View>
        </View>
      ) : null}
    </View>
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
  const subjects = useQuery({ queryKey: ['subjects'], queryFn: () => unwrap(api.GET('/subjects')) });
  const subj = subjects.data?.items?.find((x) => x.id === sid);
  const subjectLabel = subj ? `${subj.code ? `${subj.code} ` : ''}${subj.name}` : undefined;
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
        <PageHeader title="评分标准" onBack={() => router.back()} right={r.user && !editing ? <Button title="编辑" kind="text" size="sm" color={semantic.textPrimary} style={styles.link} onPress={() => setEditing(true)} /> : undefined} />
        {r.user && editing ? (
          <Editor r={r.user} onCancel={() => setEditing(false)} saving={save.isPending} onSave={(name, dims) => save.mutate({ name, dims })} />
        ) : null}
        {!editing ? <RubricMain r={usingUser && r.user ? r.user : r.generic} subjectLabel={subjectLabel} /> : null}
        {!r.user ? (
          <Card tone="fill" style={styles.gap}>
            <Text variant="small">还没有从你的资料里识别出评分细则。导入评分细则后，按你学校的标准批改，分数才计入预估分</Text>
            <Button title="导入评分细则" kind="secondary" onPress={() => router.push({ pathname: '/import', params: { subjectId: String(sid) } })} />
          </Card>
        ) : null}
        {r.user && !editing ? (
          <Pressable accessibilityRole="switch" accessibilityState={{ checked: !usingUser }} disabled={select.isPending} onPress={() => select.mutate(usingUser ? 'generic' : 'user_material')} style={styles.switchRow}>
            <View style={styles.flex}>
              <Text variant="body">{usingUser ? '改用通用标准' : '改用你资料里的标准'}</Text>
              <Text variant="small">{usingUser ? '觉得识别的细则不准时可以切回' : '按你学校的细则批改，分数才计入预估分'}</Text>
            </View>
            <Switch
              accessibilityLabel={usingUser ? '改用通用标准' : '改用你资料里的标准'}
              value={!usingUser}
              disabled={select.isPending}
              onValueChange={(v) => select.mutate(v ? 'generic' : 'user_material')}
              trackColor={{ true: semantic.primary, false: semantic.border }}
            />
          </Pressable>
        ) : null}
        <Text variant="small" style={styles.lh}>
          改了评分标准后，新写的作文按新标准批改，已批改的分数不变。按你资料里的细则批改、真题限时完成的作文，才会计入预估分。
        </Text>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: 14, paddingBottom: spacing.xl },
  gap: { gap: 10 },
  gap2: { gap: 2 },
  gapSm: { gap: spacing.xs },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 19 },
  link: { paddingHorizontal: 0 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  dims: { paddingVertical: 4 },
  dim: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 62, paddingVertical: 10 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  bands: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  band: { flexBasis: '22%', flexGrow: 1, gap: 2, padding: 10, borderRadius: 14, backgroundColor: semantic.fill },
  switchRow: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 60, borderTopWidth: 1, borderBottomWidth: 1, borderColor: semantic.border },
  editRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  input: { minHeight: 44, paddingHorizontal: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 16 },
  score: { width: 72, textAlign: 'center' },
});
