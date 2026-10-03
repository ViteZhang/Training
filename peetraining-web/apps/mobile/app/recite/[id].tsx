// 4.14 背诵：挖空 / 默写 / 口述三种模式切换（口述受功能开关控制）；标原文出处；
// 挖空自评没记住 / 模糊 / 记住了；默写与口述显示关键词覆盖（4.15）。背完进入 4.17。
import { semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router, useLocalSearchParams } from 'expo-router';
import { useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, ConfirmDialog, EmptyState, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { Segments } from '@/features/import/ui';
import { modeNames, reciteKeys, useReciteSession, type ReciteMode } from '@/features/recite/api';
import { Cloze, CoverageView, Dictation, Oral, type RecordBody, type RecordResult } from '@/features/recite/ItemView';
import { api, unwrap } from '@/lib/api';

export default function RecitePage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const sessionId = Number(id);
  const qc = useQueryClient();
  const session = useReciteSession(sessionId);
  const [current, setCurrent] = useState<number | null>(null);
  const [mode, setMode] = useState<ReciteMode>('cloze');
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<RecordResult>();
  const [done, setDone] = useState<Set<number>>(new Set());
  const [exiting, setExiting] = useState(false);
  const s = session.data;
  const resumeAt = useMemo(() => {
    if (!s) return 0;
    const i = s.items.findIndex((it) => !it.result);
    return i < 0 ? Math.max(0, s.items.length - 1) : i;
    // 只在第一次拿到会话时定断点。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [s?.id]);
  const index = current ?? resumeAt;

  if (session.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (session.isError || !s) return <Screen><ErrorState error={session.error} onRetry={() => void session.refetch()} /></Screen>;
  if (s.items.length === 0) return <Screen><EmptyState title="这一轮没有可背的内容" actionText="返回" onAction={() => router.back()} /></Screen>;
  const item = s.items[index]!;
  const finished = s.items.filter((it) => it.result || done.has(it.kp_id)).length;
  const modes: ReciteMode[] = s.oral_enabled ? ['cloze', 'dictation', 'oral'] : ['cloze', 'dictation'];

  const next = () => {
    setResult(undefined);
    if (index + 1 < s.items.length) {
      setCurrent(index + 1);
      return;
    }
    void qc.invalidateQueries({ queryKey: ['practice', 'home'] });
    void qc.invalidateQueries({ queryKey: ['home'] });
    router.replace({ pathname: '/recite/done/[id]', params: { id: String(sessionId), subjectId: String(s.subject_id) } });
  };

  const record = async (body: RecordBody) => {
    setBusy(true);
    try {
      const r = await unwrap(
        api.POST('/recite-sessions/{sessionId}/records', { params: { path: { sessionId } }, body: { kp_id: item.kp_id, idempotency_key: Crypto.randomUUID(), ...body } }),
      );
      setDone((d) => new Set(d).add(item.kp_id));
      if (body.mode === 'cloze') next();
      else setResult(r);
    } catch (e) {
      toast(e instanceof Error ? e.message : '没记上，请重试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <View style={styles.top}>
        <Pressable accessibilityRole="button" accessibilityLabel="退出背诵" onPress={() => setExiting(true)} style={styles.close}>
          <Text variant="h3">×</Text>
        </Pressable>
        <Text variant="bodyStrong" style={styles.flex}>
          {s.title}
        </Text>
        <Text variant="caption">
          {index + 1} / {s.items.length}
        </Text>
      </View>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Segments<ReciteMode>
          value={mode}
          onChange={(m) => {
            setMode(m);
            setResult(undefined);
          }}
          options={modes.map((m) => ({ key: m, label: modeNames[m] }))}
        />
        {item.path.length > 0 ? <Text variant="caption">{item.path.join(' · ')}</Text> : null}
        <Text variant="h2">{item.name}</Text>
        {item.source_ref ? (
          <Text variant="caption">
            原文出自 {item.source_ref.file_name}
            {item.source_ref.page ? ` 第 ${item.source_ref.page} 页` : ''}
          </Text>
        ) : null}
        {result ? (
          <>
            <CoverageView item={item} result={result} />
            <View style={styles.row}>
              <Button title={mode === 'oral' ? '再说一次' : '再默写一次'} kind="secondary" style={styles.flex} onPress={() => setResult(undefined)} />
              <Button title={index + 1 < s.items.length ? '下一条' : '背完了'} style={styles.flex} onPress={next} />
            </View>
          </>
        ) : mode === 'cloze' ? (
          <Cloze key={`c${item.kp_id}`} item={item} busy={busy} onAssess={(r) => void record({ mode: 'cloze', result: r })} />
        ) : mode === 'dictation' ? (
          <Dictation key={`d${item.kp_id}`} item={item} busy={busy} onSubmit={(text) => void record({ mode: 'dictation', text })} />
        ) : (
          <Oral key={`o${item.kp_id}`} item={item} sessionId={sessionId} busy={busy} onSubmit={(audio_key) => void record({ mode: 'oral', audio_key })} />
        )}
        <Text variant="small" color={semantic.textSecondary}>
          背诵按遗忘规律安排复习：没记住明天再背，模糊 2 天后，记住了 3 天后起逐步拉长
        </Text>
      </ScrollView>
      <ConfirmDialog
        visible={exiting}
        title="先休息一下？"
        message={`这一轮已背 ${finished} / ${s.items.length} 条，下次从没背的继续。`}
        confirmText="退出"
        cancelText="继续背"
        onConfirm={() => {
          setExiting(false);
          void qc.invalidateQueries({ queryKey: reciteKeys.session(sessionId) });
          router.back();
        }}
        onCancel={() => setExiting(false)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  top: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm },
  close: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  flex: { flex: 1 },
  row: { flexDirection: 'row', gap: spacing.sm },
  scroll: { gap: spacing.md, paddingBottom: spacing.xl },
});
