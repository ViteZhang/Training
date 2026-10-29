// 3.3 题目详情：题型、分值、来源；所属知识点；参考答案、答案来源与出处（可查看原文）；「补充 / 修改答案」；采分点与状态；
// 我的每次作答（日期、得分、遗漏的采分点、失分类型）；「再练这道题」；右上角编辑（题干、答案、分值、采分点，合计须等于分值）、删除。
// 3.3b（PRD 11.16）：答案是 AI 参考答案时，顶部说明「你的资料里没有这道题的答案……确认前不计入预估分」并给「补充答案文件」；
// 答案与采分点标「AI 参考答案 · 待确认」「AI 生成」；显示按这份答案批改过的次数与分数；底部「我来改」「答案没问题，确认」。
import { ApiError, type Schemas } from '@training/api-client';
import { colors, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, ConfirmDialog, ErrorState, GroupNote, InfoCard, Loading, PassageCard, RubricLine, Screen, Tag, Text, toast } from '@/components';
import { bankKeys, shortDate, sourceLabel, useQuestion, type QuestionDetail } from '@/features/bank/api';
import { useSubjects } from '@/features/mine/api';
import { track } from '@/lib/analytics';
import { isSubjective, qtypeNames, rubricTotal } from '@/features/import/api';
import { RubricEditor } from '@/features/import/RubricEditor';
import { PageHeader } from '@/features/import/ui';
import { QuestionSourceChoices } from '@/features/import/QuestionSourceChoices';
import type { QuestionFileSource } from '@/features/import/files';
import { api, unwrap } from '@/lib/api';

const lossNames: Record<string, string> = { knowledge: '知识没掌握', norm: '答题不规范', time: '时间不够' };
// 答案档位的标注（PRD 11.16）
const tierNames: Partial<Record<Schemas['AnswerTier'], string>> = { material: '来自你的资料', confirmed: '你已确认' };

