// 1.7b 核对：显示出处（文件 + 页码）；参考答案标来源；主观题采分点逐条可改分值、可增删，合计须等于题目分值；
// 缺答案时可让 AI 生成参考答案（标「AI 生成」，计入 AI 出题次数）；可改知识点归属；「稍后再看」或「确认」。
// 知识点与作文资料条目可以确认或删除。
import { ApiError, type Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { AIGenerating, Button, Card, ConfirmDialog, ErrorState, Loading, QuotaSheet, Screen, Tag, Text, toast } from '@/components';
import { essayTypeNames, importKeys, isSubjective, qtypeNames, rubricTotal, sourceText, useImportItem, type ImportItem, type QuestionDraft } from '@/features/import/api';
import { RubricEditor } from '@/features/import/RubricEditor';
import { PageHeader } from '@/features/import/ui';
import { Footer } from '@/features/onboarding/ui';
import { api, unwrap } from '@/lib/api';

const originLabel: Partial<Record<Schemas['Origin'], string>> = { imported: '来自原文', ai_generated: 'AI 生成', user_confirmed: '你改过', ai_extracted: 'AI 提取' };

function QuestionEditor({ item, onSaved }: { item: ImportItem; onSaved: (it: ImportItem) => void }) {
  const qc = useQueryClient();
  const [q, setQ] = useState<QuestionDraft>(item.question!);
  const [kp, setKp] = useState((item.question!.kp_path ?? []).join(' / '));
  const [busy, setBusy] = useState<'save' | 'gen' | 'delete' | null>(null);
  const [quota, setQuota] = useState(false);
  const [removing, setRemoving] = useState(false);
  const subjective = isSubjective(q);
  const points = q.rubric_points ?? [];
  const mismatch = subjective && q.score !== undefined && points.length > 0 && rubricTotal(points) !== q.score;

  const patch = async (body: Schemas['ImportItemPatch']) => {
    const saved = await unwrap(api.PATCH('/import-items/{itemId}', { params: { path: { itemId: item.id } }, body }));
    qc.setQueryData(importKeys.item(item.id), saved);
    await qc.invalidateQueries({ queryKey: ['import-items', item.job_id] });
    return saved;
  };

  const save = async () => {
    setBusy('save');
    try {
      const path = kp
        .split(/[/／]/)
        .map((s) => s.trim())
        .filter(Boolean);
      onSaved(await patch({ status: 'confirmed', question: { ...q, kp_path: path.length ? path : undefined } }));
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没保存成功，请重试');
    } finally {
      setBusy(null);
    }
  };

  const generate = async () => {
    setBusy('gen');
    try {
      const saved = await unwrap(api.POST('/import-items/{itemId}/generate-answer', { params: { path: { itemId: item.id } } }));
      qc.setQueryData(importKeys.item(item.id), saved);
      if (saved.question) setQ(saved.question);
    } catch (e) {
      if (e instanceof ApiError && e.isQuotaExceeded) setQuota(true);
      else toast(e instanceof ApiError ? e.message : '生成失败，未扣除次数，请重试');
    } finally {
      setBusy(null);
    }
  };

  const remove = async () => {
    setBusy('delete');
    try {
      onSaved(await patch({ status: 'deleted' }));
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '删除没成功，请重试');
    } finally {
      setBusy(null);
    }
  };

  if (busy === 'gen') return <AIGenerating steps={['理解题目', '写参考答案', '提取采分点']} current={1} eta="约 20 秒" />;

  return (
    <>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        <View style={styles.meta}>
          <View style={styles.pill}>
            <Text variant="small" color={semantic.textPrimary} style={styles.medium}>
              {qtypeNames[q.qtype]}
              {q.score !== undefined ? ` · ${q.score} 分` : ''}
            </Text>
          </View>
          {item.source ? <Text variant="small">{sourceText(item.source)}</Text> : null}
        </View>
        <TextInput accessibilityLabel="题干" value={q.stem} onChangeText={(v) => setQ({ ...q, stem: v })} multiline style={styles.stem} maxFontSizeMultiplier={layout.maxFontScale} />
        {q.options?.map((o) => (
          <Text key={o.key} variant="body">
            {o.key}. {o.text}
          </Text>
        ))}

        <View style={styles.sectionHead}>
          <Text variant="caption" color={semantic.textPrimary} style={styles.medium}>
            参考答案
            {q.answer && q.answer_origin ? <Text variant="caption"> · {originLabel[q.answer_origin] ?? ''}</Text> : null}
          </Text>
          {q.answer && q.answer_origin === 'ai_generated' ? <Tag label="AI 生成" tone="ai" /> : null}
        </View>
        {q.answer ? (
          <TextInput accessibilityLabel="参考答案" value={q.answer} onChangeText={(v) => setQ({ ...q, answer: v })} multiline style={styles.input} maxFontSizeMultiplier={layout.maxFontScale} />
        ) : (
          <Card style={styles.missing}>
            <Text variant="caption">没找到参考答案。可以自己填，也可以让 AI 生成（标「AI 生成」，计入今天的 AI 出题次数）</Text>
            <TextInput accessibilityLabel="参考答案" placeholder="填写参考答案" value={q.answer ?? ''} onChangeText={(v) => setQ({ ...q, answer: v })} multiline style={styles.input} maxFontSizeMultiplier={layout.maxFontScale} />
            <Button title="让 AI 生成参考答案" kind="secondary" onPress={() => void generate()} />
          </Card>
        )}

        {subjective ? <RubricEditor points={points} score={q.score} onChange={(p) => setQ({ ...q, rubric_points: p })} /> : null}

        <Text variant="caption" color={semantic.textPrimary} style={[styles.section, styles.medium]}>
          知识点
        </Text>
        <TextInput accessibilityLabel="知识点归属" placeholder="板块 / 章节 / 知识点" value={kp} onChangeText={setKp} style={styles.input} maxFontSizeMultiplier={layout.maxFontScale} />
        <Button title="删除这道题" kind="text" color={semantic.danger} onPress={() => setRemoving(true)} style={styles.delete} />
      </ScrollView>
      <Footer>
        <View style={styles.actions}>
          <Button title="稍后再看" kind="secondary" onPress={() => router.back()} style={styles.flex} />
          <Button title="确认" style={styles.flex2} disabled={mismatch || !q.stem.trim()} loading={busy === 'save'} onPress={() => void save()} />
        </View>
      </Footer>
      <ConfirmDialog
        visible={removing}
        title="删除这道题？"
        message="它不会进入题库。"
        confirmText="删除"
        danger
        onCancel={() => setRemoving(false)}
        onConfirm={() => {
          setRemoving(false);
          void remove();
        }}
      />
      <QuotaSheet
        visible={quota}
        onClose={() => setQuota(false)}
        title="今天的 AI 出题次数用完了"
        desc="明天会恢复。开通会员后不限次数。"
        onUpgrade={() => {
          setQuota(false);
          router.push('/member');
        }}
        freeOptions={[{ label: '自己填写参考答案', onPress: () => setQuota(false) }]}
      />
    </>
  );
}

