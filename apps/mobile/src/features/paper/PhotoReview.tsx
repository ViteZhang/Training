import { semantic, spacing, radius } from '@training/ui-tokens';
import { Image, ScrollView, StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, Text } from '@/components';
import { Highlighted } from '@/features/practice/handwriting';
import type { LocalPhoto, PaperHandwriting } from './photos';

export function PaperPhotoReview({ jobs, local, edits, disabled, onEdit, onConfirm, onRetry, onDiscard }: {
  jobs: PaperHandwriting[]; local: LocalPhoto[]; edits: Record<string, string>; disabled: boolean;
  onEdit: (key: string, text: string) => void;
  onConfirm: (job: PaperHandwriting, text: string) => void;
  onRetry: (photo: LocalPhoto | PaperHandwriting) => void;
  onDiscard: (photo: LocalPhoto | PaperHandwriting) => void;
}) {
  return <View style={styles.gap}>
    {local.filter((photo) => photo.status !== 'registered').map((photo) => <Card key={photo.key} style={styles.gap}>
      <Text variant="bodyStrong">{photo.status === 'failed' ? '文件保存失败' : photo.inputKind === 'voice' ? '正在后台上传录音' : '正在后台上传照片'}</Text>
      <PhotoThumbs photo={photo} />
      <Text variant="caption">{photo.status === 'failed' ? `${photo.error ?? '暂时无法上传'}。原文件已保存在本机，请重试。` : '可以继续作答或切换下一题，照片或录音会保留在本题。交卷前会等待上传完成。'}</Text>
      {photo.status === 'failed' ? <View style={styles.row}>
        <Button title="重试上传" kind="secondary" disabled={disabled} style={styles.flex} onPress={() => onRetry(photo)} />
        <Button title="弃用文件" kind="text" disabled={disabled} style={styles.flex} onPress={() => onDiscard(photo)} />
      </View> : null}
    </Card>)}
    {jobs.filter((job) => job.status !== 'confirmed').map((job) => {
      const photo = local.find((p) => p.key === job.idempotency_key);
      const value = edits[job.idempotency_key] ?? job.text;
      const earlier = jobs.slice(0, jobs.indexOf(job)).some((previous) => previous.status !== 'confirmed' && previous.status !== 'discarded') || local.some((previous) => previous.status !== 'registered' && (previous.order ?? 0) < (job.capture_order ?? job.id));
      return <Card key={job.id} style={styles.gap}>
        <Text variant="bodyStrong">{job.status === 'ready' ? '确认识别结果' : job.status === 'failed' ? '识别失败' : job.input_kind === 'voice' ? '语音正在后台识别' : '手写稿正在后台识别'}</Text>
        {photo ? <PhotoThumbs photo={photo} /> : null}
        {job.status === 'ready' ? <>
          <Text variant="caption">已识别 {[...value.replace(/\s/g, '')].length} 字。可以核对修改；未核对直接交卷时默认采用识别结果。</Text>
          {job.low_confidence.length ? <View style={styles.box}><Text variant="caption" color={semantic.danger}>{job.low_confidence.length} 处不确定，请核对</Text><Highlighted text={job.text} ranges={job.low_confidence} /></View> : null}
          <Text variant="bodyStrong">识别文字（可修改）</Text>
          <TextInput accessibilityLabel="识别文字" multiline editable={!disabled} value={value} onChangeText={(text) => onEdit(job.idempotency_key, text)} style={styles.input} textAlignVertical="top" />
          {earlier ? <Text variant="small">请先处理前面的照片或录音，答案将按保存顺序拼接。</Text> : null}
          <Button title="确认并填入答案" disabled={disabled || earlier || !value.trim()} onPress={() => onConfirm(job, value)} />
        </> : <Text variant="caption">{job.status === 'failed' ? (job.error ?? '原文件已保存，请重试识别。') : '原文件已保存，可以切换下一题。返回本题可核对，交卷后未完成的识别会在后台继续。'}</Text>}
        {job.status === 'failed' ? <Button title="重试识别" kind="secondary" disabled={disabled} onPress={() => onRetry(job)} /> : null}
        <Button title="弃用文件" kind="text" disabled={disabled} onPress={() => onDiscard(job)} />
      </Card>;
    })}
  </View>;
}
function PhotoThumbs({ photo }: { photo: LocalPhoto }) {
  if (photo.inputKind === 'voice') return <Text variant="caption">本题录音已保留，识别完成后可核对文字。</Text>;
  return <ScrollView horizontal contentContainerStyle={styles.row}>{photo.shots.map((shot, i) => <Image key={`${shot.uri}-${i}`} source={{ uri: shot.uri }} style={styles.thumb} accessibilityLabel={`第 ${photo.seq} 题照片 ${i + 1}`} />)}</ScrollView>;
}
const styles = StyleSheet.create({
  gap: { gap: spacing.sm },
  row: { flexDirection: 'row', gap: spacing.sm, alignItems: 'center' },
  flex: { flex: 1 },
  thumb: { width: 96, height: 128, borderRadius: radius.md, backgroundColor: semantic.border },
  box: { gap: spacing.xs, padding: spacing.md, borderRadius: radius.md, backgroundColor: semantic.dangerSoft },
  input: { minHeight: 160, borderWidth: 1, borderColor: semantic.inputBorder, borderRadius: radius.md, padding: spacing.md, fontSize: 16, lineHeight: 24, color: semantic.textPrimary, backgroundColor: semantic.surface },
});
