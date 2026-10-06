// 4.14 挖空、4.15 默写结果、4.16 口述：一条知识点的三种背法。
import type { Schemas } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { RecordingPresets, requestRecordingPermissionsAsync, setAudioModeAsync, useAudioRecorder } from 'expo-audio';
import { File } from 'expo-file-system';
import { useEffect, useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { Button, Text, toast } from '@/components';
import { usePermissionPrompt } from '@/features/permissions/PermissionPrompt';
import { api, unwrap } from '@/lib/api';
import { levelNames, type ReciteItem, type ReciteLevel } from './api';

export type RecordResult = Schemas['ReciteRecordResult'];
export type RecordBody = { mode: Schemas['ReciteMode']; result?: ReciteLevel; text?: string; audio_key?: string };

/** 挖空：采分关键词遮住，点空格查看，「全部显示」；自评没记住 / 模糊 / 记住了。 */
export function Cloze({ item, busy, onAssess }: { item: ReciteItem; busy: boolean; onAssess: (r: ReciteLevel) => void }) {
  const [shown, setShown] = useState<Set<number>>(new Set());
  const blanks = item.segments.map((s, i) => (s.blank ? i : -1)).filter((i) => i >= 0);
  const all = blanks.length > 0 && blanks.every((i) => shown.has(i));
  return (
    <View style={styles.gap}>
      <Text variant="small">点空格查看</Text>
      <Text variant="body" style={styles.text}>
        {item.segments.map((s, i) =>
          s.blank ? (
            <Text
              key={i}
              variant="body"
              accessibilityRole="button"
              accessibilityLabel={shown.has(i) ? s.text : `第 ${blanks.indexOf(i) + 1} 个空`}
              onPress={() => setShown((v) => new Set(v).add(i))}
              color={shown.has(i) ? semantic.primary : semantic.textSecondary}
              style={shown.has(i) ? styles.shown : styles.blank}
            >
              {shown.has(i) ? s.text : '　'.repeat(Math.max(2, [...s.text].length))}
            </Text>
          ) : (
            <Text key={i} variant="body">
              {s.text}
            </Text>
          ),
        )}
      </Text>
      {blanks.length > 0 && !all ? <Button title="全部显示" kind="soft" size="sm" style={styles.left} onPress={() => setShown(new Set(blanks))} /> : null}
      <View style={styles.row}>
        {(['forgot', 'vague', 'remembered'] as const).map((r) => (
          <Button
            key={r}
            title={levelNames[r]}
            kind={r === 'remembered' ? 'primary' : r === 'vague' ? 'soft' : 'secondary'}
            color={r === 'forgot' ? semantic.danger : undefined}
            style={styles.flex}
            disabled={busy}
            onPress={() => onAssess(r)}
          />
        ))}
      </View>
    </View>
  );
}

/** 4.15 默写 / 口述的关键词比对结果：原文里写对的关键词标绿、遗漏的标红。 */
export function CoverageView({ item, result }: { item: ReciteItem; result: RecordResult }) {
  const hit = new Map((result.coverage?.keywords ?? []).map((k) => [k.text, k.hit]));
  return (
    <View style={styles.gap}>
      <View style={styles.row}>
        <Text variant="caption" style={styles.flex}>
          关键词覆盖
        </Text>
        <Text variant="score">{result.coverage?.hit ?? 0}</Text>
        <Text variant="body">/ {result.coverage?.total ?? 0}</Text>
      </View>
      <Text variant="bodyStrong" color={result.result === 'remembered' ? semantic.mastered : result.result === 'vague' ? semantic.info : semantic.danger}>
        {levelNames[result.result]}
      </Text>
      {result.transcript ? (
        <View style={styles.box}>
          <Text variant="caption">你说的是</Text>
          <Text variant="body">{result.transcript}</Text>
        </View>
      ) : null}
      <Text variant="caption">对照原文</Text>
      <Text variant="body" style={styles.text}>
        {item.segments.map((s, i) =>
          s.blank ? (
            <Text key={i} variant="bodyStrong" color={hit.get(s.text) ? semantic.mastered : semantic.danger}>
              {hit.get(s.text) ? s.text : `漏：${s.text}`}
            </Text>
          ) : (
            <Text key={i} variant="body">
              {s.text}
            </Text>
          ),
        )}
      </Text>
      <View style={styles.legend}>
        <Text variant="small" color={semantic.mastered}>
          ■ 关键词写对
        </Text>
        <Text variant="small" color={semantic.danger}>
          ■ 关键词遗漏
        </Text>
      </View>
    </View>
  );
}

/** 默写：不看原文写下来，按关键词比对（规则，不调模型）。 */
export function Dictation({ item, busy, onSubmit }: { item: ReciteItem; busy: boolean; onSubmit: (text: string) => void }) {
  const [text, setText] = useState('');
  return (
    <View style={styles.gap}>
      <Text variant="bodyStrong">默写：{item.name}</Text>
      <TextInput accessibilityLabel="默写" multiline value={text} onChangeText={setText} placeholder="不看原文，把它写下来" placeholderTextColor={semantic.textSecondary} style={styles.input} textAlignVertical="top" />
      <Button title="写好了，对照原文" disabled={!text.trim()} loading={busy} onPress={() => onSubmit(text)} />
    </View>
  );
}

function clock(sec: number) {
  return `${String(Math.floor(sec / 60)).padStart(2, '0')}:${String(sec % 60).padStart(2, '0')}`;
}

/** 4.16 口述：点一下开始录音，再点结束；录音上传后语音转文字，按关键词检查说到了哪些要点。 */
export function Oral({ item, sessionId, busy, onSubmit }: { item: ReciteItem; sessionId: number; busy: boolean; onSubmit: (audioKey: string) => void }) {
  const recorder = useAudioRecorder(RecordingPresets.HIGH_QUALITY);
  const [recording, setRecording] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [secs, setSecs] = useState(0);
  const mic = usePermissionPrompt('microphone', async () => {
    const r = await requestRecordingPermissionsAsync();
    return { granted: r.granted, canAskAgain: r.canAskAgain };
  });
  useEffect(() => {
    if (!recording) return;
    const t = setInterval(() => setSecs((s) => s + 1), 1000);
    return () => clearInterval(t);
  }, [recording]);

  const start = async () => {
    if (!(await mic.ensure())) return;
    await setAudioModeAsync({ allowsRecording: true, playsInSilentMode: true });
    await recorder.prepareToRecordAsync();
    recorder.record();
    setSecs(0);
    setRecording(true);
  };
  const stop = async () => {
    setRecording(false);
    await recorder.stop();
    const uri = recorder.uri;
    if (!uri) return toast('没有录到声音，请再试一次');
    setUploading(true);
    try {
      const f = new File(uri);
      const t = await unwrap(api.POST('/recite-sessions/{sessionId}/audio-upload', { params: { path: { sessionId } }, body: { content_type: 'audio/m4a', size: f.size || 1 } }));
      const r = await f.upload(t.upload_url, { httpMethod: 'PUT', headers: t.upload_headers });
      if (r.status < 200 || r.status >= 300) throw new Error('录音上传失败');
      onSubmit(t.object_key);
    } catch (e) {
      toast(e instanceof Error ? e.message : '录音上传失败，请重试');
    } finally {
      setUploading(false);
    }
  };
  return (
    <View style={styles.gap}>
      <Text variant="bodyStrong">口述：{item.name}</Text>
      <Text variant="caption">不看原文，用自己的话说出定义和特征</Text>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={recording ? '结束录音' : '开始录音'}
        disabled={busy || uploading}
        onPress={() => void (recording ? stop() : start())}
        style={[styles.mic, recording && styles.micOn]}
      >
        <Text variant="number" color={semantic.textOnBrand}>
          {recording ? clock(secs) : '录音'}
        </Text>
      </Pressable>
      <Text variant="caption">{recording ? '点击结束，AI 会检查你说到了哪些要点' : uploading || busy ? '正在识别…' : '点击开始录音'}</Text>
      {mic.element}
    </View>
  );
}

const styles = StyleSheet.create({
  gap: { gap: 12 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm },
  flex: { flex: 1 },
  text: { fontSize: 17, lineHeight: 30 },
  blank: { backgroundColor: '#EEEBFB', borderRadius: radius.sm },
  shown: { backgroundColor: '#EEEBFB', color: '#3E3190' },
  left: { alignSelf: 'flex-start' },
  box: { gap: spacing.xs, padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.infoSoft },
  legend: { flexDirection: 'row', gap: spacing.lg },
  input: { minHeight: 160, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, padding: spacing.md, fontSize: 16, lineHeight: 24, color: semantic.textPrimary, backgroundColor: semantic.surface },
  mic: { alignSelf: 'center', width: 96, height: 96, borderRadius: 48, alignItems: 'center', justifyContent: 'center', backgroundColor: semantic.primary },
  micOn: { backgroundColor: semantic.danger },
});
