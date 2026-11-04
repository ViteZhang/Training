// 6.5 会员中心（T25）：权益对比表（PRD 13.1）；三档会员（冲刺卡、考季卡、月卡，推荐档由服务端判断）与现在购买的有效期截止；
// 在线支付开通前主按钮为「获取兑换码」（→ 6.5a），上方说明「暂未开放在线支付，加客服企业微信获取兑换码」；
// 开通后为「立即开通 ¥价格」；「我已有兑换码」→ 6.7；会员服务协议；注明不自动续费。
// 购买入口关闭的平台（iOS 提审前）不显示本页入口，误进来时直接退回（PRD v3 3.4）。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { colors, fontFamily, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation } from '@tanstack/react-query';
import * as Crypto from 'expo-crypto';
import { router } from 'expo-router';
import { useEffect, useMemo, useState } from 'react';
import { Platform, Pressable, ScrollView, StyleSheet, View } from 'react-native';
import { Button, Card, ErrorState, Loading, Screen, Tag, Text } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { channelNames, ruleText, useMemberCenter, yuan } from '@/features/membership/api';
import { launchPay } from '@/features/membership/pay';
import { usePurchaseEntry } from '@/features/membership/purchase';
import { tierNames, ymd } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';
import { track } from '@/lib/analytics';

// 推荐档由服务端按距初试天数判断（PRD v3 13.2）：临近初试推荐月卡、强化期推荐冲刺卡、更早推荐考季卡。
const positioning: Record<Schemas['PlanTier'], string> = { sprint: '用到本次初试结束', season: '长线备考，含下一年初试', monthly: '30 天，按需续' };

export default function MemberCenter() {
  const center = useMemberCenter();
  const entry = usePurchaseEntry();
  useEffect(() => track('member_page_view'), []);
  useEffect(() => {
    if (entry.data && !entry.data.enabled) router.back();
  }, [entry.data]);
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
      <PageHeader title={c.membership.is_member ? '会员中心' : '开通会员'} onBack={() => router.back()} />
      <ScrollView contentContainerStyle={styles.body}>
        {c.membership.is_member ? (
          <View style={styles.hero}>
            <Text variant="h2" color={colors.white}>
              {`${c.membership.tier ? tierNames[c.membership.tier] : '会员'}生效中`}
            </Text>
            <Text variant="small" color={colors.faint}>
              {c.membership.ends_at ? `有效期至 ${ymd(c.membership.ends_at)}，续费时长叠加` : ''}
            </Text>
          </View>
        ) : (
          <Text variant="h1" style={styles.title}>
            备考期间，批改不限次
          </Text>
        )}

        <Card style={styles.table}>
          <View style={[styles.tableRow, styles.tableHead]}>
            <Text variant="small" style={styles.cellName}>权益</Text>
            <Text variant="small" style={styles.cell}>免费版</Text>
            <Text variant="small" color={semantic.textPrimary} style={[styles.cell, styles.bold]}>会员</Text>
          </View>
          {c.benefits.map((b) => (
            <View key={b.quota_type} style={[styles.tableRow, styles.divider]} accessibilityLabel={`${b.name} 免费版 ${ruleText(b.quota_type, b.free)} 会员 ${ruleText(b.quota_type, b.member)}`}>
              <Text variant="caption" color={semantic.textPrimary} style={styles.cellName}>{b.name}</Text>
              <Text variant="small" num style={styles.cell}>{ruleText(b.quota_type, b.free)}</Text>
              <Text variant="caption" num={700} color={semantic.textPrimary} style={styles.cell}>{ruleText(b.quota_type, b.member)}</Text>
            </View>
          ))}
        </Card>
        <Text variant="small">客观题判分、背诵、错题本、复习计划、考情分析、导出题库，免费版也不限</Text>

        {plans.map((p) => {
          const on = p.tier === tier;
          return (
            <Pressable
              key={p.tier}
              accessibilityRole="radio"
              accessibilityState={{ selected: on, disabled: !p.available }}
              accessibilityLabel={`${p.name} ${yuan(p.price_cents)}`}
              disabled={!p.available || !canPay}
              onPress={() => setTier(p.tier)}
              style={[styles.plan, on && styles.planOn, !p.available && styles.planOff]}
            >
              <View style={styles.flex}>
                <View style={styles.inline}>
                  <Text variant="bodyStrong" style={styles.bold}>
                    {p.name}
                  </Text>
                  {p.recommended ? <Tag label="推荐" tone="outline" /> : null}
                </View>
                <Text variant="small">
                  {p.available ? (p.ends_at ? `有效期至 ${ymd(p.ends_at)} · ${positioning[p.tier]}` : positioning[p.tier]) : p.unavailable_reason ?? '暂未开放'}
                </Text>
              </View>
              <Text style={styles.price}>{yuan(p.price_cents)}</Text>
            </Pressable>
          );
        })}
        {canPay ? (
          <>
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
        ) : (
          <>
            <Text variant="small" style={styles.center}>
              {entry.data?.contact?.note || '暂未开放在线支付，加客服企业微信获取兑换码'}
            </Text>
            <Button title="获取兑换码" onPress={() => router.push('/member/getcode')} />
          </>
        )}

        <Button title="我已有兑换码" kind="text" onPress={() => router.push('/mine/redeem')} />
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
  body: { gap: 12, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1, gap: 2 },
  bold: { fontWeight: '700' },
  title: { marginTop: spacing.sm, marginBottom: 4 },
  inline: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' },
  hero: { gap: 4, padding: 18, borderRadius: radius.card, backgroundColor: semantic.primary },
  table: { paddingVertical: 0, paddingHorizontal: 0, overflow: 'hidden' },
  tableHead: { backgroundColor: semantic.fill, minHeight: 38 },
  tableRow: { flexDirection: 'row', alignItems: 'center', minHeight: 42, paddingHorizontal: 16 },
  divider: { borderTopWidth: 1, borderTopColor: semantic.border },
  cellName: { flex: 1.6 },
  cell: { flex: 1, textAlign: 'center' },
  plan: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, padding: 16, borderRadius: radius.xl, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  planOn: { borderColor: semantic.textPrimary, borderWidth: 2, padding: 15 },
  planOff: { opacity: 0.5 },
  price: { fontFamily: fontFamily.numberSemiBold, fontSize: 24, color: semantic.textPrimary },
  channel: { minHeight: 44, justifyContent: 'center', paddingHorizontal: spacing.lg, borderRadius: radius.lg, borderWidth: 1, borderColor: semantic.border, backgroundColor: semantic.surface },
  footer: { flexDirection: 'row', justifyContent: 'center', flexWrap: 'wrap' },
  center: { textAlign: 'center' },
});
