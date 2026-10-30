import { useQuery, useQueryClient } from '@tanstack/react-query';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useEffect, useState } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, Text, toast } from '@/components';
import { getJSON, setJSON } from '@/lib/storage';
import { VoiceRecorder } from './VoiceRecorder';
import { useLocalVoice, type PracticeVoiceQueue, type VoiceJob } from './voiceQueue';

export const voiceEditsKey = (sessionId: number, questionId: number) => `practice:voice-edits:${sessionId}:${questionId}`;
export function PracticeVoiceInput({ queue, onConfirm, applied, onRecordingChange }: { queue: PracticeVoiceQueue; onConfirm: (job: VoiceJob, text: string) => void; applied: string[]; onRecordingChange: (recording: boolean) => void }) {
  const qc = useQueryClient();
  const local = useLocalVoice(queue);
  const key = ['practice-voice', queue.sessionId, queue.questionId];
  const jobs = useQuery({ queryKey: key, queryFn: () => queue.list(), refetchInterval: (query) => query.state.data?.some((job) => job.status === 'pending') || Object.values(queue.snapshot()).some((record) => record.status === 'uploading') ? 2500 : false });
  const editsKey = voiceEditsKey(queue.sessionId, queue.questionId);
  const [edits, setEdits] = useState(() => getJSON<Record<string, string>>(editsKey) ?? {});
  useEffect(() => { queue.resume(); }, [queue]);
  useEffect(() => { void qc.invalidateQueries({ queryKey: ['practice-voice', queue.sessionId, queue.questionId] }); }, [qc, queue, local]);
  const retry = async (job: VoiceJob) => {
    try { await queue.register(job.idempotency_key, job.object_key); await jobs.refetch(); }
    catch (error) { toast(error instanceof Error ? error.message : '暂时无法重试'); }
  };
  return <View style={styles.gap}>
    <VoiceRecorder onCapture={(clip) => { queue.add(clip); }} onRecordingChange={onRecordingChange} />
    {jobs.isError ? <View style={styles.gap}><Text variant="caption">暂时无法读取识别状态，录音会保留。</Text><Button title="刷新语音识别状态" kind="text" onPress={() => void jobs.refetch()} /></View> : null}
    {Object.values(local).filter((record) => record.status !== 'registered').map((record) => <Card key={record.key} style={styles.gap}>
      <Text variant="caption">{record.status === 'failed' ? record.error ?? '录音上传失败，已保存在本机' : '录音正在后台上传，可以切换题目；提交前会等待保存完成。'}</Text>
      {record.status === 'failed' ? <><Button title="重试上传录音" kind="secondary" onPress={() => { void queue.run(record.key).catch((error: Error) => toast(error.message)); }} /><Button title="弃用录音" kind="text" onPress={() => { void queue.discard(record).then(() => jobs.refetch()).catch(() => toast('弃用失败，请重试')); }} /></> : null}
    </Card>)}
    {(jobs.data ?? []).filter((job) => job.status !== 'discarded' && !applied.includes(job.idempotency_key)).map((job, index, all) => {
      const value = edits[job.idempotency_key] ?? job.text;
      const earlier = all.slice(0, index).some((previous) => !applied.includes(previous.idempotency_key)) || Object.values(local).some((record) => record.status !== 'registered' && (record.order ?? 0) < (job.capture_order ?? job.id));
      return <Card key={job.id} style={styles.gap}>
        <Text variant="bodyStrong">{job.status === 'ready' ? '语音识别结果' : job.status === 'failed' ? '语音识别失败' : '语音正在后台识别'}</Text>
        {job.status === 'ready' ? <>
          <Text variant="caption">只转写原话，请核对专业术语；未确认直接提交时默认采用。可继续输入文字。</Text>
          <TextInput accessibilityLabel="语音识别文字" value={value} multiline onChangeText={(text) => { const next = { ...edits, [job.idempotency_key]: text }; setEdits(next); setJSON(editsKey, next); }} style={styles.input} textAlignVertical="top" />
          {earlier ? <Text variant="small">请先处理前面的录音，文字按录音顺序拼接。</Text> : null}
          <Button title="确认语音并填入答案" disabled={earlier || !value.trim()} onPress={() => onConfirm(job, value)} />
        </> : <Text variant="caption">{job.status === 'failed' ? job.error ?? '录音已保存，请重试' : '录音已保存，切题后仍会继续识别，返回本题可核对。'}</Text>}
        {job.status === 'failed' ? <Button title="重试语音识别" kind="secondary" onPress={() => void retry(job)} /> : null}
        <Button title="弃用录音" kind="text" onPress={() => { void queue.discard(job).then(() => jobs.refetch()).catch(() => toast('弃用失败，请重试')); }} />
      </Card>;
    })}
  </View>;
}
const styles = StyleSheet.create({ gap: { gap: spacing.sm }, input: { minHeight: 120, padding: spacing.md, borderWidth: 1, borderColor: semantic.inputBorder, borderRadius: radius.md, color: semantic.textPrimary, backgroundColor: semantic.surface, fontSize: 16, lineHeight: 24 } });
