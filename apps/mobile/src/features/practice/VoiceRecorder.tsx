import { AudioModule, RecordingPresets, requestRecordingPermissionsAsync, setAudioModeAsync, type AudioRecorder } from 'expo-audio';
import { File } from 'expo-file-system';
import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState, Platform, StyleSheet, View } from 'react-native';
import { spacing } from '@training/ui-tokens';
import { Button, Text, toast } from '@/components';
import { usePermissionPrompt } from '@/features/permissions/PermissionPrompt';
import type { VoiceClip } from './voiceUpload';

/** 自主管理原生录音生命周期，停止并保存完成后才释放，离开页面也不会先释放录音器。 */
export function VoiceRecorder({ onCapture, onRecordingChange, disabled = false }: { onCapture: (clip: VoiceClip) => void; onRecordingChange?: (recording: boolean) => void; disabled?: boolean }) {
  const recorder = useRef<AudioRecorder | null>(null);
  const [recording, setRecording] = useState(false);
  const [busy, setBusy] = useState(false);
  const [seconds, setSeconds] = useState(0);
  const started = useRef(0);
  const active = useRef(false);
  const starting = useRef(false);
  const stopping = useRef<Promise<void> | null>(null);
  const mounted = useRef(true);
  const capturing = useRef(onCapture);
  const changed = useRef(onRecordingChange);
  useEffect(() => { capturing.current = onCapture; changed.current = onRecordingChange; }, [onCapture, onRecordingChange]);
  const mic = usePermissionPrompt('microphone', async () => {
    const permission = await requestRecordingPermissionsAsync();
    return { granted: permission.granted, canAskAgain: permission.canAskAgain };
  });
  const stop = useCallback((): Promise<void> => {
    if (stopping.current) return stopping.current;
    if (!active.current || !recorder.current) return Promise.resolve();
    active.current = false;
    const current = recorder.current;
    if (mounted.current) setBusy(true);
    const work = (async () => {
      try {
        await current.stop();
        const uri = current.uri;
        if (!uri) throw new Error('没有录到声音，请重试');
        const file = new File(uri);
        if (!file.size || file.size > 10 * 1024 * 1024) throw new Error('录音为空或超过 10MB，请重新录制');
        // onCapture 同步写入持久队列后再允许切题，网络上传独立运行。
        capturing.current({ uri, mimeType: 'audio/m4a', size: file.size });
        toast('录音已保存在本题，正在后台识别，可以继续作答');
      } catch (error) { toast(error instanceof Error ? error.message : '录音保存失败，请重试'); }
      finally {
        await setAudioModeAsync({ allowsRecording: false }).catch(() => undefined);
        current.release();
        if (recorder.current === current) recorder.current = null;
        if (mounted.current) { setRecording(false); setBusy(false); changed.current?.(false); }
      }
    })();
    stopping.current = work;
    void work.finally(() => { stopping.current = null; });
    return work;
  }, []);
  const start = async () => {
    if (disabled || starting.current || active.current || stopping.current) return;
    starting.current = true;
    setBusy(true); changed.current?.(true);
    try {
      if (!(await mic.ensure()) || !mounted.current) return;
      await setAudioModeAsync({ allowsRecording: true, playsInSilentMode: true });
      if (!mounted.current || ['background', 'inactive'].includes(AppState.currentState ?? '')) return;
      const preset = RecordingPresets.HIGH_QUALITY;
      const options = { ...preset, ...(Platform.OS === 'ios' ? preset.ios : preset.android), sampleRate: 16000, numberOfChannels: 1, bitRate: 64000 };
      const current = new AudioModule.AudioRecorder(options);
      recorder.current = current;
      await current.prepareToRecordAsync();
      if (!mounted.current || ['background', 'inactive'].includes(AppState.currentState ?? '')) { current.release(); recorder.current = null; return; }
      current.record();
      started.current = Date.now(); active.current = true;
      setSeconds(0); setRecording(true);
    } catch { toast('无法开始录音，请检查麦克风权限后重试'); }
    finally {
      starting.current = false;
      if (!active.current) {
        recorder.current?.release(); recorder.current = null;
        await setAudioModeAsync({ allowsRecording: false }).catch(() => undefined);
        if (mounted.current) changed.current?.(false);
      }
      if (mounted.current) setBusy(false);
    }
  };
  useEffect(() => {
    if (!recording) return;
    const timer = setInterval(() => {
      const elapsed = Math.floor((Date.now() - started.current) / 1000);
      setSeconds(elapsed);
      if (elapsed >= 290) void stop();
    }, 500);
    const subscription = AppState.addEventListener('change', (state) => { if (state !== 'active') void stop(); });
    return () => { clearInterval(timer); subscription.remove(); };
  }, [recording, stop]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; void stop(); }; }, [stop]);
  useEffect(() => { if (disabled && active.current) void stop(); }, [disabled, stop]);
  return <View style={styles.gap}>
    <Button title={recording ? `结束录音 ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` : '语音输入'} kind={recording ? 'primary' : 'secondary'} size="sm" disabled={busy || (disabled && !recording)} onPress={() => void (recording ? stop() : start())} />
    {recording ? <Text variant="small">正在录音，结束后可切题。单段接近 5 分钟时自动保存；只转写原话，请核对专业术语。</Text> : null}
    {mic.element}
  </View>;
}
const styles = StyleSheet.create({ gap: { gap: spacing.xs } });
