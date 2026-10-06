// 资料解析额度条（3.1c、6.3）：「免费版 · 累计 98 / 100 页」，快用完时给「开通会员」。
import { semantic } from '@training/ui-tokens';
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
    <Card tone="fill" style={styles.card}>
      <View style={styles.row}>
        <Text variant="caption" color={semantic.textPrimary} style={[styles.flex, styles.bold]}>
          资料解析额度
        </Text>
        <Text variant="small">
          {quota.data.membership.is_member ? '会员' : '免费版'} · {periodName[item.period]} {item.used}
          {limit !== undefined ? ` / ${limit}` : ''} 页
        </Text>
      </View>
      {limit !== undefined ? <ProgressBar value={Math.min(item.used / limit, 1)} height={4} /> : null}
      {low ? (
        <View style={styles.row}>
          <Text variant="small" style={styles.flex}>
            还剩 {left} 页。用完后仍可刷题，再解析资料需开通会员
          </Text>
          <Button title="开通会员" kind="text" size="sm" color={semantic.textPrimary} onPress={() => router.push('/member')} />
        </View>
      ) : null}
    </Card>
  );
}

const styles = StyleSheet.create({
  card: { gap: 10, paddingVertical: 16 },
  row: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  flex: { flex: 1 },
  bold: { fontWeight: '700' },
});
