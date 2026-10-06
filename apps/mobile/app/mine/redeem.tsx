// 6.7 兑换码：输入框，不区分大小写；无效、已使用、已作废、已过期分别提示；说明一码一次、时长叠加到当前会员之后、过期不可用。
import { ApiError } from '@training/api-client';
import { radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';
import { Button, Card, Screen, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { mineKeys, tierNames, ymd } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

export default function RedeemPage() {
  const qc = useQueryClient();
  const [code, setCode] = useState('');
  const [error, setError] = useState<string>();
  const redeem = useMutation({
    mutationFn: () => unwrap(api.POST('/me/redeem', { body: { code } })),
    onSuccess: () => {
      track('redeem');
      setError(undefined);
      void qc.invalidateQueries({ queryKey: mineKeys.me });
      void qc.invalidateQueries({ queryKey: ['quota'] });
    },
    onError: (e) => setError(e instanceof ApiError || e instanceof Error ? e.message : '兑换失败，请重试'),
  });
  const r = redeem.data;
  return (
    <Screen>
      <PageHeader title="兑换码" onBack={() => router.back()} />
      {r ? (
        <Card style={styles.gap}>
          <Text variant="h3">兑换成功</Text>
          <Text variant="body">
            {tierNames[r.tier]} · {r.days} 天，已叠加到当前会员之后
          </Text>
          {r.membership.ends_at ? <Text variant="caption">会员有效期至 {ymd(r.membership.ends_at)}</Text> : null}
          <Button title="完成" onPress={() => router.back()} />
        </Card>
      ) : (
        <View style={styles.gap}>
          <Text variant="h1" style={styles.title}>
            输入兑换码
          </Text>
          <Text variant="caption" style={styles.desc}>
            兑换码来自官方活动，每个码只能使用一次
          </Text>
          <TextInput
            accessibilityLabel="兑换码"
            value={code}
            onChangeText={(v) => {
              setCode(v.toUpperCase());
              setError(undefined);
            }}
            autoCapitalize="characters"
            autoCorrect={false}
            placeholder="8 位字母和数字"
            placeholderTextColor={semantic.textSecondary}
            style={[styles.input, error ? styles.inputError : null]}
          />
          {error ? (
            <Text variant="caption" color={semantic.danger}>
              {error}
            </Text>
          ) : null}
          <Button title="兑换" disabled={code.replace(/[\s-]/g, '').length < 8} loading={redeem.isPending} onPress={() => redeem.mutate()} />
        </View>
      )}
      <Card tone="fill" style={styles.gap}>
        <Text variant="small" color={semantic.textPrimary} style={styles.bold}>
          兑换说明
        </Text>
        <Text variant="small" style={styles.lh}>兑换码不区分大小写；兑换后的会员时长会叠加到当前会员之后；兑换码有有效期，过期无法使用。</Text>
      </Card>
    </Screen>
  );
}

const styles = StyleSheet.create({
  gap: { gap: 12, marginBottom: spacing.md },
  title: { marginTop: spacing.sm },
  desc: { fontSize: 14, marginTop: -6, marginBottom: 6 },
  bold: { fontWeight: '700' },
  lh: { lineHeight: 19 },
  input: { minHeight: 56, paddingHorizontal: 16, borderRadius: radius.xl, borderWidth: 1.5, borderColor: semantic.border, backgroundColor: semantic.surface, fontSize: 18, letterSpacing: 3, color: semantic.textPrimary },
  inputError: { borderColor: semantic.danger },
});
