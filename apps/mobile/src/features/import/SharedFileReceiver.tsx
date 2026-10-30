import { semantic, spacing } from '@training/ui-tokens';
import { router, usePathname } from 'expo-router';
import * as Sharing from 'expo-sharing';
import { useEffect } from 'react';
import { AppState, Linking, Platform, Pressable, StyleSheet } from 'react-native';
import { Text, toast } from '@/components';
import { clearSharedReceipts, receiveSharedFiles, useSharedInbox } from './sharedInbox';

let receiving: Promise<void> | undefined;
/** 放在根布局：冷启动与回到前台都接收，独立于登录态与当前考试。 */
export function SharedFileReceiver() {
  const files = useSharedInbox();
  const path = usePathname();
  useEffect(() => {
    if (Platform.OS === 'web') return;
    const receive = () => {
      if (receiving) return receiving;
      receiving = (async () => {
        try {
          const payloads = await Sharing.getResolvedSharedPayloadsAsync();
          if (!payloads.length) { clearSharedReceipts(); return; }
          const errors = await receiveSharedFiles(payloads, true);
          // 失败仍保留原生载荷，允许返回前台后重试；已复制的 URI 去重。
          if (!errors.length) {
            const latest = await Sharing.getResolvedSharedPayloadsAsync();
            // 处理期间另一批分享到达时保留载荷，下次接收时按 URI 去重。
            if (JSON.stringify(latest.map((item) => item.value)) === JSON.stringify(payloads.map((item) => item.value))) { Sharing.clearSharedPayloads(); clearSharedReceipts(); }
          }
          else toast(errors[0]!);
        } catch { /* 旧安装包没有接收模块时，不影响原有启动和作答。 */ }
      })().finally(() => { receiving = undefined; });
      return receiving;
    };
    const openURL = async (url: string) => {
      if (/^(file|content):\/\//.test(url)) {
        const errors = await receiveSharedFiles([{ value: url, shareType: 'file', contentType: 'file', contentUri: url, contentMimeType: null, originalName: null, contentSize: null }]);
        if (errors.length) toast(errors[0]!);
      } else await receive();
    };
    void receive();
    void Linking.getInitialURL().then((url) => { if (url) void openURL(url); });
    const app = AppState.addEventListener('change', (state) => { if (state === 'active') void receive(); });
    const link = Linking.addEventListener('url', ({ url }) => { void openURL(url); });
    return () => { app.remove(); link.remove(); };
  }, []);
  if (!files.length || path.startsWith('/import') || path === '/') return null;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={`有 ${files.length} 个待导入文件，点击处理`} onPress={() => router.push('/import/shared')} style={styles.notice}>
      <Text variant="caption" color={semantic.textOnBrand}>已收到 {files.length} 个文件 · 点击继续导入</Text>
    </Pressable>
  );
}
const styles = StyleSheet.create({ notice: { position: 'absolute', left: spacing.lg, right: spacing.lg, bottom: 96, minHeight: 44, padding: spacing.md, borderRadius: 16, backgroundColor: semantic.primary, alignItems: 'center', justifyContent: 'center' } });