/** 知识点、作文资料：显示内容与出处，可确认或删除（编辑在题库里做）。 */
function OtherItem({ item, onSaved }: { item: ImportItem; onSaved: (it: ImportItem) => void }) {
  const qc = useQueryClient();
  const [busy, setBusy] = useState<'confirmed' | 'deleted' | null>(null);
  const set = async (status: 'confirmed' | 'deleted') => {
    setBusy(status);
    try {
      const saved = await unwrap(api.PATCH('/import-items/{itemId}', { params: { path: { itemId: item.id } }, body: { status } }));
      qc.setQueryData(importKeys.item(item.id), saved);
      await qc.invalidateQueries({ queryKey: ['import-items', item.job_id] });
      onSaved(saved);
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '操作没成功，请重试');
    } finally {
      setBusy(null);
    }
  };
  const kp = item.knowledge_point;
  const e = item.essay ?? {};
  return (
    <>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="caption">
          {kp ? `知识点 · ${kp.kp_path.join(' / ')}` : essayTypeNames[item.item_type]}
          {item.source ? ` · ${sourceText(item.source)}` : ''}
        </Text>
        {kp ? (
          <>
            <Text variant="h3">{kp.name}</Text>
            {kp.original_text ? <Text variant="body">{kp.original_text}</Text> : null}
            {kp.rubric_points?.map((p, i) => (
              <Text key={i} variant="caption">
                · {p.content}
              </Text>
            ))}
          </>
        ) : (
          <>
            <Text variant="h3">{String(e.title ?? e.name ?? e.theme ?? '')}</Text>
            {e.content ? <Text variant="body">{String(e.content)}</Text> : null}
            {Array.isArray(e.dimensions)
              ? (e.dimensions as { name: string; score: number; description?: string }[]).map((d) => (
                  <Text key={d.name} variant="body">
                    {d.name}（{d.score} 分）{d.description ? `：${d.description}` : ''}
                  </Text>
                ))
              : null}
            {e.ai_supplement ? <Tag label="AI 补充" tone="ai" /> : null}
          </>
        )}
      </ScrollView>
      <Footer>
        <View style={styles.actions}>
          <Button title="删除" kind="secondary" loading={busy === 'deleted'} onPress={() => void set('deleted')} style={styles.flex} />
          <Button title="确认" loading={busy === 'confirmed'} onPress={() => void set('confirmed')} style={styles.flex} />
        </View>
      </Footer>
    </>
  );
}

export default function ItemScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const item = useImportItem(Number(id));
  if (item.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (item.isError || !item.data) return <Screen><ErrorState error={item.error} onRetry={() => void item.refetch()} /></Screen>;
  const it = item.data;
  const done = () => router.back();
  return (
    <Screen>
      <PageHeader title={it.question ? '核对这道题' : '核对这一条'} onBack={() => router.back()} />
      {it.question ? <QuestionEditor item={it} onSaved={done} /> : <OtherItem item={it} onSaved={done} />}
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  meta: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', gap: 8, marginTop: spacing.sm },
  pill: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill, backgroundColor: semantic.fill },
  medium: { fontWeight: '500' },
  input: { minHeight: 48, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.xl, paddingHorizontal: 14, paddingVertical: 12, fontSize: 14, lineHeight: 24, color: semantic.textPrimary, backgroundColor: semantic.surface },
  stem: { fontSize: 20, lineHeight: 28, fontWeight: '700', color: semantic.textPrimary, padding: 0 },
  sectionHead: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginTop: spacing.sm },
  section: { marginTop: spacing.sm },
  missing: { gap: spacing.sm, backgroundColor: semantic.amberSoft, borderColor: '#F3DDB3' },
  actions: { flexDirection: 'row', gap: 10 },
  flex: { flex: 1 },
  flex2: { flex: 1.6 },
  delete: { alignSelf: 'flex-start', paddingHorizontal: 0 },
});
