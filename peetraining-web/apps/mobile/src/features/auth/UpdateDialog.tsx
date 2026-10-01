// 0.6 版本更新 / 0.6b 强制更新：启动时检查；强制更新不可关闭；内测期安卓跳安装包下载、iOS 跳 TestFlight。
import type { Schemas } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { Linking, StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Text } from '@/components';
import { appConfig } from '@/lib/config';

export function UpdateDialog({ update, onLater }: { update: Schemas['AppUpdate']; onLater: () => void }) {
  const force = update.force;
  return (
    <BottomSheet visible dismissible={!force} onClose={onLater} title={force ? '请更新到最新版本' : '发现新版本'}>
      {force ? (
        <Text variant="body" color={semantic.textSecondary}>
          当前版本 v{appConfig.version} 已停止服务，更新后才能继续使用。题库和学习记录保存在账号里，更新后不会丢失。
        </Text>
      ) : (
        <Text variant="number">v{update.latest_version}</Text>
      )}
      {update.release_notes ? (
        <Text variant="body" style={styles.notes}>
          {update.release_notes}
        </Text>
      ) : null}
      <View style={styles.actions}>
        {!force ? <Button title="稍后" kind="secondary" onPress={onLater} style={styles.flex} /> : null}
        <Button title="立即更新" onPress={() => void Linking.openURL(update.download_url)} style={styles.flex} />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  notes: { marginTop: spacing.md },
  actions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.xl },
  flex: { flex: 1 },
});
