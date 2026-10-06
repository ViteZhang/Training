// 4.20 练习模式作答、4.21 模拟考试作答、4.22 答题卡、4.23 交卷确认；交卷后显示批改进度与得分。
// 倒计时以服务端 deadline_at 为准（按 server_now 校准本机时钟）；草稿每 5 秒存 MMKV 并同步到服务端；
// 模拟考试被系统中断（杀掉 App、来电）后，10 分钟内可恢复一次并补回中断时长（PRD 11.9）。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { BackButton, BottomSheet, Button, Card, ConfirmDialog, ErrorState, Icon, Loading, Screen, Tag, Text, toast } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { clearDrafts, clock, lastActive, loadDrafts, markActive, paperKeys, saveDrafts, type PaperItem, type PaperSession } from '@/features/paper/api';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

const SYNC_MS = 5000;
const objective = (it: PaperItem) => (it.options?.length ?? 0) > 0;

function chars(s: string) {
  return [...s.replace(/\s/g, '')].length;
}

function useSession(id: number) {
  return useQuery({
    queryKey: paperKeys.session(id),
    queryFn: () => unwrap(api.GET('/paper-sessions/{sessionId}', { params: { path: { sessionId: id } } })),
    // 批改中每 15 秒刷新一次，直到出分。
    refetchInterval: (q) => (q.state.data?.status === 'grading' ? 15000 : false),
  });
}

/** 本机时钟与服务端的差：以服务端为准计时。 */
function useServerClock(s: PaperSession | undefined, fetchedAt: number) {
  const offset = useMemo(() => (s ? Date.parse(s.server_now) - fetchedAt : 0), [s, fetchedAt]);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  return now + offset;
}

function ResultView({ s }: { s: PaperSession }) {
  const grading = s.status === 'grading';
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="h2">{s.title}</Text>
        <Card style={styles.gap}>
          {grading ? (
            <>
              <Text variant="h3">正在整卷批改</Text>
              <Text variant="caption">按你资料里的采分点逐题批改，大约需要 2 分钟。批改完成后会发消息提醒你，可以先离开。</Text>
            </>
          ) : (
            <>
              <Text variant="caption">得分</Text>
              <Text variant="score">
                {s.score ?? '-'}
                <Text variant="caption"> / {s.full_score}</Text>
              </Text>
              <Text variant="caption">{s.counts_for_estimate ? '这套真题卷会用来更新预估分' : 'AI 组卷的成绩只作参考，不计入预估分'}</Text>
            </>
          )}
        </Card>
        <Card style={styles.list}>
          {s.items.map((it) => (
            <Pressable
              key={it.seq}
              accessibilityRole="button"
              accessibilityLabel={`第 ${it.seq} 题`}
              disabled={!it.grading_id}
              onPress={() => it.grading_id && router.push({ pathname: '/practice/grading/[id]', params: { id: String(it.grading_id) } })}
              style={styles.resultRow}
            >
              <Text variant="bodyStrong" style={styles.seq}>
                {it.seq}
              </Text>
              <Text variant="caption" style={styles.flex}>
                {qtypeNames[it.qtype]} · {it.answered ? '已答' : '未答'}
              </Text>
              <Text variant="bodyStrong">{it.got !== undefined ? `${it.got} / ${it.score}` : grading ? '批改中' : `- / ${it.score}`}</Text>
            </Pressable>
          ))}
        </Card>
      </ScrollView>
      <View style={styles.nav}>
        <Button
          title="返回整卷列表"
          kind="secondary"
          style={styles.flex}
          onPress={() => router.replace({ pathname: '/paper/list', params: { subjectId: String(s.subject_id) } })}
        />
        {!grading ? (
          <Button title="整卷报告" style={styles.flex} onPress={() => router.push({ pathname: '/paper/report/[id]', params: { id: String(s.id) } })} />
        ) : null}
      </View>
    </Screen>
  );
}

