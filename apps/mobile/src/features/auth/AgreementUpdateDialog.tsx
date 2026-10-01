// 0.4b 协议更新重新同意：老用户启动时弹出，列出后台配置的变更摘要；不同意则退出登录，题库和记录保留。
import type { Schemas } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { BottomSheet, Button, Text } from '@/components';
import { api, unwrap } from '@/lib/api';

export function AgreementUpdateDialog({
  agreements,
  onAccepted,
  onDeclined,
}: {
  agreements: Schemas['AgreementSummary'][];
  onAccepted: () => void;
  onDeclined: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const accept = async () => {
    setBusy(true);
    try {
      await unwrap(api.POST('/me/agreements', { body: { agreement_ids: agreements.map((a) => a.id) } }));
      onAccepted();
    } finally {
      setBusy(false);
    }
  };
  return (
    <BottomSheet visible dismissible={false} onClose={() => {}} title="用户协议和隐私政策已更新">
      <Text variant="body" color={semantic.textSecondary}>
        请阅读并同意更新后的条款后继续使用。本次主要变化：
      </Text>
      {agreements.map((a) => (
        <Text key={a.id} variant="body" style={styles.item}>
          · {a.change_summary ?? `${a.title}更新到 ${a.version}`}
        </Text>
      ))}
      <View style={styles.links}>
        <Text variant="caption">查看完整</Text>
        <Button title="《用户协议》" kind="text" onPress={() => router.push('/(auth)/agreement?kind=user')} />
        <Text variant="caption">和</Text>
        <Button title="《隐私政策》" kind="text" onPress={() => router.push('/(auth)/agreement?kind=privacy')} />
      </View>
      <View style={styles.actions}>
        <Button title="不同意" kind="secondary" onPress={onDeclined} style={styles.flex} />
        <Button title="同意并继续" loading={busy} onPress={() => void accept()} style={styles.flex} />
      </View>
      <Text variant="caption" style={styles.center}>
        不同意将退出登录，你的题库和记录仍保留
      </Text>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  item: { marginTop: spacing.sm },
  links: { flexDirection: 'row', alignItems: 'center', flexWrap: 'wrap', marginTop: spacing.md },
  actions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.lg },
  flex: { flex: 1 },
  center: { textAlign: 'center', marginTop: spacing.md },
});
