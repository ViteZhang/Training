// 5.2 写作文：题目、计时（真题默认按考试时长倒计时，可关闭）、提交；正文编辑器，字数 / 要求字数；每 5 秒存草稿；
// 工具栏：素材（5.3 素材弹层：来自写作笔记、按主题分组、AI 补充单独标、插入到光标处、去素材库看更多）、拍照上传（5.4）。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Redirect, router, useLocalSearchParams } from 'expo-router';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, ConfirmDialog, ErrorState, Loading, QuotaSheet, Screen, Tag, Text, toast } from '@/components';
import { clearEssayDraft, countWords, essayKeys, loadEssayDraft, saveEssayDraft, sourceNames, useEssay, useEssayKB, type Essay } from '@/features/essay/api';
import { clock } from '@/features/paper/api';
import { HandwritingFlow } from '@/features/practice/handwriting';
import { api, unwrap } from '@/lib/api';

const SYNC_MS = 5000;

type Material = Schemas['EssayKnowledgeBase']['materials'][number];

function MaterialSheet({ subjectId, visible, onClose, onInsert }: { subjectId: number; visible: boolean; onClose: () => void; onInsert: (m: Material) => void }) {
  const kb = useEssayKB(subjectId, visible);
  const [theme, setTheme] = useState<string>();
  const list = kb.data?.materials ?? [];
  const themes = [...new Set(list.map((m) => m.theme))];
  const cur = theme ?? themes[0];
  return (
    <BottomSheet visible={visible} onClose={onClose} title="素材">
      {kb.isLoading ? <Loading rows={3} /> : kb.isError ? <ErrorState error={kb.error} onRetry={() => void kb.refetch()} /> : (
        <View style={styles.gap}>
          <Text variant="caption">来自你的写作笔记 · {list.length} 条</Text>
          {list.length === 0 ? <Text variant="caption">还没有素材。导入写作笔记后，AI 会按主题整理成素材库</Text> : null}
          <ScrollView horizontal contentContainerStyle={styles.row}>
            {themes.map((t) => (
              <Button key={t} title={t} kind={t === cur ? 'primary' : 'secondary'} onPress={() => setTheme(t)} />
            ))}
          </ScrollView>
          <ScrollView style={styles.sheetList}>
            {list
              .filter((m) => m.theme === cur)
              .map((m) => (
                <View key={m.id} style={styles.material}>
                  <View style={styles.flex}>
                    {m.origin === 'ai_generated' ? <Tag label="AI 补充" tone="ai" /> : null}
                    <Text variant="body">{m.content}</Text>
                  </View>
                  <Button title="插入" kind="text" onPress={() => onInsert(m)} />
                </View>
              ))}
          </ScrollView>
          <Button
            title="去素材库看更多"
            kind="text"
            onPress={() => {
              onClose();
              router.push('/(tabs)/bank');
            }}
          />
        </View>
      )}
    </BottomSheet>
  );
}