function Editor({ q, onDone }: { q: QuestionDetail; onDone: () => void }) {
  const qc = useQueryClient();
  const [stem, setStem] = useState(q.stem);
  const [answer, setAnswer] = useState(q.answer ?? '');
  const [score, setScore] = useState(q.score !== undefined ? String(q.score) : '');
  const [source, setSource] = useState<QuestionFileSource>(q.source === 'exam' || q.source === 'exercise' ? q.source : 'unconfirmed');
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
          body: { stem, answer, score: s, source: q.source === 'exam' || q.source === 'exercise' || q.source === 'unconfirmed' ? source : undefined, rubric_points: rubricChanged ? points.filter((p) => p.content.trim()) : undefined, needs_review: false },
        }),
      );
      qc.setQueryData(bankKeys.question(q.id), saved);
      void qc.invalidateQueries({ queryKey: ['bank'] });
      if (q.answer_tier === 'ai' && saved.answer_tier !== 'ai') track('ai_answer_edit');
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
      {q.source === 'exam' || q.source === 'exercise' || q.source === 'unconfirmed' ? (
        <QuestionSourceChoices label="题目来源" value={source} onChange={setSource} />
      ) : null}
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
  const { id, edit } = useLocalSearchParams<{ id: string; edit?: string }>();
  const qid = Number(id);
  const qc = useQueryClient();
  const question = useQuestion(qid);
  const [editing, setEditing] = useState(edit === '1');
  const [removing, setRemoving] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const subjects = useSubjects();

  if (question.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (question.isError || !question.data) return <Screen><ErrorState error={question.error} onRetry={() => void question.refetch()} /></Screen>;
  const q = question.data;
  const primary = q.knowledge_points.find((k) => k.is_primary) ?? q.knowledge_points[0];
  const tier = q.answer_tier ?? (q.answer ? 'material' : 'missing');
  const ai = tier === 'ai';
  const subject = subjects.data?.items?.find((x) => x.id === q.subject_id);
  const books = subject?.textbooks ?? [];
  const answerSrc = q.answer_source ?? (q.answer_match === 'original' ? q.source_ref : undefined);
  const openAnswerFile = () => router.push({ pathname: '/import/answers', params: { subject: String(q.subject_id ?? subject?.id ?? '') } });

  const confirmAI = async () => {
    setConfirming(true);
    try {
      const saved = await unwrap(api.POST('/questions/{questionId}/answer/confirm', { params: { path: { questionId: qid } } }));
      qc.setQueryData(bankKeys.question(qid), saved);
      void qc.invalidateQueries({ queryKey: ['bank'] });
      track('ai_answer_confirm');
      toast('已确认，之后的批改会计入预估分');
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '没确认成功，请重试');
    } finally {
      setConfirming(false);
    }
  };

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
        title={editing ? '编辑题目' : '题目详情'}
        onBack={() => router.back()}
        right={editing ? undefined : <Button title="编辑" kind="text" color={semantic.textPrimary} style={styles.edit} onPress={() => setEditing(true)} />}
      />
      {editing ? (
        <Editor q={q} onDone={() => setEditing(false)} />
      ) : (
        <>
          <ScrollView contentContainerStyle={styles.scroll}>
            <View style={styles.row}>
              <View style={styles.pill}>
                <Text variant="small" color={semantic.textPrimary} style={styles.medium}>
                  {qtypeNames[q.qtype]}
                  {q.score !== undefined ? ` · ${q.score} 分` : ''}
                </Text>
              </View>
              {q.origin_tags.includes('ai_generated') || q.source === 'ai_generated' ? (
                <Tag label="AI 出题" tone="ai" />
              ) : (
                <View style={[styles.pill, q.source === 'exam' && styles.pillGreen]}>
                  <Text variant="small" color={q.source === 'exam' ? colors.mintInk : semantic.textPrimary}>
                    {sourceLabel(q.source, q.exam_year)}
                  </Text>
                </View>
              )}
              {primary ? (
                <Pressable accessibilityRole="link" onPress={() => router.push({ pathname: '/bank/kp/[id]', params: { id: String(primary.id) } })} style={[styles.pill, styles.pillLine]}>
                  <Text variant="small" color={semantic.textPrimary}>
                    知识点 · {primary.name} ›
                  </Text>
                </Pressable>
              ) : null}
              {q.needs_review ? <Tag label="待核对" tone="danger" /> : null}
            </View>
            <GroupNote group={q.group} />
            {q.passage ? <PassageCard text={q.passage.text} /> : null}
            <Text variant="h1" style={styles.stem}>
              {q.stem}
            </Text>
            {q.options?.map((o) => (
              <Text key={o.key} variant="body">
                {o.key}. {o.text}
              </Text>
            ))}
            {ai ? (
              <View style={styles.aiNote}>
                <Text variant="caption" color={colors.blueInk} style={styles.lh2}>
                  <Text variant="caption" color={colors.blueInk} style={styles.bold}>
                    你的资料里没有这道题的答案
                  </Text>
                  ，下面是 AI 参考你的{books.length > 0 ? `教材${books.map((b) => (b.includes('《') ? b.replace(/^.*?(《.*》).*$/, '$1') : `《${b}》`)).join('')}` : '资料'}生成的，确认前不计入预估分
                </Text>
                <Button title="补充答案文件 ›" kind="text" size="sm" color={colors.blueInk} style={styles.link} onPress={openAnswerFile} />
              </View>
            ) : null}
            <InfoCard label="参考答案" style={ai ? styles.aiCard : undefined} right={ai ? <Tag label="AI 参考答案 · 待确认" tone="ai" /> : tierNames[tier]}>
              <Text variant="body" style={styles.lh}>
                {q.answer ?? '这道题还没有答案。做到时 AI 会给参考答案；也可以补充答案文件，或点「补充 / 修改答案」自己填'}
              </Text>
              {answerSrc ? (
                <View style={styles.sourceRow}>
                  <Text variant="small" style={styles.flex}>
                    {q.answer_match === 'other_material' ? '从你的其他资料找到：' : q.answer_match === 'answer_file' ? '来自答案文件：' : '出自：'}
                    {answerSrc.file_name}
                    {answerSrc.page ? ` · 第 ${answerSrc.page} 页` : ''}
                  </Text>
                  {answerSrc.page ? (
                    <Button
                      title="查看原文 ›"
                      kind="text"
                      size="sm"
                      color={semantic.textPrimary}
                      style={styles.link}
                      onPress={() => router.push({ pathname: '/bank/page', params: { materialId: String(answerSrc.material_id), page: String(answerSrc.page), highlight: (q.answer ?? q.stem).slice(0, 30) } })}
                    />
                  ) : null}
                </View>
              ) : null}
              {!ai ? (
                <View style={styles.sourceRow}>
                  <Button title={q.answer ? '修改答案' : '补充 / 修改答案'} kind="text" size="sm" color={semantic.info} style={styles.link} onPress={() => setEditing(true)} />
                  {!q.answer ? <Button title="补充答案文件" kind="text" size="sm" color={semantic.info} style={styles.link} onPress={openAnswerFile} /> : null}
                </View>
              ) : null}
            </InfoCard>
            {q.rubric_points.length > 0 ? (
              <InfoCard
                label="采分点 · 批改依据"
                right={
                  <Text variant="small" color={ai ? colors.lavenderInk : q.rubric_points.every((r) => r.origin === 'user_confirmed') ? semantic.mastered : colors.orange}>
                    {ai ? 'AI 生成' : q.rubric_points.every((r) => r.origin === 'user_confirmed') ? '已确认' : '待确认'} · 合计 {rubricTotal(q.rubric_points)} 分
                  </Text>
                }
              >
                {q.rubric_points.map((r, i) => (
                  <RubricLine key={r.id} index={i} content={r.content} score={r.score} />
                ))}
              </InfoCard>
            ) : null}
            {ai && q.reference_gradings && q.reference_gradings.count > 0 ? (
              <Text variant="small">
                按这份答案批改过 {q.reference_gradings.count} 次
                {q.reference_gradings.last_score !== undefined ? ` · 最近 ${q.reference_gradings.last_score} / ${q.reference_gradings.last_full_score ?? q.score ?? ''} 分` : ''}（不计入预估分）
              </Text>
            ) : null}
            <Text variant="caption" color={semantic.textPrimary} style={styles.medium}>
              我的作答 · {q.attempts.length} 次
            </Text>
            {q.attempts.length === 0 ? (
              <Text variant="small">还没做过这道题</Text>
            ) : (
              <Card style={styles.list}>
                {q.attempts.map((a, i) => (
                  <View key={a.attempt_id} style={[styles.attempt, i > 0 && styles.divider]}>
                    <View style={[styles.flex, styles.gap2]}>
                      <Text variant="caption" color={semantic.textPrimary}>
                        {shortDate(a.answered_at)}
                        <Text variant="caption" num={600} color={semantic.textPrimary}>
                          {a.score !== undefined ? ` · ${a.score}${a.full_score !== undefined ? ` / ${a.full_score}` : ''} 分` : a.is_correct !== undefined ? (a.is_correct ? ' · 答对' : ' · 答错') : ''}
                        </Text>
                      </Text>
                      {a.missed_points?.length || a.loss_types?.length ? (
                        <Text variant="small" color={colors.peachInk}>
                          {[a.missed_points?.length ? `遗漏${a.missed_points.map((m) => `「${m}」`).join('')}` : '', ...(a.loss_types ?? []).map((t) => lossNames[t])].filter(Boolean).join(' · ')}
                        </Text>
                      ) : null}
                    </View>
                  </View>
                ))}
              </Card>
            )}
            <Button title="删除这道题" kind="text" color={semantic.danger} style={styles.left} onPress={() => setRemoving(true)} />
          </ScrollView>
          <View style={styles.footer}>
            {ai ? (
              <View style={styles.actions}>
                <Button title="我来改" kind="secondary" onPress={() => setEditing(true)} style={styles.flex} />
                <Button title="答案没问题，确认" loading={confirming} onPress={() => void confirmAI()} style={styles.flex} />
              </View>
            ) : (
              <Button title="再练这道题" onPress={() => toast('练习在训练模块上线后开放')} />
            )}
          </View>
        </>
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
  scroll: { paddingBottom: spacing.xl, gap: 12 },
  gap: { gap: spacing.sm },
  gap2: { gap: 2 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 6, flexWrap: 'wrap', marginTop: spacing.sm },
  pill: { paddingHorizontal: 10, paddingVertical: 3, borderRadius: radius.pill, backgroundColor: semantic.fill },
  pillGreen: { backgroundColor: semantic.masteredSoft },
  pillLine: { backgroundColor: 'transparent', borderWidth: 1, borderColor: semantic.border },
  medium: { fontWeight: '500' },
  bold: { fontWeight: '700' },
  stem: { marginBottom: 4 },
  lh: { lineHeight: 26 },
  lh2: { lineHeight: 20 },
  aiCard: { borderWidth: 1.5, borderStyle: 'dashed', borderColor: colors.blueLine },
  aiNote: { gap: 4, paddingVertical: 12, paddingHorizontal: 14, borderRadius: 16, backgroundColor: colors.blueSoft },
  sourceRow: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  link: { paddingHorizontal: 0, minHeight: 32 },
  edit: { paddingHorizontal: 0 },
  flex: { flex: 1 },
  left: { alignSelf: 'flex-start', paddingHorizontal: 0 },
  list: { paddingVertical: 0 },
  attempt: { flexDirection: 'row', alignItems: 'center', minHeight: 58, paddingVertical: 10 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  footer: { paddingVertical: spacing.md },
  actions: { flexDirection: 'row', gap: spacing.sm },
  input: { minHeight: 44, borderWidth: 1, borderColor: semantic.inputBorder, borderRadius: radius.md, paddingHorizontal: spacing.md, paddingVertical: spacing.xs, fontSize: 15, color: semantic.textPrimary, backgroundColor: semantic.surface },
  multi: { minHeight: 96, textAlignVertical: 'top' },
});
