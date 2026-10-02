// 4.3 客观题答题：选中即判分（单选、判断），多选与填空点「提交」；「不会，看答案」计为未掌握；「题目有问题」可报错。
// 离线时用本地缓存的题与答案继续做、本地判分，联网后自动补交并以服务端复核为准。4.10 退出训练确认：进度保存，下次从断点继续。
import { semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useMemo, useRef, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, ConfirmDialog, EmptyState, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { practiceKeys, useSession } from '@/features/practice/api';
import { isObjective, judge } from '@/features/practice/judge';
import { cacheSession, submitAttempt } from '@/features/practice/offline';
import { FillBlank, Options, QuestionHeader, ResultPanel, type LocalResult } from '@/features/practice/QuestionView';
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
  const [showRef, setShowRef] = useState(false);
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
      setShowRef(false);
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
      <View style={styles.top}>
        <Pressable accessibilityRole="button" accessibilityLabel="退出训练" onPress={() => setExiting(true)} style={styles.close}>
          <Text variant="h3">×</Text>
        </Pressable>
        <Text variant="bodyStrong" style={styles.flex}>
          {s.title} · {index + 1} / {s.questions.length}
        </Text>
        <Text variant="number">{clock(elapsed)}</Text>
      </View>
      <ScrollView contentContainerStyle={styles.scroll}>
        <QuestionHeader q={q} />
        <Text variant="body" style={styles.stem}>
          {q.stem}
        </Text>
        {q.options?.length ? <Options q={q} selected={result?.revealed ? [] : (result && q.answered?.selected) || selected} result={result} onToggle={toggle} /> : null}
        {q.qtype === 'fill_blank' ? <FillBlank value={text} onChange={setText} disabled={!!result} /> : null}
        {!objective && !result && !showRef ? (
          <View style={styles.subjective}>
            <Text variant="caption">
              {q.rubric_count > 0 ? `按你资料里的 ${q.rubric_count} 个采分点批改` : '这道题还没有采分点'} · 打字作答与 AI 批改即将上线，先对照参考答案自评
            </Text>
          </View>
        ) : null}
        {result ? <ResultPanel q={q} result={result} onReport={() => setReporting(true)} /> : null}
        {!objective && showRef && !result ? (
          <View style={styles.assess}>
            <View style={styles.subjective}>
              <Text variant="bodyStrong">参考答案</Text>
              <Text variant="body">{q.answer ?? '这道题还没有参考答案'}</Text>
            </View>
            <Text variant="bodyStrong">对照参考答案，你掌握得怎么样？</Text>
            <View style={styles.row}>
              {(
                [
                  ['unknown', '不会'],
                  ['vague', '模糊'],
                  ['mastered', '掌握'],
                ] as const
              ).map(([k, label]) => (
                <Button key={k} title={label} kind="secondary" style={styles.flex} onPress={() => void send({ revealed: true, self_assess: k })} />
              ))}
            </View>
          </View>
        ) : null}
      </ScrollView>
      <View style={styles.footer}>
        {result ? (
          <Button title={index + 1 < s.questions.length ? '下一题' : '完成本组'} loading={finishing} onPress={() => void next()} />
        ) : objective ? (
          <>
            {q.qtype === 'multi_choice' ? <Button title="提交" disabled={selected.length === 0} onPress={() => void send({ selected })} /> : null}
            {q.qtype === 'fill_blank' ? <Button title="提交" disabled={!text.trim()} onPress={() => void send({ answer_text: text })} /> : null}
            <Button title="不会，看答案" kind="text" onPress={() => void send({ revealed: true })} />
          </>
        ) : showRef ? null : (
          <Button title="看参考答案" onPress={() => setShowRef(true)} />
        )}
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
  top: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm },
  close: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  flex: { flex: 1 },
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
  stem: { fontSize: 17, lineHeight: 26 },
  subjective: { padding: spacing.md, borderRadius: 12, backgroundColor: semantic.infoSoft },
  assess: { gap: spacing.sm },
  row: { flexDirection: 'row', gap: spacing.sm },
  footer: { gap: spacing.xs, paddingVertical: spacing.sm },
  reasons: { gap: spacing.sm },
});
