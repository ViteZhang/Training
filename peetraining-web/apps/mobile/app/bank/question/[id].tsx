// 3.3 题目详情：题型、分值、来源；所属知识点；参考答案与出处（可查看原文）；采分点与状态；我的每次作答（日期、得分、遗漏的采分点、
// 失分类型）；「移出错题本」「再练这道题」；右上角编辑（题干、答案、分值、采分点，合计须等于分值）、删除。
import { ApiError, type Schemas } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, ConfirmDialog, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { bankKeys, shortDate, sourceLabel, useQuestion, type QuestionDetail } from '@/features/bank/api';
import { isSubjective, qtypeNames, rubricTotal } from '@/features/import/api';
import { RubricEditor } from '@/features/import/RubricEditor';
import { PageHeader } from '@/features/import/ui';
import { api, unwrap } from '@/lib/api';

const lossNames: Record<string, string> = { knowledge: '知识没掌握', norm: '答题不规范', time: '时间不够' };
const originNames: Partial<Record<Schemas['Origin'], string>> = { imported: '来自原文', ai_generated: 'AI 生成', ai_extracted: 'AI 提取 · 待确认', user_confirmed: '已确认' };

function Editor({ q, onDone }: { q: QuestionDetail; onDone: () => void }) {
  const qc = useQueryClient();
  const [stem, setStem] = useState(q.stem);
  const [answer, setAnswer] = useState(q.answer ?? '');
  const [score, setScore] = useState(q.score !== undefined ? String(q.score) : '');
  const [points, setPoints] = useState<Schemas['RubricPointInput'][]>(q.rubric_points.map((r) => ({ content: r.content, score: r.score, keywords: r.keywords })));
  const [busy, setBusy] = useState(false);
  const s = score.trim() === '' ? undefined : Number(score);
  const subjective = isSubjective({ qtype: q.qtype, stem });
  const mismatch = subjective && s !== undefined && points.some((p) => p.score) && rubricTotal(points) !== s;

  const save = async () => {
    setBusy(true);
    try {
      const rubricChanged = JSON.stringify(points) !== JSON.stringify(q.rubric_points.map((r) => ({ content: r.content, score: r.score, keywords: r.keywords })));
      const saved = await unwrap(
        api.PATCH('/questions/{questionId}', {
          params: { path: { questionId: q.id } },
          body: { stem, answer, score: s, rubric_points: rubricChanged ? points.filter((p) => p.content.trim()) : undefined, needs_review: false },
        }),
      );
      qc.setQueryData(bankKeys.question(q.id), saved);
      void qc.invalidateQueries({ queryKey: ['bank'] });
      onDone();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没保存成功，请重试');
    } finally {
      setBusy(false);
    }
  };
  return (
    <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
      <Text variant="bodyStrong">题干</Text>
      <TextInput accessibilityLabel="题干" value={stem} onChangeText={setStem} multiline style={[styles.input, styles.multi]} maxFontSizeMultiplier={layout.maxFontScale} />
      <Text variant="bodyStrong">分值</Text>
      <TextInput accessibilityLabel="分值" value={score} onChangeText={setScore} keyboardType="decimal-pad" style={styles.input} maxFontSizeMultiplier={layout.maxFontScale} />
      <Text variant="bodyStrong">参考答案</Text>
      <TextInput accessibilityLabel="参考答案" value={answer} onChangeText={setAnswer} multiline style={[styles.input, styles.multi]} maxFontSizeMultiplier={layout.maxFontScale} />
      {subjective ? <RubricEditor points={points} score={s} onChange={setPoints} /> : null}
      <Text variant="caption">改了采分点后，之后的批改按新采分点进行，之前的批改结果不变。</Text>
      <View style={styles.actions}>
        <Button title="取消" kind="secondary" onPress={onDone} style={styles.flex} />
        <Button title="保存" disabled={!stem.trim() || mismatch} loading={busy} onPress={() => void save()} style={styles.flex} />
      </View>
    </ScrollView>
  );
}

