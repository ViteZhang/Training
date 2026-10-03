// 6.5 会员中心（T25）：权益对比表（PRD 13.1）；三档会员（冲刺卡、考季卡推荐、月卡）与现在购买的有效期截止；
// 「立即开通」在支付开关关闭时隐藏，只显示兑换码入口；会员服务协议；注明不自动续费。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router } from 'expo-router';
import { useMemo, useState } from 'react';
import { Platform, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Tag, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { channelNames, ruleText, useMemberCenter, yuan } from '@/features/membership/api';
import { launchPay } from '@/features/membership/pay';
import { tierNames, ymd } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';

const positioning: Record<Schemas['PlanTier'], string> = { sprint: '冲刺期考生', season: '默认推荐', monthly: '灵活试用' };

export default function MemberCenter() {
  const center = useMemberCenter();
  const c = center.data;
  const plans = useMemo(() => c?.plans ?? [], [c]);
  // iOS 只能用 App 内购买；安卓用微信支付、支付宝（PRD 13.3）。
  const channels = useMemo(
    () => (c?.channels ?? []).filter((ch) => (Platform.OS === 'ios' ? ch === 'apple_iap' : ch !== 'apple_iap')),
    [c],
  );
  const [picked, setTier] = useState<Schemas['PlanTier']>();
  const [pickedChannel, setChannel] = useState<Schemas['PayChannel']>();
  const [error, setError] = useState<string>();
  // 没选时默认推荐档（不可买时取第一个可买的）与第一个渠道。
  const tier = picked ?? (plans.find((p) => p.recommended && p.available) ?? plans.find((p) => p.available))?.tier;
  const channel = pickedChannel ?? channels[0];

  const buy = useMutation({
    mutationFn: async () => {
      const order = await unwrap(api.POST('/orders', { body: { tier: tier!, channel: channel!, idempotency_key: Crypto.randomUUID() } }));
      const r = await launchPay(order);
      return { order, r };
    },
    onSuccess: ({ order, r }) => {
      setError(undefined);
      router.push({ pathname: '/member/result', params: r.ok ? { orderNo: order.order_no } : { orderNo: order.order_no, failed: r.message } });
    },
    onError: (e) => setError(e instanceof ApiError || e instanceof Error ? e.message : '下单失败，请重试'),
  });

  if (center.isPending) return <Screen><Loading /></Screen>;
  if (center.isError || !c) return <Screen><ErrorState error={center.error} onRetry={() => void center.refetch()} /></Screen>;
  const selected = plans.find((p) => p.tier === tier);
  const canPay = c.payment_enabled && channels.length > 0;

  return (
    <Screen>
      <PageHeader title="会员中心" onBack={() => router.back()} />
      <ScrollView contentContainerStyle={styles.body}>
        <View style={styles.hero}>
          <Text variant="h2" color={colors.white}>
            {c.membership.is_member ? `${c.membership.tier ? tierNames[c.membership.tier] : '会员'}生效中` : '开通会员'}
          </Text>
          <Text variant="caption" color={colors.white}>
            {c.membership.is_member && c.membership.ends_at ? `有效期至 ${ymd(c.membership.ends_at)}，续费时长叠加` : '备考期间，批改不限次'}
          </Text>
        </View>

        <Card style={styles.gap}>
          <View style={styles.tableRow}>
            <Text variant="bodyStrong" style={styles.cellName}>权益</Text>
            <Text variant="bodyStrong" style={styles.cell}>免费版</Text>
            <Text variant="bodyStrong" style={styles.cell} color={semantic.primary}>会员</Text>
          </View>
          {c.benefits.map((b) => (
            <View key={b.quota_type} style={styles.tableRow} accessibilityLabel={`${b.name} 免费版 ${ruleText(b.quota_type, b.free)} 会员 ${ruleText(b.quota_type, b.member)}`}>
              <Text variant="body" style={styles.cellName}>{b.name}</Text>
              <Text variant="caption" style={styles.cell}>{ruleText(b.quota_type, b.free)}</Text>
              <Text variant="bodyStrong" style={styles.cell} color={semantic.primary}>{ruleText(b.quota_type, b.member)}</Text>
            </View>
          ))}
          <Text variant="caption">客观题判分、背诵、错题本、复习计划、考情分析、导出题库，免费版也不限</Text>
        </Card>

        {canPay ? (
          <>
            {plans.map((p) => {
              const on = p.tier === tier;
              return (
                <Pressable
                  key={p.tier}
                  accessibilityRole="radio"
                  accessibilityState={{ selected: on, disabled: !p.available }}
                  accessibilityLabel={`${p.name} ${yuan(p.price_cents)}`}
                  disabled={!p.available}
                  onPress={() => setTier(p.tier)}
                  style={[styles.plan, on && styles.planOn, !p.available && styles.planOff]}
                >
                  <View style={styles.flex}>
                    <View style={styles.inline}>
                      <Text variant="h3">{p.name}</Text>
                      {p.recommended ? <Tag label="推荐" tone="brand" /> : null}
                    </View>
                    <Text variant="caption">
                      {p.available ? (p.ends_at ? `有效期至 ${ymd(p.ends_at)} · ${positioning[p.tier]}` : positioning[p.tier]) : p.unavailable_reason ?? '暂未开放'}
                    </Text>
                  </View>
                  <Text variant="h2" color={on ? semantic.primary : semantic.textPrimary}>
                    {yuan(p.price_cents)}
                  </Text>
                </Pressable>
              );
            })}
            {channels.length > 1 ? (
              <View style={styles.inline}>
                {channels.map((ch) => (
                  <Pressable
                    key={ch}
                    accessibilityRole="radio"
                    accessibilityState={{ selected: ch === channel }}
                    onPress={() => setChannel(ch)}
                    style={[styles.channel, ch === channel && styles.planOn]}
                  >
                    <Text variant="body">{channelNames[ch]}</Text>
                  </Pressable>
                ))}
              </View>
            ) : null}
            {error ? (
              <Text variant="caption" color={semantic.danger}>
                {error}
              </Text>
            ) : null}
            <Button
              title={selected ? `立即开通 · ${yuan(selected.price_cents)}` : '立即开通'}
              disabled={!selected?.available || !channel}
              loading={buy.isPending}
              onPress={() => buy.mutate()}
            />
          </>
        ) : null}

        <Button title="有兑换码？" kind={canPay ? 'text' : 'primary'} onPress={() => router.push('/mine/redeem')} />
        <View style={styles.footer}>
          <Pressable accessibilityRole="link" onPress={() => router.push({ pathname: '/(auth)/agreement', params: { kind: 'membership' } })}>
            <Text variant="caption" color={semantic.primary}>会员服务协议</Text>
          </Pressable>
          <Text variant="caption"> · 一次性购买，不自动续费</Text>
        </View>
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1, gap: 2 },
  inline: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' },
  hero: { gap: spacing.xs, padding: spacing.lg, borderRadius: radius.lg, backgroundColor: semantic.primary },
  tableRow: { flexDirection: 'row', alignItems: 'center', minHeight: 36 },
  cellName: { flex: 1.4 },
  cell: { flex: 1, textAlign: 'center' },
  plan: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, padding: spacing.lg, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  planOn: { borderColor: semantic.primary, borderWidth: 2, backgroundColor: semantic.primarySoft },
  planOff: { opacity: 0.5 },
  channel: { minHeight: 44, justifyContent: 'center', paddingHorizontal: spacing.lg, borderRadius: radius.md, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  footer: { flexDirection: 'row', justifyContent: 'center', flexWrap: 'wrap' },
});
