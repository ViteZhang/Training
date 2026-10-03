// 4.5 拍手写稿识别：多张续拍；识别文字可修改；不确定的字词高亮并提示核对数量；「重拍」「再拍一张（续写）」「确认并提交批改」。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import * as ImagePicker from 'expo-image-picker';
import { File } from 'expo-file-system';
import { useState } from 'react';
import { Image, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { AIGenerating, Button, Text, toast } from '@/components';
import { usePermissionPrompt } from '@/features/permissions/PermissionPrompt';
import { api, unwrap } from '@/lib/api';

export type Recognized = Schemas['HandwritingResult'];
type Shot = { uri: string; mimeType: string; size: number };

const allowed = ['image/jpeg', 'image/png', 'image/heic', 'image/webp'] as const;
type PhotoType = (typeof allowed)[number];
const maxShots = 6;

function photoType(mime?: string | null): PhotoType {
  return (allowed as readonly string[]).includes(mime ?? '') ? (mime as PhotoType) : 'image/jpeg';
}

/** 上传照片并识别。返回照片的对象键与识别结果。 */
export async function uploadAndRecognize(shots: Shot[]): Promise<{ keys: string[]; result: Recognized }> {
  const req = await unwrap(
    api.POST('/handwriting/upload-requests', { body: { files: shots.map((s) => ({ content_type: photoType(s.mimeType), size: s.size })) } }),
  );
  await Promise.all(
    req.items.map(async (t, i) => {
      const r = await new File(shots[i]!.uri).upload(t.upload_url, { httpMethod: 'PUT', headers: t.upload_headers });
      if (r.status < 200 || r.status >= 300) throw new Error(`第 ${i + 1} 张上传失败`);
    }),
  );
  const keys = req.items.map((t) => t.object_key);
  const result = await unwrap(api.POST('/handwriting/recognize', { body: { object_keys: keys } }));
  return { keys, result };
}

/** 识别结果预览：不确定的字词标红（输入框里不能分色，所以单独显示一份）。 */
export function Highlighted({ text, ranges }: { text: string; ranges: Schemas['TextRange'][] }) {
  const chars = [...text];
  const parts: { s: string; low: boolean }[] = [];
  let pos = 0;
  for (const r of [...ranges].sort((a, b) => a.start - b.start)) {
    if (r.start > pos) parts.push({ s: chars.slice(pos, r.start).join(''), low: false });
    parts.push({ s: chars.slice(r.start, r.end).join(''), low: true });
    pos = r.end;
  }
  if (pos < chars.length) parts.push({ s: chars.slice(pos).join(''), low: false });
  return (
    <Text variant="body">
      {parts.map((p, i) =>
        p.low ? (
          <Text key={i} variant="body" color={semantic.danger} style={styles.low}>
            {p.s}
          </Text>
        ) : (
          p.s
        ),
      )}
    </Text>
  );
}

/** 拍照与 4.5 核对。onConfirm 交给批改；onCancel 回到打字作答。 */
export function HandwritingFlow({ onConfirm, onCancel, onStart }: { onConfirm: (text: string, keys: string[]) => void; onCancel: () => void; onStart?: () => void }) {
  const [shots, setShots] = useState<Shot[]>([]);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ keys: string[]; recognized: Recognized }>();
  const [text, setText] = useState('');
  const camera = usePermissionPrompt(
    'camera',
    async () => {
      const r = await ImagePicker.requestCameraPermissionsAsync();
      return { granted: r.granted, canAskAgain: r.canAskAgain };
    },
    { label: '改为打字作答', onPress: onCancel },
  );

  const capture = async (append: boolean) => {
    if (append && shots.length >= maxShots) {
      toast('一道题最多拍 6 张');
      return;
    }
    if (!(await camera.ensure())) return;
    const res = await ImagePicker.launchCameraAsync({ mediaTypes: ['images'], quality: 0.85 });
    if (res.canceled || !res.assets[0]) return;
    const a = res.assets[0];
    const shot = { uri: a.uri, mimeType: a.mimeType ?? 'image/jpeg', size: a.fileSize ?? new File(a.uri).size };
    const next = append ? [...shots, shot] : [shot];
    setShots(next);
    onStart?.();
    setBusy(true);
    try {
      const r = await uploadAndRecognize(next);
      setResult({ keys: r.keys, recognized: r.result });
      setText(r.result.text);
    } catch (e) {
      toast(e instanceof ApiError || e instanceof Error ? e.message : '识别失败，请重拍');
      if (!append) setShots([]);
      else setShots(shots);
    } finally {
      setBusy(false);
    }
  };

  if (busy) return <AIGenerating steps={['上传照片', '识别手写文字', '标出不确定的字词']} current={1} eta="每张大约 10 秒" />;
  if (!result) {
    return (
      <View style={styles.gap}>
        <Text variant="body">把答题纸放平、光线充足，一张拍一页；写了多页可以续拍。</Text>
        <Button title="拍手写稿" onPress={() => void capture(false)} />
        <Button title="改为打字作答" kind="text" onPress={onCancel} />
        {camera.element}
      </View>
    );
  }
  const n = result.recognized.uncertain_count;
  return (
    <View style={styles.gap}>
      <Text variant="h3">确认识别结果</Text>
      <ScrollView horizontal contentContainerStyle={styles.thumbs}>
        {shots.map((s, i) => (
          <Image key={s.uri} source={{ uri: s.uri }} style={styles.thumb} accessibilityLabel={`第 ${i + 1} 张`} />
        ))}
      </ScrollView>
      <Text variant="caption">第 1 – {shots.length} 张</Text>
      {n > 0 ? (
        <View style={styles.box}>
          <Text variant="caption" color={semantic.danger}>
            {n} 处不确定，请核对
          </Text>
          <Highlighted text={result.recognized.text} ranges={result.recognized.low_confidence} />
        </View>
      ) : null}
      <Text variant="bodyStrong">识别文字（可修改）</Text>
      <TextInput accessibilityLabel="识别文字" multiline value={text} onChangeText={setText} style={styles.input} textAlignVertical="top" />
      <View style={styles.row}>
        <Button title="再拍一张（续写）" kind="secondary" style={styles.flex} onPress={() => void capture(true)} />
        <Button title="重拍" kind="secondary" style={styles.flex} onPress={() => void capture(false)} />
      </View>
      <Button title="确认并提交批改" disabled={!text.trim()} onPress={() => onConfirm(text, result.keys)} />
      {camera.element}
    </View>
  );
}

const styles = StyleSheet.create({
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', gap: spacing.sm },
  flex: { flex: 1 },
  thumbs: { gap: spacing.sm },
  thumb: { width: 96, height: 128, borderRadius: radius.md, backgroundColor: semantic.border },
  box: { gap: spacing.xs, padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.dangerSoft },
  low: { textDecorationLine: 'underline' },
  input: { minHeight: 160, borderWidth: 1, borderColor: semantic.border, borderRadius: radius.md, padding: spacing.md, fontSize: 16, lineHeight: 24, color: semantic.textPrimary, backgroundColor: semantic.surface },
});
