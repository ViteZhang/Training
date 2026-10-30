// 4.20 练习模式作答、4.21 模拟考试作答、4.22 答题卡、4.23 交卷确认；交卷后显示批改进度与得分。
// 倒计时以服务端 deadline_at 为准（按 server_now 校准本机时钟）；草稿每 5 秒存 MMKV 并同步到服务端；
// 模拟考试被系统中断（杀掉 App、来电）后，10 分钟内可恢复一次并补回中断时长（PRD 11.9）。
import { VoiceRecorder } from '@/features/practice/VoiceRecorder';
import { useFeatureFlag } from '@/lib/flags';
import { ApiError } from '@training/api-client';
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { BackHandler, Pressable, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { BackButton, BottomSheet, Button, Card, ConfirmDialog, ErrorState, GroupNote, Icon, Loading, PassageCard, Screen, Tag, Text, toast } from '@/components';
import { qtypeNames } from '@/features/import/api';
import { clearDrafts, clock, lastActive, loadDrafts, markActive, paperKeys, saveDrafts, type PaperItem, type PaperSession } from '@/features/paper/api';
import { PaperTimer } from '@/features/paper/timer';
import { PaperPhotoReview } from '@/features/paper/PhotoReview';
import { loadPhotoEdits, mergePaperPhotos, savePhotoEdits, usePaperPhotos, type LocalPhoto, type PaperHandwriting } from '@/features/paper/photos';
import { HandwritingFlow } from '@/features/practice/handwriting';
import { Tool } from '@/features/practice/grading';
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
    refetchInterval: (q) => q.state.data?.status === 'grading' ? 15000 : q.state.data?.items.some((it) => it.handwriting?.some((h) => h.status === 'pending' || h.status === 'failed')) ? 2500 : false,
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

function ResultView({ s, refreshError, onRefresh }: { s: PaperSession; refreshError?: boolean; onRefresh: () => void }) {
  const qc = useQueryClient();
  const { photos } = usePaperPhotos(s.id);
  const unsentPhotos = Object.values(photos).filter((p) => p.status !== 'registered');
  const recognizing = s.items.some((it) => it.handwriting?.some((h) => h.status === 'pending' || h.status === 'failed' || h.status === 'ready'));
  const [previewSeq, setPreviewSeq] = useState<number>();
  const preview = s.items.find((it) => it.seq === previewSeq);
  const retry = useMutation({
    mutationFn: () => unwrap(api.POST('/paper-sessions/{sessionId}/submit', { params: { path: { sessionId: s.id } } })),
    onSuccess: (v) => { qc.setQueryData(paperKeys.session(s.id), v); void qc.invalidateQueries({ queryKey: ['papers'] }); },
    onError: (e) => toast(e instanceof Error ? e.message : '恢复批改失败，请稍后重试'),
  });
  const grading = s.status === 'grading';
  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text variant="h2">{s.title}</Text>
        {unsentPhotos.length ? <Card style={styles.gap}><Text variant="caption" color={semantic.danger}>本机还有 {unsentPhotos.length} 份照片或录音未能在交卷前保存到服务端，未包含在本次答卷中，原文件仍保留在本机。</Text></Card> : null}
        {refreshError ? <Card style={styles.gap}><Text variant="caption">暂时无法刷新批改进度，已提交的答卷已保存。</Text><Button title="刷新进度" kind="secondary" onPress={onRefresh} /></Card> : null}
        <Card style={styles.gap}>
          {grading ? (
            <>
              <Text variant="h3">{s.grading_error ? '批改暂时中断' : recognizing ? '正在识别答案并整卷批改' : '正在整卷批改'}</Text>
              <Text variant="caption">{s.grading_error ?? (recognizing ? '已上传的照片和录音会继续识别，未核对的结果默认采用；全部识别完成后再逐题批改，可以先离开。' : '按采分点逐题批改，通常需要几分钟，题目较多时可能更久。批改完成后会发消息提醒你，可以先离开。')}</Text>
              {s.grading_error ? <Button title="恢复批改" loading={retry.isPending} onPress={() => retry.mutate()} /> : null}
              <Text variant="small">已提交的答案已保存，可从整卷历史记录回来查看。</Text>
            </>
          ) : (
            <>
              <Text variant="caption">得分</Text>
              <Text variant="score">
                {s.score ?? '-'}
                <Text variant="caption" num> / {s.full_score}</Text>
              </Text>
              <Text variant="caption">{s.counts_for_estimate ? '这套真题卷会用来更新预估分' : 'AI 组卷的成绩只作参考，不计入预估分'}</Text>
            </>
          )}
        </Card>
        <Button title="历史答卷" kind="text" onPress={() => router.push({ pathname: '/paper/history', params: { subjectId: String(s.subject_id) } })} />
        <Card style={styles.list}>
          {s.items.map((it) => (
            <Pressable
              key={it.seq}
              accessibilityRole="button"
              accessibilityLabel={`第 ${it.seq} 题`}
              onPress={() => setPreviewSeq(it.seq)}
              style={styles.resultRow}
            >
              <Text variant="bodyStrong" style={styles.seq}>
                {it.seq}
              </Text>
              <Text variant="caption" style={styles.flex}>
                {qtypeNames[it.qtype]} · {it.answered ? '已答' : it.handwriting?.some((h) => h.status !== 'discarded') ? '文件待处理' : '未答'}
              </Text>
              <Text variant="bodyStrong" num={600}>{it.got !== undefined ? `${it.got} / ${it.score}` : grading ? (it.answered ? '待批改' : it.handwriting?.some((h) => h.status !== 'discarded') ? '待识别' : '未作答') : `- / ${it.score}`}</Text>
            </Pressable>
          ))}
        </Card>
      </ScrollView>
      <BottomSheet visible={preview !== undefined} onClose={() => setPreviewSeq(undefined)} title={`第 ${previewSeq ?? ''} 题答卷`}>
        <ScrollView contentContainerStyle={styles.gap}>
          <Text variant="bodyStrong">{preview?.stem}</Text>
          <Text variant="caption">你的答案</Text>
          <Text variant="body">{preview?.draft_text?.trim() || '未作答'}</Text>
          {preview?.handwriting?.some((h) => h.status !== 'confirmed' && h.status !== 'discarded') ? <Text variant="caption">本题已保存照片或录音，识别完成后默认计入答案再批改。</Text> : null}
          {preview?.grading_id ? <Button title="查看逐题批改" onPress={() => { const id = preview.grading_id; setPreviewSeq(undefined); router.push({ pathname: '/practice/grading/[id]', params: { id: String(id) } }); }} /> : null}
        </ScrollView>
      </BottomSheet>
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

function AnswerCard({ s, drafts, seq, onPick, photoStates }: { s: PaperSession; drafts: Record<number, string>; seq: number; photoStates: Record<number, string>; onPick: (seq: number) => void }) {
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
        {Object.keys(photoStates).length ? <Text variant="caption">文件待处理 {Object.keys(photoStates).length}</Text> : null}
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
                  accessibilityLabel={`第 ${it.seq} 题${done ? '，已答' : '，未答'}${it.marked ? '，已标记' : ''}${photoStates[it.seq] ? `，${photoStates[it.seq]}` : ''}`}
                  onPress={() => onPick(it.seq)}
                  style={[styles.cell, done && styles.cellDone, it.seq === seq && styles.cellOn]}
                >
                  <Text variant="bodyStrong" num={600} color={done ? semantic.surface : undefined}>
                    {it.seq}
                  </Text>
                  {photoStates[it.seq] ? <Text variant="small" style={styles.tiny} color={done ? semantic.surface : semantic.textSecondary}>{photoStates[it.seq]}</Text> : null}
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
  const voiceEnabled = useFeatureFlag('voice_answer');
  const [recording, setRecording] = useState(false);
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
  const [timer] = useState(() => new PaperTimer(s.items, seq, s.status !== 'paused'));
  const [photoSeq, setPhotoSeq] = useState<number>();
  const { queue: photoQueue, photos } = usePaperPhotos(s.id);
  const jobs = mergePaperPhotos(s.items, photos);
  const jobsRef = useRef(jobs);
  useEffect(() => { jobsRef.current = jobs; }, [jobs]);
  const [photoEdits, setPhotoEdits] = useState(() => loadPhotoEdits(s.id));
  const photoEditsRef = useRef(photoEdits);
  const [photoBusy, setPhotoBusy] = useState(false);
  const photoAction = useRef(false);
  const [discardPhoto, setDiscardPhoto] = useState<LocalPhoto | PaperHandwriting>();
  useEffect(() => { photoQueue.resume(); }, [photoQueue]);
  useEffect(() => { photoQueue.reconcile(s.items); }, [photoQueue, s.items]);
  useEffect(() => {
    if (Object.keys(photos).length) void qc.invalidateQueries({ queryKey: paperKeys.session(s.id) });
  }, [photos, qc, s.id]);
  const editPhoto = (key: string, value: string) => {
    const edits = { ...photoEditsRef.current, [key]: value };
    photoEditsRef.current = edits;
    savePhotoEdits(s.id, edits);
    setPhotoEdits(edits);
  };
  const [syncError, setSyncError] = useState(false);
  const syncQueue = useRef(Promise.resolve());
  const pendingWrites = useRef<Record<number, { draft: string; delta: number; total: number; key: string }>>({});
  useEffect(() => { timer.setRunning(s.status === 'in_progress'); }, [s.status, timer]);
  useEffect(() => {
    if (photoSeq === undefined) return;
    const sub = BackHandler.addEventListener('hardwareBackPress', () => { setPhotoSeq(undefined); return true; });
    return () => sub.remove();
  }, [photoSeq]);
  const state = useRef({ drafts, seq });
  const mounted = useRef(false);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  useEffect(() => {
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

  const setDraftFor = useCallback((target: number, v: string) => {
    const next = { ...state.current.drafts, ...loadDrafts(s.id), [target]: v };
    state.current = { ...state.current, drafts: next };
    saveDrafts(s.id, next);
    setDrafts(next);
  }, [s.id]);
  const applyPhotoAnswer = useCallback((it: PaperItem) => {
    if (!mounted.current) return false;
    synced.current[it.seq] = it.draft_text ?? '';
    setDraftFor(it.seq, it.draft_text ?? '');
    jobsRef.current = jobsRef.current.map((job) => it.handwriting?.find((candidate) => candidate.id === job.id) ?? job);
    const edits = { ...photoEditsRef.current };
    for (const job of it.handwriting ?? []) if (job.status === 'confirmed') delete edits[job.idempotency_key];
    photoEditsRef.current = edits; setPhotoEdits(edits); savePhotoEdits(s.id, edits);
    qc.setQueryData<PaperSession>(paperKeys.session(s.id), (previous) => previous ? { ...previous, items: previous.items.map((item) => item.seq === it.seq ? { ...item, ...it } : item) } : previous);
    return true;
  }, [qc, s.id, setDraftFor]);

  const sync = useCallback(() => {
    // 串行同步：切题、定时保存、交卷不能互相覆盖；失败沿用同一幂等键重试。
    const run = async () => {
      await photoQueue.recoverConfirmations(applyPhotoAnswer);
      if (!mounted.current) return;
      const d = state.current.drafts;
      saveDrafts(s.id, d);
      markActive(s.id);
      for (const k of Object.keys(d).map(Number)) {
        const throughSeconds = timer.seconds(k);
        while (true) {
          let write = pendingWrites.current[k];
          if (!write) {
            const delta = timer.pending(k, throughSeconds);
            if (d[k] === synced.current[k] && delta === 0) break;
            write = { draft: d[k] ?? '', delta, total: throughSeconds, key: Crypto.randomUUID() };
            pendingWrites.current[k] = write;
          }
          const res = await api.PUT('/paper-sessions/{sessionId}/items/{seq}', {
            params: { path: { sessionId: s.id, seq: k } },
            headers: { 'Idempotency-Key': write.key },
            body: { draft_text: write.draft, time_spent_seconds: write.total },
          });
          if (res.response.status === 409) {
            void qc.invalidateQueries({ queryKey: paperKeys.session(s.id) });
          }
          await unwrap(Promise.resolve(res));
          timer.acknowledge(k, write.delta);
          synced.current[k] = write.draft;
          delete pendingWrites.current[k];
        }
      }
      setSyncError(false);
    };
    const next = syncQueue.current.catch(() => undefined).then(run);
    syncQueue.current = next;
    return next.catch((e: unknown) => { setSyncError(true); throw e; });
  }, [applyPhotoAnswer, photoQueue, qc, s.id, timer]);

  useEffect(() => {
    if (paused) return;
    const t = setInterval(() => { void sync().catch(() => undefined); }, SYNC_MS);
    return () => clearInterval(t);
  }, [paused, sync]);

  const submit = useMutation({
    mutationFn: async (automatic: boolean) => {
      if (recording && !automatic) throw new Error('请先结束录音再交卷');
      if (photoAction.current && !automatic) throw new Error('正在保存识别结果，请稍候再交卷');
      if (!automatic) {
        await photoQueue.waitForUploads();
        const failed = jobsRef.current.find((job) => job.status === 'failed');
        if (failed) throw new Error(`第 ${failed.seq} 题识别失败，请重试或弃用文件后交卷`);
      }
      try { await sync(); } catch (error) { if (!automatic || !(error instanceof ApiError) || error.status !== 409) throw error; }
      // 已修改但没点确认的文字也随交卷保存；未修改的结果由服务端默认确认。
      if (!automatic) for (const job of jobsRef.current) {
        const edited = photoEditsRef.current[job.idempotency_key];
        if (job.status === 'ready' && edited !== undefined) {
          await savePhotoAnswer(job, edited, false);
        }
      }
      return unwrap(api.POST('/paper-sessions/{sessionId}/submit', { params: { path: { sessionId: s.id } } }));
    },
    onSuccess: (v) => {
      track('paper_submit', { mode: v.mode });
      clearDrafts(s.id);
      photoQueue.clearRegistered();
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
      toast('考试时间到，已登记的照片和录音会继续识别后批改');
      submitNow(true, { onError: () => void qc.invalidateQueries({ queryKey: paperKeys.session(s.id) }) });
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
      timer.setRunning(true);
      markActive(s.id);
      setSession(v);
    },
    onError: (e) => toast(e instanceof Error ? e.message : '没能恢复'),
  });
  const mark = useMutation({
    mutationFn: (v: boolean) => unwrap(api.PUT('/paper-sessions/{sessionId}/items/{seq}', { params: { path: { sessionId: s.id, seq } }, body: { marked: v } })),
    onSuccess: (it) => setSession({ ...s,
      server_now: new Date(Date.now() + fetchedServer - fetchedAt).toISOString(),
      elapsed_seconds: Math.floor(paused ? s.elapsed_seconds : s.elapsed_seconds + (Date.now() - fetchedAt) / 1000),
      items: s.items.map((x) => (x.seq === it.seq ? { ...x, marked: it.marked } : x)),
    }),
  });

  const go = (to: number) => {
    if (recording) { toast('请先结束录音，识别可在切题后继续'); return; }
    timer.select(to);
    void sync().catch(() => undefined);
    state.current = { ...state.current, seq: to };
    setPhotoSeq(undefined);
    setSeq(to);
    setCardOpen(false);
  };
  const setDraft = (v: string) => setDraftFor(seq, v);
  const savePhotoAnswer = (job: PaperHandwriting, recognized: string, confirm = true) => {
    // 与草稿写入共用队列，防止一个较早发出的定时保存覆盖刚确认的文字。
    const next = syncQueue.current.catch(() => undefined).then(async () => {
      await photoQueue.confirm(job, recognized, confirm, applyPhotoAnswer);
    });
    syncQueue.current = next;
    return next;
  };
  const confirmPhoto = async (job: PaperHandwriting, recognized: string) => {
    if (photoAction.current || submitting || timeUp) return;
    photoAction.current = true; setPhotoBusy(true);
    try {
      await sync();
      await savePhotoAnswer(job, recognized);
    } catch (error) { toast(error instanceof Error ? error.message : '保存识别结果失败，请重试'); }
    finally { photoAction.current = false; setPhotoBusy(false); }
  };
  const retryPhoto = async (photo: LocalPhoto | PaperHandwriting) => {
    try {
      if ('key' in photo) await photoQueue.run(photo.key);
      else await unwrap(api.POST('/paper-sessions/{sessionId}/items/{seq}/handwriting', { params: { path: { sessionId: s.id, seq: photo.seq } }, body: { object_keys: photo.object_keys, idempotency_key: photo.idempotency_key, capture_order: photo.capture_order, input_kind: photo.input_kind } }));
      await qc.invalidateQueries({ queryKey: paperKeys.session(s.id) });
    } catch (error) { toast(error instanceof Error ? error.message : '重试失败，原文件已保留'); }
  };
  const discardPhotos = async (photo: LocalPhoto | PaperHandwriting) => {
    try {
      let job = 'id' in photo ? photo : undefined;
      if ('key' in photo && photo.keys) {
        // 响应丢失也可能已经在服务端登记；先找回同一个任务再弃用，不能只删本地记录。
        job = await unwrap(api.POST('/paper-sessions/{sessionId}/items/{seq}/handwriting', { params: { path: { sessionId: s.id, seq: photo.seq } }, body: { object_keys: photo.keys, idempotency_key: photo.key, capture_order: photo.order ?? 0, input_kind: photo.inputKind } }));
      }
      if (job) {
        await unwrap(api.DELETE('/paper-sessions/{sessionId}/items/{seq}/handwriting/{handwritingId}', { params: { path: { sessionId: s.id, seq: job.seq, handwritingId: job.id } } }));
        await qc.invalidateQueries({ queryKey: paperKeys.session(s.id) });
      }
      if ('key' in photo) photoQueue.discardLocal(photo.key);
    } catch (error) { toast(error instanceof Error ? error.message : '弃用失败，原文件仍然保留'); }
  };
  const photoStates: Record<number, string> = {};
  for (const job of jobs) {
    if (job.status === 'confirmed') continue;
    const status = job.status === 'failed' ? '识别失败' : job.status === 'ready' ? '待核对' : '识别中';
    if (photoStates[job.seq] !== '识别失败') photoStates[job.seq] = status;
  }
  for (const photo of Object.values(photos)) if (photo.status !== 'registered') photoStates[photo.seq] = photo.status === 'failed' ? '上传失败' : '上传中';
  const pendingPhotoCount = Object.keys(photoStates).length;
  const confirmingPhoto = Object.values(photos).some((photo) => photo.seq === seq && photo.confirmingText !== undefined);
  const openPhoto = () => {
    if (recording) { toast('请先结束录音'); return; }
    const savedCount = jobs.filter((job) => job.seq === seq && job.input_kind !== 'voice').reduce((n, job) => n + job.object_keys.length, 0);
    const localCount = Object.values(photos).filter((photo) => photo.seq === seq && photo.status !== 'registered' && photo.inputKind !== 'voice').reduce((n, photo) => n + photo.shots.length, 0);
    if (savedCount + localCount >= 6) { toast('一道题最多拍 6 张，请先核对或弃用已有照片'); return; }
    setPhotoSeq(seq);
  };
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
  const unanswered = s.items.filter((it) => (drafts[it.seq] ?? '').trim() === '' && !jobs.some((job) => job.seq === it.seq) && !Object.values(photos).some((photo) => photo.seq === it.seq && photo.status !== 'registered')).length;
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
      {submitting ? <Text variant="caption">正在保存答卷，等待照片和录音上传并登记完成；已登记的文件会在后台识别后批改。</Text> : pendingPhotoCount ? <Text variant="small">{pendingPhotoCount} 题文件待处理，可继续切题，返回原题核对。</Text> : null}
      {syncError ? <Text variant="small" color={semantic.danger}>草稿已保存在本机，暂未同步。恢复网络后会重试，交卷前需同步成功。</Text> : null}
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
              第 <Text variant="small" num={600} color={semantic.textPrimary}>{seq}</Text> / {s.items.length} 题 · {qtypeNames[item.qtype]} · {item.score} 分
            </Text>
            {item.origin_tags?.includes('ai_generated') ? <Tag label="AI 变式题" tone="ai" /> : null}
            <Pressable accessibilityRole="button" accessibilityLabel={item.marked ? '已标记' : '标记'} onPress={() => mark.mutate(!item.marked)} style={[styles.mark, item.marked && styles.markOn]}>
              <View style={[styles.markDot, item.marked && styles.markDotOn]} />
              <Text variant="small" color={semantic.textPrimary}>
                {item.marked ? '已标记' : '标记'}
              </Text>
            </Pressable>
          </View>
          <GroupNote group={item.group} />
          {item.passage ? <PassageCard key={item.seq} text={item.passage.text} lines={6} defaultOpen /> : null}
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
              {photoSeq === seq ? <HandwritingFlow
                confirmLabel="确认并填入答案"
                onCancel={() => setPhotoSeq(undefined)}
                onCapture={(shots) => {
                  if (timeUp || submitting) { toast('考试已结束，不能再增加照片'); return; }
                  photoQueue.add(seq, shots);
                  setPhotoSeq(undefined);
                  toast('照片已保存在本题，正在后台上传和识别，可以继续下一题');
                }}
                onConfirm={() => undefined}
              /> : <>
              <TextInput
                accessibilityLabel="你的答案"
                multiline
                editable={!submitting && !photoBusy && !timeUp && !confirmingPhoto}
                value={text}
                onChangeText={setDraft}
                placeholder="写下你的答案"
                placeholderTextColor={semantic.textSecondary}
                style={styles.answer}
                textAlignVertical="top"
              />
              {voiceEnabled ? <VoiceRecorder disabled={submitting || photoBusy || timeUp || confirmingPhoto} onRecordingChange={setRecording} onCapture={(clip) => { photoQueue.addVoice(seq, clip); }} /> : null}
              <View style={styles.answerTools}>
              {!submitting && !photoBusy && !timeUp ? <Tool icon="camera" label="拍手写稿" onPress={openPhoto} /> : null}
              <Text variant="small" num style={styles.wordCount}>
                {chars(text)} 字{item.required_words ? ` · 建议 ${item.required_words} 字左右` : ''}
              </Text>
              </View>
              </>}
              {confirmingPhoto ? <View style={styles.gap}><Text variant="caption">正在保存本题识别结果，网络恢复后会自动重试，可以先做其他题。</Text><Button title="重试保存识别结果" kind="text" disabled={photoBusy || submitting || timeUp} onPress={() => { void sync().catch(() => toast('暂未同步成功，识别结果已保留')); }} /></View> : null}
              <PaperPhotoReview jobs={jobs.filter((job) => job.seq === seq)} local={Object.values(photos).filter((photo) => photo.seq === seq)} edits={photoEdits} disabled={submitting || photoBusy || timeUp || confirmingPhoto}
                onEdit={editPhoto} onConfirm={(job, value) => void confirmPhoto(job, value)} onRetry={(photo) => void retryPhoto(photo)} onDiscard={setDiscardPhoto} />
            </>
          )}
          <Text variant="small" num>本题已用 {clock(timer.seconds(seq))}</Text>
        </ScrollView>
      )}
      {!paused ? (
        <View style={styles.nav}>
          <Button title="上一题" kind="secondary" disabled={idx <= 0 || submitting || photoBusy} onPress={() => go(s.items[idx - 1]!.seq)} style={styles.flex} />
          {idx < s.items.length - 1 ? (
            <Button title="下一题" disabled={submitting || photoBusy} onPress={() => go(s.items[idx + 1]!.seq)} style={styles.flex} />
          ) : (
            <Button title="交卷" disabled={photoBusy} loading={submitting} onPress={() => setConfirm(true)} style={styles.flex} />
          )}
        </View>
      ) : null}
      <BottomSheet visible={cardOpen} onClose={() => setCardOpen(false)} title="答题卡">
        <AnswerCard s={s} drafts={drafts} seq={seq} onPick={(target) => { if (!submitting && !photoBusy) go(target); }} photoStates={photoStates} />
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
        message={`${pendingPhotoCount ? `有 ${pendingPhotoCount} 题照片或录音待处理，上传完成后交卷，后台继续识别，未核对的结果默认采用。` : ''}${unanswered > 0 ? `还有 ${unanswered} 题未作答，` : ''}${markedCount > 0 ? `${markedCount} 题已标记。` : ''}交卷后开始整卷批改，大约需要 2 分钟。按你资料里的采分点逐题批改。`}
        confirmText="交卷"
        cancelText="继续作答"
        onCancel={() => setConfirm(false)}
        onConfirm={() => {
          setConfirm(false);
          submit.mutate(false);
        }}
      />
      <ConfirmDialog visible={discardPhoto !== undefined} title="弃用这份文件？" message="弃用后不会将这份照片或录音用于本题作答，已经写下的答案会保留。" confirmText="弃用" onCancel={() => setDiscardPhoto(undefined)} onConfirm={() => { if (discardPhoto) void discardPhotos(discardPhoto); setDiscardPhoto(undefined); }} />
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
            void sync().catch(() => undefined);
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
  if (!session.data) return <Screen><ErrorState error={session.error} onRetry={() => void session.refetch()} /></Screen>;
  const s = session.data;
  if (s.status === 'abandoned') {
    return (
      <Screen>
        <ErrorState error={new Error('这套卷已经放弃了')} onRetry={() => router.back()} />
      </Screen>
    );
  }
  if (s.status === 'grading' || s.status === 'graded') return <ResultView s={s} refreshError={session.isError} onRefresh={() => void session.refetch()} />;
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
  banner: { flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 12, paddingHorizontal: 16, borderRadius: radius.lg, backgroundColor: colors.ink, marginBottom: spacing.md },
  bannerDanger: { backgroundColor: semantic.danger },
  qHead: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs },
  mark: { flexDirection: 'row', alignItems: 'center', gap: 4, minHeight: 30, paddingHorizontal: 10, borderRadius: radius.pill, borderWidth: 1, borderColor: semantic.border },
  markOn: { borderColor: colors.peach, backgroundColor: semantic.peachSoft },
  markDot: { width: 6, height: 6, borderRadius: 3, backgroundColor: colors.placeholder },
  markDotOn: { backgroundColor: colors.peach },
  option: { flexDirection: 'row', alignItems: 'center', gap: 12, minHeight: 52, paddingVertical: 12, paddingHorizontal: 14, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  optionOn: { borderColor: semantic.primary, borderWidth: 1.5 },
  answer: { minHeight: 260, padding: 14, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 15, lineHeight: 28, color: semantic.textPrimary },
  nav: { flexDirection: 'row', gap: 10, paddingVertical: spacing.sm },
  answerTools: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: spacing.sm, paddingVertical: spacing.sm },
  wordCount: { flex: 1, flexShrink: 1, textAlign: 'right' },
  sheetNav: { marginTop: spacing.lg, marginBottom: spacing.lg },
  counts: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.lg },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  cell: { width: 56, minHeight: 56, paddingVertical: 4, alignItems: 'center', justifyContent: 'center', borderRadius: 14, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  cellDone: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  cellOn: { borderWidth: 2, borderColor: semantic.textPrimary },
  flag: { position: 'absolute', top: 4, right: 4, width: 8, height: 8, borderRadius: 4, backgroundColor: colors.peach },
  resultRow: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 48, borderTopWidth: 1, borderTopColor: semantic.border },
  seq: { width: 28, fontFamily: fontFamily.numberSemiBold },
});