function Writer({ e }: { e: Essay }) {
  const qc = useQueryClient();
  const [text, setText] = useState(() => loadEssayDraft(e.id)?.content ?? e.content);
  const [seconds, setSeconds] = useState(() => Math.max(loadEssayDraft(e.id)?.seconds ?? 0, e.duration_seconds));
  const [timed, setTimed] = useState(e.timed);
  const [sel, setSel] = useState({ start: text.length, end: text.length });
  const [materials, setMaterials] = useState(false);
  const [photo, setPhoto] = useState(false);
  const [leave, setLeave] = useState(false);
  const [quotaOut, setQuotaOut] = useState(false);
  const [saved, setSaved] = useState(true);
  const synced = useRef({ content: e.content, seconds: e.duration_seconds, timed: e.timed });
  const state = useRef({ text, seconds, timed });
  useEffect(() => {
    state.current = { text, seconds, timed };
  }, [text, seconds, timed]);

  // 计时：页面打开时每秒加 1（离开后不计时）。
  useEffect(() => {
    const t = setInterval(() => setSeconds((s) => s + 1), 1000);
    return () => clearInterval(t);
  }, []);

  const sync = useCallback(async (extra?: { photo_keys?: string[]; content?: string }) => {
    const cur = state.current;
    const content = extra?.content ?? cur.text;
    saveEssayDraft(e.id, { content, seconds: cur.seconds });
    const s = synced.current;
    if (!extra && s.content === content && s.timed === cur.timed && Math.abs(s.seconds - cur.seconds) < 30) return;
    const v = await unwrap(
      api.PUT('/essays/{essayId}/draft', { params: { path: { essayId: e.id } }, body: { content, duration_seconds: cur.seconds, timed: cur.timed, ...extra } }),
    );
    synced.current = { content, seconds: cur.seconds, timed: cur.timed };
    qc.setQueryData(essayKeys.essay(e.id), v);
    setSaved(true);
  }, [e.id, qc]);

  useEffect(() => {
    const t = setInterval(() => void sync().catch(() => setSaved(false)), SYNC_MS);
    return () => clearInterval(t);
  }, [sync]);

  const submit = useMutation({
    mutationFn: async (extra?: { photo_keys?: string[]; content?: string }) => {
      await sync(extra ?? { content: state.current.text });
      return unwrap(api.POST('/essays/{essayId}/submit', { params: { path: { essayId: e.id } } }));
    },
    onSuccess: (v) => {
      clearEssayDraft(e.id);
      qc.setQueryData(essayKeys.essay(e.id), v);
      void qc.invalidateQueries({ queryKey: essayKeys.home(e.subject_id) });
      router.replace({ pathname: '/essay/result/[id]', params: { id: String(e.id) } });
    },
    onError: (err) => (err instanceof ApiError && err.isQuotaExceeded ? setQuotaOut(true) : toast(err instanceof Error ? err.message : '提交失败，请重试')),
  });

  const insert = (m: Material) => {
    const next = text.slice(0, sel.start) + m.content + text.slice(sel.end);
    setText(next);
    setSaved(false);
    const pos = sel.start + m.content.length;
    setSel({ start: pos, end: pos });
    setMaterials(false);
  };

  const words = countWords(text);
  const limit = e.time_limit_minutes * 60;
  const left = limit - seconds;
  return (
    <Screen>
      <View style={styles.top}>
        <Button title="退出" kind="text" onPress={() => setLeave(true)} />
        {timed ? (
          <Text variant="number" color={left <= 0 ? semantic.danger : undefined} accessibilityLabel="剩余时间">
            {left > 0 ? clock(left) : '时间到'}
          </Text>
        ) : (
          <Text variant="caption">不计时</Text>
        )}
        <Button title="提交" loading={submit.isPending} onPress={() => submit.mutate(undefined)} />
      </View>
      <ScrollView contentContainerStyle={styles.scroll} keyboardShouldPersistTaps="handled">
        <View style={styles.row}>
          <Tag label={e.topic_source === 'ai' ? 'AI 出题' : sourceNames[e.topic_source]} tone={e.topic_source === 'ai' ? 'ai' : 'neutral'} />
          {e.draft_no > 1 ? <Tag label={`第 ${e.draft_no} 稿`} /> : null}
          <View style={styles.flex} />
          <Button title={timed ? '关闭计时' : '开启计时'} kind="text" onPress={() => setTimed((v) => !v)} />
        </View>
        <Text variant="bodyStrong">{e.topic}</Text>
        {e.status === 'failed' && e.fail_reason ? (
          <Text variant="caption" color={semantic.danger}>
            {e.fail_reason}
          </Text>
        ) : null}
        <TextInput
          accessibilityLabel="作文正文"
          multiline
          value={text}
          onChangeText={(v) => {
            setText(v);
            setSaved(false);
          }}
          selection={sel}
          onSelectionChange={(ev) => setSel(ev.nativeEvent.selection)}
          placeholder="在这里写作文，空一行分段"
          placeholderTextColor={semantic.textSecondary}
          style={styles.editor}
          textAlignVertical="top"
        />
      </ScrollView>
      <View style={styles.toolbar}>
        <Button title="素材" kind="text" onPress={() => setMaterials(true)} />
        <Button title="拍照上传" kind="text" onPress={() => setPhoto(true)} />
        <View style={styles.flex} />
        <Text variant="caption">
          {words}
          {e.required_words ? ` / ${e.required_words}` : ''} 字 · {saved ? '草稿已保存' : '保存中'}
        </Text>
      </View>
      <MaterialSheet subjectId={e.subject_id} visible={materials} onClose={() => setMaterials(false)} onInsert={insert} />
      <BottomSheet visible={photo} onClose={() => setPhoto(false)} title="上传手写稿">
        <Text variant="caption">按页码顺序拍摄，一张拍一页；识别后可以修改，再提交批改</Text>
        <HandwritingFlow
          onConfirm={(t, keys) => {
            setText(t);
            setPhoto(false);
            submit.mutate({ content: t, photo_keys: keys });
          }}
          onCancel={() => setPhoto(false)}
        />
      </BottomSheet>
      <ConfirmDialog
        visible={leave}
        title="先离开？"
        message="草稿已保存，回来可以接着写。"
        confirmText="离开"
        cancelText="继续写"
        onCancel={() => setLeave(false)}
        onConfirm={() => {
          setLeave(false);
          void sync().finally(() => router.back());
        }}
      />
      <QuotaSheet
        visible={quotaOut}
        onClose={() => setQuotaOut(false)}
        title="本周的作文批改次数用完了"
        desc="免费版每周可批改 1 篇作文，下周一恢复。草稿已保存，可以先写着。"
        onUpgrade={() => toast('会员马上上线')}
        freeOptions={[{ label: '先保存草稿', onPress: () => router.back() }]}
      />
    </Screen>
  );
}

export default function EssayWritePage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const essay = useEssay(Number(id));
  if (essay.isLoading) return <Screen><Loading rows={6} /></Screen>;
  if (essay.isError || !essay.data) return <Screen><ErrorState error={essay.error} onRetry={() => void essay.refetch()} /></Screen>;
  const e = essay.data;
  if (e.status === 'grading' || e.status === 'graded') return <Redirect href={{ pathname: '/essay/result/[id]', params: { id: String(e.id) } }} />;
  return <Writer key={e.id} e={e} />;
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.sm, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  top: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  editor: { minHeight: 360, padding: spacing.md, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 17, lineHeight: 28 },
  toolbar: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
  sheetList: { maxHeight: 320 },
  material: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, paddingVertical: spacing.sm, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: semantic.border },
});