function AnswerCard({ s, drafts, seq, onPick }: { s: PaperSession; drafts: Record<number, string>; seq: number; onPick: (seq: number) => void }) {
  const answered = s.items.filter((it) => (drafts[it.seq] ?? '').trim() !== '').length;
  const marked = s.items.filter((it) => it.marked).length;
  const sections = s.items.reduce<{ name: string; items: PaperItem[] }[]>((acc, it) => {
    const last = acc[acc.length - 1];
    if (last && last.name === it.section) last.items.push(it);
    else acc.push({ name: it.section, items: [it] });
    return acc;
  }, []);
  return (
    <View style={styles.gap}>
      <View style={styles.counts}>
        <Text variant="caption">已答 {answered}</Text>
        <Text variant="caption">未答 {s.items.length - answered}</Text>
        <Text variant="caption">标记 {marked}</Text>
      </View>
      {sections.map((sec) => (
        <View key={sec.name} style={styles.gapSm}>
          <Text variant="bodyStrong">{sec.name}</Text>
          <View style={styles.grid}>
            {sec.items.map((it) => {
              const done = (drafts[it.seq] ?? '').trim() !== '';
              return (
                <Pressable
                  key={it.seq}
                  accessibilityRole="button"
                  accessibilityLabel={`第 ${it.seq} 题${done ? '，已答' : '，未答'}${it.marked ? '，已标记' : ''}`}
                  onPress={() => onPick(it.seq)}
                  style={[styles.cell, done && styles.cellDone, it.seq === seq && styles.cellOn]}
                >
                  <Text variant="bodyStrong" color={done ? semantic.surface : undefined}>
                    {it.seq}
                  </Text>
                  {it.marked ? <View style={styles.flag} /> : null}
                </Pressable>
              );
            })}
          </View>
        </View>
      ))}
    </View>
  );
}

