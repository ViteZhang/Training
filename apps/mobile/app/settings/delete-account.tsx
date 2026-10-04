// 6.12 注销账号：列出将删除的内容；勾选确认后申请；7 天冷静期，期间重新登录即撤销。
import { ApiError } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { Button, Card, ConfirmDialog, Icon, Screen, Text, toast } from '@/components';
import { useSession } from '@/lib/session';
import { queryClient } from '@/lib/queryClient';
import { api, unwrap } from '@/lib/api';

const items = [
  '学习记录、掌握度、错题本、作文本将被永久删除',
  '你导入的所有资料、识别出的题目和知识点将被删除，无法找回',
  '未到期的会员权益将一并失效',
];

export default function DeleteAccount() {
  const [checked, setChecked] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);

  const apply = async () => {
    setBusy(true);
    try {
      await unwrap(api.POST('/me/deletion', { body: { confirmed: true } }));
      // 申请后服务端已退出全部设备。
      useSession.getState().clear();
      queryClient.clear();
      toast('已申请注销，7 天内重新登录即可撤销');
      router.replace('/(auth)/login');
    } catch (e) {
      toast(e instanceof ApiError ? e.message : '申请失败，请重试');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen scroll>
      <Button title="返回" kind="text" onPress={() => router.back()} style={styles.back} />
      <Text variant="h2">注销账号</Text>
      <Card style={styles.card}>
        <Text variant="bodyStrong">注销前请确认</Text>
        {items.map((t) => (
          <View key={t} style={styles.item}>
            <Text variant="body" color={semantic.danger}>
              ·
            </Text>
            <Text variant="body" style={styles.flex}>
              {t}
            </Text>
          </View>
        ))}
        <Text variant="caption" style={styles.note}>
          提交申请后有 7 天冷静期，期间重新登录即可撤销注销。
        </Text>
      </Card>
      <Pressable accessibilityRole="checkbox" accessibilityState={{ checked }} onPress={() => setChecked(!checked)} style={styles.check}>
        <View style={[styles.box, checked && styles.boxOn]}>{checked ? <Icon name="check" size={14} color="#FFFFFF" /> : null}</View>
        <Text variant="body">我已了解以上内容，确认注销</Text>
      </Pressable>
      <Button title="申请注销" kind="danger" disabled={!checked} loading={busy} onPress={() => setConfirm(true)} />
      <ConfirmDialog
        visible={confirm}
        title="确认申请注销？"
        message="7 天后将删除你的全部资料和学习记录。"
        confirmText="申请注销"
        danger
        onCancel={() => setConfirm(false)}
        onConfirm={() => {
          setConfirm(false);
          void apply();
        }}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  back: { alignSelf: 'flex-start', marginTop: spacing.sm, paddingHorizontal: 0 },
  card: { marginTop: spacing.lg, gap: spacing.sm },
  item: { flexDirection: 'row', gap: spacing.sm },
  flex: { flex: 1 },
  note: { marginTop: spacing.sm },
  check: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginVertical: spacing.xl, minHeight: 44 },
  box: { width: 20, height: 20, borderRadius: 4, borderWidth: 1.5, borderColor: semantic.textSecondary, alignItems: 'center', justifyContent: 'center' },
  boxOn: { backgroundColor: semantic.danger, borderColor: semantic.danger },
});
