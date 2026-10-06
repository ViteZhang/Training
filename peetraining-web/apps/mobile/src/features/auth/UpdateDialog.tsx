// 0.6 版本更新 / 0.6b 强制更新：启动时检查；强制更新不可关闭；内测期安卓跳安装包下载、iOS 跳 TestFlight。
import type { Schemas } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { Linking, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Text } from '@/components';
import { appConfig } from '@/lib/config';

export function UpdateDialog({ update, onLater }: { update: Schemas['AppUpdate']; onLater: () => void }) {
  const force = update.force;
  return (
    <BottomSheet visible centered dismissible={!force} onClose={onLater} title={force ? '请更新到最新版本' : `发现新版本 v${update.latest_version}`}>
      {force ? (
        <Text variant="caption" style={styles.lh}>
          当前版本 v{appConfig.version} 已停止服务，更新后才能继续使用。题库和学习记录保存在账号里，更新后不会丢失。
        </Text>
      ) : null}
      {update.release_notes ? (
        <Text variant="caption" color={semantic.textPrimary} style={[styles.notes, styles.lh]}>
          {update.release_notes}
        </Text>
      ) : null}
      <View style={styles.actions}>
        {!force ? <Button title="稍后" kind="secondary" onPress={onLater} style={styles.flex} /> : null}
        <Button title="立即更新" onPress={() => void Linking.openURL(update.download_url)} style={styles.flex2} />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  notes: { marginTop: spacing.xs },
  lh: { lineHeight: 22 },
  actions: { flexDirection: 'row', gap: 10, marginTop: 18 },
  flex: { flex: 1, minHeight: 46 },
  flex2: { flex: 1.4, minHeight: 46 },
});