function Answering({ s, fetchedAt }: { s: PaperSession; fetchedAt: number }) {
  const qc = useQueryClient();
  const mock = s.mode === 'mock';
  const now = useServerClock(s, fetchedAt);
  const [seq, setSeq] = useState(() => s.items.find((it) => !it.answered)?.seq ?? 1);
  // 本地草稿优先：杀掉 App 再打开时，最后 5 秒内没同步上去的内容还在。
  const [drafts, setDrafts] = useState<Record<number, string>>(() => {
    const server = Object.fromEntries(s.items.map((it) => [it.seq, it.draft_text ?? '']));
    return { ...server, ...loadDrafts(s.id) };
  });
  const [cardOpen, setCardOpen] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [leave, setLeave] = useState(false);
  const [interrupted] = useState(() => {
    const at = lastActive(s.id);
    // 超过 1 分钟没在作答页，按「被中断」处理。
    return mock && s.resume_available && at !== undefined && Date.now() - at > 60_000 ? at : undefined;
  });
  const [recoverAsk, setRecoverAsk] = useState(interrupted !== undefined);
  const synced = useRef<Record<number, string>>(Object.fromEntries(s.items.map((it) => [it.seq, it.draft_text ?? ''])));
  const tickStart = useRef(0);
  const state = useRef({ drafts, seq });
  useEffect(() => {
    tickStart.current = Date.now();
    markActive(s.id);
  }, [s.id]);
  useEffect(() => {
    state.current = { drafts, seq };
  }, [drafts, seq]);

  const item = s.items.find((it) => it.seq === seq) ?? s.items[0]!;
  const paused = s.status === 'paused';
  const deadline = s.deadline_at ? Date.parse(s.deadline_at) : undefined;
  const fetchedServer = Date.parse(s.server_now);
  const elapsed = paused ? s.elapsed_seconds : s.elapsed_seconds + Math.max(0, (now - fetchedServer) / 1000);
  const left = deadline !== undefined ? (deadline - now) / 1000 : undefined;

  const setSession = (v: PaperSession) => qc.setQueryData(paperKeys.session(v.id), v);

  const sync = useCallback(async () => {
    const { drafts: d, seq: cur } = state.current;
    saveDrafts(s.id, d);
    markActive(s.id);
    const delta = Math.round((Date.now() - tickStart.current) / 1000);
    tickStart.current = Date.now();
    const changed = Object.keys(d).map(Number).filter((k) => d[k] !== synced.current[k]);
    if (!changed.includes(cur)) changed.push(cur);
    for (const k of changed) {
      const res = await api.PUT('/paper-sessions/{sessionId}/items/{seq}', {
        params: { path: { sessionId: s.id, seq: k } },
        body: { draft_text: d[k] ?? '', time_spent_delta: k === cur ? delta : 0 },
      });
      if (res.response.status === 409) {
        // 时间到了服务端已自动交卷。
        void qc.invalidateQueries({ queryKey: paperKeys.session(s.id) });
        return;
      }
      if (res.response.ok) synced.current[k] = d[k] ?? '';
    }
  }, [qc, s.id]);

  useEffect(() => {
    if (paused) return;
    const t = setInterval(() => void sync(), SYNC_MS);
    return () => clearInterval(t);
  }, [paused, sync]);

  const submit = useMutation({
    mutationFn: async () => {
      await sync();
      return unwrap(api.POST('/paper-sessions/{sessionId}/submit', { params: { path: { sessionId: s.id } } }));
    },
    onSuccess: (v) => {
      track('paper_submit', { mode: v.mode });
      clearDrafts(s.id);
      setSession(v);
      void qc.invalidateQueries({ queryKey: ['papers'] });
    },
    onError: (e) => toast(e instanceof Error ? e.message : '交卷失败，请重试'),
  });
  const { mutate: submitNow, isPending: submitting } = submit;

  // 模拟考试时间到自动交卷（服务端也会按 deadline_at 自动交卷，这里只是让页面立即反应）。
  // 只触发一次：失败时不在这里反复重试，服务端到点会自己交卷，页面刷新后进入批改中。
  const timeUp = left !== undefined && left <= 0;
  const autoSubmitted = useRef(false);
  useEffect(() => {
    if (timeUp && !submitting && !recoverAsk && !autoSubmitted.current) {
      autoSubmitted.current = true;
      toast('时间到了，已自动交卷');
      submitNow(undefined, { onError: () => void qc.invalidateQueries({ queryKey: paperKeys.session(s.id) }) });
    }
  }, [timeUp, submitting, submitNow, recoverAsk, qc, s.id]);

  const pause = useMutation({
    mutationFn: async () => {
      await sync();
      return unwrap(api.POST('/paper-sessions/{sessionId}/pause', { params: { path: { sessionId: s.id } } }));
    },
    onSuccess: setSession,
    onError: (e) => toast(e instanceof Error ? e.message : '暂停失败'),
  });
  const resume = useMutation({
    mutationFn: (interruptedAt?: number) =>
      unwrap(
        api.POST('/paper-sessions/{sessionId}/resume', {
          params: { path: { sessionId: s.id } },
          body: interruptedAt !== undefined ? { interrupted_at: new Date(interruptedAt).toISOString() } : {},
        }),
      ),
    onSuccess: (v) => {
      tickStart.current = Date.now();
      markActive(s.id);
      setSession(v);
    },
    onError: (e) => toast(e instanceof Error ? e.message : '没能恢复'),
  });
  const mark = useMutation({
    mutationFn: (v: boolean) => unwrap(api.PUT('/paper-sessions/{sessionId}/items/{seq}', { params: { path: { sessionId: s.id, seq } }, body: { marked: v } })),
    onSuccess: (it) => setSession({ ...s, items: s.items.map((x) => (x.seq === it.seq ? { ...x, marked: it.marked } : x)) }),
  });

  const go = (to: number) => {
    void sync();
    setSeq(to);
    setCardOpen(false);
  };
  const setDraft = (v: string) => setDrafts((d) => ({ ...d, [seq]: v }));
  const toggleKey = (k: string) => {
    const cur = drafts[seq] ?? '';
    if (item.qtype === 'multi_choice') {
      const keys = new Set(cur.split(''));
      if (keys.has(k)) keys.delete(k);
      else keys.add(k);
      setDraft([...keys].sort().join(''));
    } else setDraft(cur === k ? '' : k);
  };

  // 模拟考试：到某题型的累计建议用时就提醒一次（以最近一条为准），剩 N 分钟醒目提示。
  const reminder = mock ? [...s.reminders].reverse().find((r) => elapsed >= r.at_minutes * 60 && elapsed < r.at_minutes * 60 + 5 * 60) : undefined;
  const lastMinutes = mock && left !== undefined && left <= s.remind_left_minutes * 60;
  const unanswered = s.items.filter((it) => (drafts[it.seq] ?? '').trim() === '').length;
  const markedCount = s.items.filter((it) => it.marked).length;
  const idx = s.items.findIndex((it) => it.seq === seq);
  const text = drafts[seq] ?? '';

  return (
    <Screen>
      <View style={styles.top}>
        <BackButton icon="close" label="退出" onPress={() => setLeave(true)} />
        <View style={styles.clock}>
          <Text variant="number" style={styles.clockText} color={lastMinutes ? semantic.danger : semantic.textPrimary} accessibilityLabel={mock ? '剩余时间' : '已用时间'}>
            {mock && left !== undefined ? clock(left) : clock(elapsed)}
          </Text>
          <Text variant="small" color={mock ? semantic.danger : semantic.textSecondary} style={styles.tiny}>
            {mock ? '模拟考试 · 不可暂停' : '练习模式'}
          </Text>
        </View>
        <Button title="答题卡" kind="soft" size="sm" onPress={() => setCardOpen(true)} />
      </View>
      {lastMinutes ? (
        <View style={[styles.banner, styles.bannerDanger]}>
          <Icon name="clock" size={18} color={colors.white} />
          <Text variant="caption" color={colors.white} style={styles.flex}>
            还剩 {Math.ceil((left ?? 0) / 60)} 分钟，先把没写的题写上要点
          </Text>
        </View>
      ) : reminder ? (
        <View style={styles.banner}>
          <Icon name="clock" size={18} color={colors.white} />
          <Text variant="caption" color={colors.white} style={styles.flex}>
            {reminder.text}
          </Text>
        </View>
      ) : null}
      {paused ? (
        <Card style={styles.gap}>
          <Text variant="h3">已暂停</Text>
          <Text variant="caption">已答 {s.items.length - unanswered} / {s.items.length}，暂停期间不计时</Text>
          <Button title="继续作答" loading={resume.isPending} onPress={() => resume.mutate(undefined)} />
        </Card>
      ) : (
        <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
          <View style={styles.qHead}>
            <Text variant="small" style={styles.flex}>
              第 <Text variant="small" color={semantic.textPrimary} style={styles.bold}>{seq}</Text> / {s.items.length} 题 · {qtypeNames[item.qtype]} · {item.score} 分
            </Text>
            {item.origin_tags?.includes('ai_generated') ? <Tag label="AI 变式题" tone="ai" /> : null}
            <Pressable accessibilityRole="button" accessibilityLabel={item.marked ? '已标记' : '标记'} onPress={() => mark.mutate(!item.marked)} style={[styles.mark, item.marked && styles.markOn]}>
              <View style={[styles.markDot, item.marked && styles.markDotOn]} />
              <Text variant="small" color={semantic.textPrimary}>
                {item.marked ? '已标记' : '标记'}
              </Text>
            </Pressable>
          </View>
          <Text variant="h2">{item.qtype === 'term' ? `${qtypeNames.term}：${item.stem}` : item.stem}</Text>
          {objective(item) ? null : (
            <Text variant="small" style={styles.label}>
              你的答案
            </Text>
          )}
          {objective(item) ? (
            <View style={styles.gapSm}>
              {item.options!.map((o) => {
                const on = text.includes(o.key);
                return (
                  <Pressable
                    key={o.key}
                    accessibilityRole={item.qtype === 'multi_choice' ? 'checkbox' : 'radio'}
                    accessibilityState={{ checked: on }}
                    onPress={() => toggleKey(o.key)}
                    style={[styles.option, on && styles.optionOn]}
                  >
                    <Text variant="caption" color={semantic.textPrimary} style={styles.bold}>
                      {o.key}
                    </Text>
                    <Text variant="body" style={styles.flex}>
                      {o.text}
                    </Text>
                  </Pressable>
                );
              })}
            </View>
          ) : (
            <>
              <TextInput
                accessibilityLabel="你的答案"
                multiline
                value={text}
                onChangeText={setDraft}
                placeholder="写下你的答案"
                placeholderTextColor={semantic.textSecondary}
                style={styles.answer}
                textAlignVertical="top"
              />
              <Text variant="small">
                {chars(text)} 字{item.required_words ? ` · 建议 ${item.required_words} 字左右` : ''}
              </Text>
            </>
          )}
          <Text variant="small">本题已用 {clock(item.time_spent_seconds)}</Text>
        </ScrollView>
      )}
      {!paused ? (
        <View style={styles.nav}>
          <Button title="上一题" kind="secondary" disabled={idx <= 0} onPress={() => go(s.items[idx - 1]!.seq)} style={styles.flex} />
          {idx < s.items.length - 1 ? (
            <Button title="下一题" onPress={() => go(s.items[idx + 1]!.seq)} style={styles.flex} />
          ) : (
            <Button title="交卷" onPress={() => setConfirm(true)} style={styles.flex} />
          )}
        </View>
      ) : null}
      <BottomSheet visible={cardOpen} onClose={() => setCardOpen(false)} title="答题卡">
        <AnswerCard s={s} drafts={drafts} seq={seq} onPick={go} />
        <View style={[styles.nav, styles.sheetNav]}>
          <Button title="继续作答" kind="secondary" onPress={() => setCardOpen(false)} style={styles.flex} />
          <Button
            title="交卷"
            onPress={() => {
              setCardOpen(false);
              setConfirm(true);
            }}
            style={styles.flex}
          />
        </View>
      </BottomSheet>
      <ConfirmDialog
        visible={confirm}
        title="确认交卷？"
        message={`${unanswered > 0 ? `还有 ${unanswered} 题未作答，` : ''}${markedCount > 0 ? `${markedCount} 题已标记。` : ''}交卷后开始整卷批改，大约需要 2 分钟。按你资料里的采分点逐题批改。`}
        confirmText="交卷"
        cancelText="继续作答"
        onCancel={() => setConfirm(false)}
        onConfirm={() => {
          setConfirm(false);
          submit.mutate();
        }}
      />
      <ConfirmDialog
        visible={leave}
        title={mock ? '模拟考试不能暂停' : '先离开？'}
        message={mock ? '离开后倒计时继续，时间到会自动交卷。草稿已保存。' : '会暂停计时，草稿已保存，回来可以接着做。'}
        confirmText={mock ? '离开' : '暂停并离开'}
        cancelText="继续作答"
        onCancel={() => setLeave(false)}
        onConfirm={() => {
          setLeave(false);
          if (mock) {
            void sync();
            router.back();
          } else pause.mutate(undefined, { onSuccess: () => router.back() });
        }}
      />
      <ConfirmDialog
        visible={recoverAsk && !timeUp}
        title="考试被中断了"
        message="10 分钟内可以恢复一次，中断的时间会补回来。"
        confirmText="恢复考试"
        cancelText="不用了"
        onCancel={() => {
          setRecoverAsk(false);
          markActive(s.id);
        }}
        onConfirm={() => {
          setRecoverAsk(false);
          resume.mutate(interrupted);
        }}
      />
    </Screen>
  );
}

