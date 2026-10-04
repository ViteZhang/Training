// 资料解析额度条（3.1c、6.3）：「免费版 · 累计 98 / 100 页」，快用完时给「开通会员」。
import { semantic, spacing } from '@training/ui-tokens';
import { router } from 'expo-router';
import { StyleSheet, View } from 'react-native';
import { Button, Card, ProgressBar, Text } from '@/components';
import { useQuota } from '@/features/import/api';

const periodName = { total: '累计', monthly: '本月', daily: '今天', weekly: '本周' } as const;

export function ParseQuotaBar() {
  const quota = useQuota();
  const item = quota.data?.items.find((i) => i.quota_type === 'parse_pages');
  if (!quota.data || !item) return null;
  const limit = item.limit ?? undefined;
  const left = limit !== undefined ? Math.max(limit - item.used, 0) : undefined;
  const low = limit !== undefined && left !== undefined && left <= limit * 0.1;
  return (
    <Card style={styles.card}>
      <View style={styles.row}>
        <Text variant="bodyStrong" style={styles.flex}>
          资料解析额度
        </Text>
        <Text variant="caption">
          {quota.data.membership.is_member ? '会员' : '免费版'} · {periodName[item.period]} <Text variant="number">{item.used}</Text>
          {limit !== undefined ? ` / ${limit}` : ''} 页
        </Text>
      </View>
      {limit !== undefined ? <ProgressBar value={Math.min(item.used / limit, 1)} /> : null}
      {low ? (
        <>
          <Text variant="caption" color={semantic.danger}>
            还剩 {left} 页。用完后仍可刷题，再解析资料需开通会员
          </Text>
          <Button title="开通会员" kind="secondary" onPress={() => router.push('/member')} />
        </>
      ) : null}
    </Card>
  );
}

const styles = StyleSheet.create({
  card: { gap: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'baseline' },
  flex: { flex: 1 },
});