export default function QuestionScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const qid = Number(id);
  const qc = useQueryClient();
  const question = useQuestion(qid);
  const [editing, setEditing] = useState(false);
  const [removing, setRemoving] = useState(false);

  if (question.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (question.isError || !question.data) return <Screen><ErrorState error={question.error} onRetry={() => void question.refetch()} /></Screen>;
  const q = question.data;
  const primary = q.knowledge_points.find((k) => k.is_primary) ?? q.knowledge_points[0];

  const remove = async () => {
    try {
      await unwrap(api.DELETE('/questions/{questionId}', { params: { path: { questionId: qid } } }));
      await qc.invalidateQueries({ queryKey: ['bank'] });
      router.back();
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '删除没成功，请重试');
    }
  };

  return (
    <Screen>
      <PageHeader
        title={editing ? '编辑题目' : `${qtypeNames[q.qtype]}${q.score !== undefined ? ` · ${q.score} 分` : ''}`}
        desc={editing ? undefined : sourceLabel(q.source, q.exam_year)}
        onBack={() => router.back()}
        right={editing ? undefined : <Button title="编辑" kind="text" onPress={() => setEditing(true)} />}
      />
      {editing ? (
        <Editor q={q} onDone={() => setEditing(false)} />
      ) : (
        <ScrollView contentContainerStyle={styles.scroll}>
          <View style={styles.row}>
            {q.origin_tags.includes('ai_generated') ? <Tag label="AI 出题" tone="ai" /> : null}
            {q.needs_review ? <Tag label="待核对" tone="danger" /> : null}
            {q.in_wrong_book ? <Tag label="错题本" tone="danger" /> : null}
          </View>
          {primary ? (
            <Button title={`知识点 · ${primary.name} ›`} kind="text" style={styles.left} onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(primary.id) } })} />
          ) : null}
          <Text variant="h3">{q.stem}</Text>
          {q.options?.map((o) => (
            <Text key={o.key} variant="body">
              {o.key}. {o.text}
            </Text>
          ))}
          <Card style={styles.gap}>
            <View style={styles.row}>
              <Text variant="bodyStrong" style={styles.flex}>
                参考答案
              </Text>
              {q.answer_origin ? <Tag label={originNames[q.answer_origin] ?? ''} tone={q.answer_origin === 'ai_generated' ? 'ai' : 'neutral'} /> : null}
            </View>
            <Text variant="body">{q.answer ?? '还没有参考答案，点右上角「编辑」可以补上'}</Text>
            {q.source_ref ? (
              <View style={styles.row}>
                <Text variant="caption" style={styles.flex}>
                  出自：{q.source_ref.file_name}
                  {q.source_ref.page ? ` · 第 ${q.source_ref.page} 页` : ''}
                </Text>
                {q.source_ref.page ? (
                  <Button title="查看原文" kind="text" onPress={() => router.push({ pathname: '/bank/page', params: { materialId: String(q.source_ref!.material_id), page: String(q.source_ref!.page), highlight: q.stem.slice(0, 30) } })} />
                ) : null}
              </View>
            ) : null}
          </Card>
          {q.rubric_points.length > 0 ? (
            <Card style={styles.gap}>
              <View style={styles.row}>
                <Text variant="bodyStrong" style={styles.flex}>
                  采分点 · 批改依据
                </Text>
                <Text variant="caption">
                  {q.rubric_points.every((r) => r.origin === 'user_confirmed') ? '已确认' : '待确认'} · 合计 {rubricTotal(q.rubric_points)} 分
                </Text>
              </View>
              {q.rubric_points.map((r, i) => (
                <View key={r.id} style={styles.row}>
                  <Text variant="number" color={semantic.textSecondary}>
                    {String(i + 1).padStart(2, '0')}
                  </Text>
                  <Text variant="body" style={styles.flex}>
                    {r.content}
                  </Text>
                  {r.score !== undefined ? <Text variant="caption">{r.score} 分</Text> : null}
                </View>
              ))}
            </Card>
          ) : null}
          <Card style={styles.gap}>
            <Text variant="bodyStrong">我的作答 · {q.attempts.length} 次</Text>
            {q.attempts.length === 0 ? <Text variant="caption">还没做过这道题</Text> : null}
            {q.attempts.map((a) => (
              <View key={a.attempt_id} style={styles.attempt}>
                <Text variant="body">
                  {shortDate(a.answered_at)}
                  {a.score !== undefined ? ` · ${a.score}${a.full_score !== undefined ? ` / ${a.full_score}` : ''} 分` : a.is_correct !== undefined ? (a.is_correct ? ' · 答对' : ' · 答错') : ''}
                </Text>
                {a.missed_points?.length || a.loss_types?.length ? (
                  <Text variant="caption">
                    {[a.missed_points?.length ? `遗漏${a.missed_points.map((m) => `「${m}」`).join('')}` : '', ...(a.loss_types ?? []).map((t) => lossNames[t])].filter(Boolean).join(' · ')}
                  </Text>
                ) : null}
              </View>
            ))}
          </Card>
          <View style={styles.actions}>
            <Button title="删除这道题" kind="secondary" onPress={() => setRemoving(true)} style={styles.flex} />
            <Button title="再练这道题" onPress={() => toast('练习在训练模块上线后开放')} style={styles.flex} />
          </View>
        </ScrollView>
      )}
      <ConfirmDialog
        visible={removing}
        title="删除这道题？"
        message="会一起删除它的作答记录和错题记录。"
        confirmText="删除"
        danger
        onCancel={() => setRemoving(false)}
        onConfirm={() => {
          setRemoving(false);
          void remove();
        }}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { paddingBottom: spacing.xl, gap: spacing.md },
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' },
  flex: { flex: 1 },
  left: { alignSelf: 'flex-start' },
  attempt: { gap: 2, paddingVertical: spacing.xs, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  actions: { flexDirection: 'row', gap: spacing.sm },
  input: { minHeight: 44, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, paddingHorizontal: spacing.md, paddingVertical: spacing.xs, fontSize: 16, color: semantic.textPrimary, backgroundColor: semantic.surface },
  multi: { minHeight: 96, textAlignVertical: 'top' },
});