export default function PaperSessionPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const session = useSession(Number(id));
  if (session.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (session.isError || !session.data) return <Screen><ErrorState error={session.error} onRetry={() => void session.refetch()} /></Screen>;
  const s = session.data;
  if (s.status === 'abandoned') {
    return (
      <Screen>
        <ErrorState error={new Error('这套卷已经放弃了')} onRetry={() => router.back()} />
      </Screen>
    );
  }
  if (s.status === 'grading' || s.status === 'graded') return <ResultView s={s} />;
  return <Answering key={s.id} s={s} fetchedAt={session.dataUpdatedAt} />;
}

const styles = StyleSheet.create({
  scroll: { gap: 12, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  gapSm: { gap: 10 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
  tiny: { fontSize: 11, lineHeight: 15 },
  label: { marginBottom: -4 },
  list: { paddingVertical: spacing.sm },
  top: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', minHeight: 52 },
  clock: { alignItems: 'center' },
  clockText: { fontSize: 18, lineHeight: 24 },
  banner: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 12, paddingHorizontal: 16, borderRadius: radius.lg, backgroundColor: colors.indigo, marginBottom: spacing.md },
  bannerDanger: { backgroundColor: semantic.danger },
  qHead: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs },
  mark: { flexDirection: 'row', alignItems: 'center', gap: 4, minHeight: 30, paddingHorizontal: 10, borderRadius: radius.pill, borderWidth: 1, borderColor: semantic.border },
  markOn: { borderColor: colors.amber, backgroundColor: semantic.amberSoft },
  markDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: '#CFCAC0' },
  markDotOn: { backgroundColor: colors.amber },
  option: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 52, paddingVertical: 12, paddingHorizontal: 14, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  optionOn: { borderColor: semantic.primary, borderWidth: 1.5 },
  answer: { minHeight: 260, padding: 14, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 15, lineHeight: 28, color: semantic.textPrimary },
  nav: { flexDirection: 'row', gap: 10, paddingVertical: spacing.sm },
  sheetNav: { marginTop: spacing.lg, marginBottom: spacing.lg },
  counts: { flexDirection: 'row', gap: spacing.lg },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  cell: { width: 48, height: 48, alignItems: 'center', justifyContent: 'center', borderRadius: 14, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  cellDone: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  cellOn: { borderWidth: 2, borderColor: semantic.textPrimary },
  flag: { position: 'absolute', top: 4, right: 4, width: 8, height: 8, borderRadius: 4, backgroundColor: colors.amber },
  resultRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 48, borderTopWidth: 1, borderTopColor: semantic.border },
  seq: { width: 28 },
});
