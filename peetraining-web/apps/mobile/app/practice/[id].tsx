// 4.3 客观题答题：选中即判分（单选、判断），多选与填空点「提交」；「不会，看答案」计为未掌握；「题目有问题」可报错。
// 离线时用本地缓存的题与答案继续做、本地判分，联网后自动补交并以服务端复核为准。4.10 退出训练确认：进度保存，下次从断点继续。
import { semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';
import { BackButton, BottomSheet, Button, ConfirmDialog, EmptyState, ErrorState, Loading, NavBar, Screen, Text, toast } from '@/components';
import { practiceKeys, useSession } from '@/features/practice/api';
import { isObjective, judge } from '@/features/practice/judge';
import { cacheSession, submitAttempt } from '@/features/practice/offline';
import { DisputeSheet, GradingResultView, useRegrade } from '@/features/practice/grading';
import { FillBlank, Options, QuestionHeader, ResultPanel, type LocalResult } from '@/features/practice/QuestionView';
import { SubjectiveFlow } from '@/features/practice/SubjectiveFlow';
import { qtypeNames } from '@/features/import/api';
import { api, unwrap } from '@/lib/api';

const reportReasons = ['答案不对', '题干有错字或缺字', '选项有问题', '和知识点不相关'];

function clock(sec: number) {
  const m = Math.floor(sec / 60);
  const s = sec % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

export default function PracticeRunner() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const sessionId = Number(id);
  const qc = useQueryClient();
  const session = useSession(sessionId);
  const s = session.data;
  const [current, setIndex] = useState<number | null>(null);
  const [selected, setSelected] = useState<string[]>([]);
  const [text, setText] = useState('');
  const [results, setResults] = useState<Record<number, LocalResult>>({});
  const [reporting, setReporting] = useState(false);
  const [exiting, setExiting] = useState(false);
  const [finishing, setFinishing] = useState(false);
  const [disputing, setDisputing] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  const shownAt = useRef(0);

  useEffect(() => {
    const t = setInterval(() => setElapsed((e) => e + 1), 1000);
    return () => clearInterval(t);
  }, []);

  // 从断点继续：第一道还没做的题（4.10）。
  const resumeAt = useMemo(() => {
    if (!s) return 0;
    const first = s.questions.findIndex((x) => !x.answered);
    return first < 0 ? Math.max(0, s.questions.length - 1) : first;
    // 先用本地缓存定断点，拿到服务端最新的会话后再定一次；之后作答更新缓存不改变当前题。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [s?.id, session.isFetchedAfterMount]);
  const index = current ?? resumeAt;

  useEffect(() => {
    shownAt.current = Date.now();
  }, [index]);

  const q = s ? s.questions[index] : undefined;
  const regrade = useRegrade((g) => q && setResults((r) => ({ ...r, [q.id]: { ...(r[q.id] as LocalResult), grading: g } })));
  const result = q ? (results[q.id] ?? fromAnswered(q.answered)) : undefined;
  const done = useMemo(() => (s ? s.questions.filter((x) => x.answered || results[x.id]).length : 0), [s, results]);

  if (session.isLoading && !s) return <Screen><Loading rows={6} /></Screen>;
  if (!s) return <Screen><ErrorState error={session.error} onRetry={() => void session.refetch()} /></Screen>;
  if (s.questions.length === 0) return <Screen><EmptyState title="这组题都被删除了" actionText="返回" onAction={() => router.back()} /></Screen>;
  if (!q) return <Screen><Loading rows={6} /></Screen>;

  const objective = isObjective(q.qtype);

  const send = async (body: { selected?: string[]; answer_text?: string; revealed?: boolean; self_assess?: LocalResult['selfAssess'] }) => {
    const local = body.revealed || !objective ? null : judge(q.qtype, q.options, q.answer, body.selected ?? [], body.answer_text ?? '');
    const base: LocalResult = { correct: local, revealed: !!body.revealed, synced: false, selfAssess: body.self_assess };
    setResults((r) => ({ ...r, [q.id]: base }));
    const duration = Math.round((Date.now() - shownAt.current) / 1000);
    try {
      const res = await submitAttempt(sessionId, { question_id: q.id, idempotency_key: Crypto.randomUUID(), duration_seconds: duration, offline: false, ...body, revealed: !!body.revealed });
      // 本地缓存记下已作答，断网重开也从断点继续。
      const answered = { is_correct: res?.is_correct ?? local ?? undefined, revealed: !!body.revealed, self_assess: body.self_assess, selected: body.selected };
      const next = { ...s, questions: s.questions.map((x) => (x.id === q.id ? { ...x, answered } : x)) };
      cacheSession(next);
      if (res) {
        setResults((r) => ({ ...r, [q.id]: { ...base, correct: res.is_correct ?? local, synced: true, answer: res.correct_answer, analysis: res.analysis, wrongBook: res.wrong_book } }));
      }
    } catch (e) {
      setResults((r) => {
        const rest = { ...r };
        delete rest[q.id];
        return rest;
      });
      toast(e instanceof Error ? e.message : '提交失败，请重试');
    }
  };

  const toggle = (key: string) => {
    if (q.qtype === 'multi_choice') {
      setSelected((sel) => (sel.includes(key) ? sel.filter((k) => k !== key) : [...sel, key]));
      return;
    }
    setSelected([key]);
    void send({ selected: [key] });
  };

  const next = async () => {
    if (index + 1 < s.questions.length) {
      setIndex(index + 1);
      setSelected([]);
      setText('');
      return;
    }
    setFinishing(true);
    await qc.invalidateQueries({ queryKey: ['practice', 'home'] });
    router.replace({ pathname: '/practice/summary/[id]', params: { id: String(sessionId) } });
  };

  const exit = async () => {
    setExiting(false);
    try {
      await unwrap(api.PUT('/practice-sessions/{sessionId}/progress', { params: { path: { sessionId } }, body: { cursor_index: done } }));
    } catch {
      // 离线时断点已在本地缓存里（已作答的题），联网后再进来从第一道没做的题继续。
    }
    void qc.invalidateQueries({ queryKey: practiceKeys.session(sessionId) });
    void qc.invalidateQueries({ queryKey: ['practice', 'home'] });
    router.back();
  };

  const report = async (reason: string) => {
    setReporting(false);
    try {
      const r = await unwrap(api.POST('/questions/{questionId}/report', { params: { path: { questionId: q.id } }, body: { reason } }));
      toast(r.offline ? '已收到，这道题已下线' : '已收到，我们会核对这道题');
    } catch {
      toast('没提交成功，请稍后再试');
    }
  };

  return (
    <Screen>
      <NavBar
        title={`${s.title} · ${index + 1} / ${s.questions.length}`}
        left={<BackButton icon="close" label="退出训练" onPress={() => setExiting(true)} />}
        right={<Text variant="small">{clock(elapsed)}</Text>}
      />
      <View style={styles.progress} accessibilityElementsHidden>
        <View style={[styles.progressFill, { width: `${((index + (result ? 1 : 0)) / Math.max(1, s.questions.length)) * 100}%` }]} />
      </View>
      <ScrollView contentContainerStyle={styles.scroll}>
        <QuestionHeader q={q} />
        <Text variant="h2" style={styles.stem}>
          {q.qtype === 'term' ? `${qtypeNames.term}：${q.stem}` : q.stem}
        </Text>
        {q.options?.length ? <Options q={q} selected={result?.revealed ? [] : (result && q.answered?.selected) || selected} result={result} onToggle={toggle} /> : null}
        {q.qtype === 'fill_blank' ? <FillBlank value={text} onChange={setText} disabled={!!result} /> : null}
        {result?.grading ? (
          <GradingResultView g={result.grading} kpId={q.knowledge_points[0]?.id} norm={{ subjectId: s.subject_id, qtype: q.qtype }} onDispute={() => setDisputing(true)} onRegrade={() => regrade.mutate(result.grading!.grading_id)} regrading={regrade.isPending} />
        ) : result?.queued ? (
          <Text variant="body" color={semantic.info}>
            答案已保存为待批改，明天 0 点后在训练页一键提交
          </Text>
        ) : result ? (
          <ResultPanel q={q} result={result} onReport={() => setReporting(true)} />
        ) : null}
        {result?.grading ? (
          <DisputeSheet g={result.grading} visible={disputing} onClose={() => setDisputing(false)} onDone={(g) => setResults((r) => ({ ...r, [q.id]: { ...result, grading: g } }))} />
        ) : null}
        {!objective && !result ? (
          <SubjectiveFlow
            key={q.id}
            q={q}
            sessionId={sessionId}
            onGraded={(g) => setResults((r) => ({ ...r, [q.id]: { correct: null, revealed: false, synced: true, grading: g } }))}
            onQueued={() => setResults((r) => ({ ...r, [q.id]: { correct: null, revealed: false, synced: true, queued: true } }))}
            onSelfAssess={(k) => void send({ revealed: true, self_assess: k })}
          />
        ) : null}
      </ScrollView>
      <View style={styles.footer}>
        {result ? (
          <Button title={index + 1 < s.questions.length ? '下一题' : '完成本组'} loading={finishing} onPress={() => void next()} />
        ) : objective ? (
          <>
            {q.qtype === 'multi_choice' ? <Button title="提交" disabled={selected.length === 0} onPress={() => void send({ selected })} /> : null}
            {q.qtype === 'fill_blank' ? <Button title="提交" disabled={!text.trim()} onPress={() => void send({ answer_text: text })} /> : null}
            <Button title="不会，看答案" kind="soft" style={styles.reveal} onPress={() => void send({ revealed: true })} />
          </>
        ) : null}
      </View>
      <ConfirmDialog
        visible={exiting}
        title="先休息一下？"
        message={`${s.title}已完成 ${done} / ${s.questions.length} 题，进度已保存，下次从第 ${Math.min(done + 1, s.questions.length)} 题继续。`}
        confirmText="退出"
        cancelText="继续训练"
        onConfirm={() => void exit()}
        onCancel={() => setExiting(false)}
      />
      <BottomSheet visible={reporting} onClose={() => setReporting(false)} title="这道题有什么问题？">
        <View style={styles.reasons}>
          {reportReasons.map((r) => (
            <Button key={r} title={r} kind="secondary" onPress={() => void report(r)} />
          ))}
        </View>
      </BottomSheet>
    </Screen>
  );
}

function fromAnswered(a?: { is_correct?: boolean; revealed: boolean; self_assess?: LocalResult['selfAssess'] }): LocalResult | undefined {
  if (!a) return undefined;
  return { correct: a.is_correct ?? null, revealed: a.revealed, synced: true, selfAssess: a.self_assess ?? (a.revealed ? 'unknown' : undefined) };
}

const styles = StyleSheet.create({
  progress: { height: 3, borderRadius: 2, backgroundColor: semantic.border, marginTop: 4, marginBottom: spacing.md },
  progressFill: { height: 3, borderRadius: 2, backgroundColor: semantic.textPrimary },
  reveal: { alignSelf: 'center', paddingHorizontal: 24 },
  flex: { flex: 1 },
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  stem: { lineHeight: 30 },
  subjective: { padding: spacing.md, borderRadius: 12, backgroundColor: semantic.infoSoft },
  assess: { gap: spacing.sm },
  row: { flexDirection: 'row', gap: spacing.sm },
  footer: { gap: spacing.xs, paddingVertical: spacing.sm },
  reasons: { gap: spacing.sm },
});
