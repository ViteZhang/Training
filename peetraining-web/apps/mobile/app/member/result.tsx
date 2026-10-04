// 6.6 支付结果（T25）：成功显示档位与有效期、权益说明、「去训练」；失败或取消说明原因与重试。
// 支付平台回调到达后订单才变为已支付，所以先轮询几次；仍未确认时显示「确认中」，可手动刷新。
import { semantic, spacing } from '@training/ui-tokens';
import { useQueryClient } from '@tanstack/react-query';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect } from 'react';
import { StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Text } from '@/components';
import { memberKeys, useOrder } from '@/features/membership/api';
import { mineKeys, tierNames, ymd } from '@/features/mine/api';

export default function PayResult() {
  const { orderNo = '', failed } = useLocalSearchParams<{ orderNo?: string; failed?: string }>();
  const qc = useQueryClient();
  const order = useOrder(failed ? '' : orderNo);
  const paid = order.data?.status === 'paid';
  useEffect(() => {
    if (!paid) return;
    // 会员身份与额度立即刷新。
    void qc.invalidateQueries({ queryKey: mineKeys.me });
    void qc.invalidateQueries({ queryKey: ['quota'] });
    void qc.invalidateQueries({ queryKey: memberKeys.center });
  }, [paid, qc]);

  if (failed) {
    return (
      <Screen>
        <View style={styles.center}>
          <Text variant="h2">支付未完成</Text>
          <Text variant="body" style={styles.text}>{failed}</Text>
          <Text variant="caption" style={styles.text}>如果已经扣款，会员会在支付确认后自动开通，可稍后在「我的」查看</Text>
          <Button title="重新支付" block onPress={() => router.back()} />
          <Button title="返回我的" kind="text" onPress={() => router.replace('/(tabs)/me')} />
        </View>
      </Screen>
    );
  }
  if (order.isPending) return <Screen><Loading /></Screen>;
  if (order.isError || !order.data) return <Screen><ErrorState error={order.error} onRetry={() => void order.refetch()} /></Screen>;
  const o = order.data;
  if (!paid) {
    const confirming = o.status === 'created';
    return (
      <Screen>
        <View style={styles.center}>
          <Text variant="h2">{confirming ? '支付结果确认中' : '订单已关闭'}</Text>
          <Text variant="body" style={styles.text}>
            {confirming ? '支付平台通知可能有几秒延迟；如已完成支付，稍后刷新即可看到结果' : '这笔订单没有完成支付，可以重新开通'}
          </Text>
          {confirming ? <Button title="刷新" block loading={order.isFetching} onPress={() => void order.refetch()} /> : null}
          <Button title="返回会员中心" kind={confirming ? 'text' : 'primary'} onPress={() => router.back()} />
        </View>
      </Screen>
    );
  }
  return (
    <Screen>
      <View style={styles.center}>
        <Text variant="h1">开通成功</Text>
        <Text variant="body" style={styles.text}>
          {tierNames[o.tier]}已生效{o.membership_ends_at ? `，有效期至 ${ymd(o.membership_ends_at)}` : ''}
        </Text>
        <Card style={styles.card}>
          <Text variant="body">批改、出题、资料解析都不限次数</Text>
          <Text variant="caption">会员一次性购买，不自动续费；时长叠加到当前会员之后</Text>
        </Card>
        <Button title="去训练" block onPress={() => router.replace('/(tabs)/train')} />
        <Button title="返回我的" kind="text" onPress={() => router.replace('/(tabs)/me')} />
      </View>
    </Screen>
  );
}

const styles = StyleSheet.create({
  center: { flex: 1, justifyContent: 'center', alignItems: 'stretch', gap: spacing.md, padding: spacing.lg },
  text: { textAlign: 'center', color: semantic.textSecondary },
  card: { gap: spacing.xs },
});
