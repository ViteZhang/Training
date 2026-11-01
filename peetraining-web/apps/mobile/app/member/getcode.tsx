// 6.5a 获取兑换码（企业微信）：在线支付开通前，加客服企业微信购买兑换码（PRD v3 6.5a、13.3）。
// 底部弹层：客服二维码（可保存到相册）、名称、服务时间；企业微信 ID 选填，配了才显示并可复制；三步说明（列出档位与价格）；
// 「我已有兑换码，去兑换」→ 6.7。二维码、ID、服务时间、说明文案在后台 7.8 配置。购买入口关闭的平台不显示本页入口。
import { semantic, spacing } from '@training/ui-tokens';
import * as Clipboard from 'expo-clipboard';
import { File, Paths } from 'expo-file-system';
import * as MediaLibrary from 'expo-media-library';
import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { Image, ScrollView, StyleSheet, useWindowDimensions, View } from 'react-native';
import { BottomSheet, Button, Icon, Loading, Text, toast } from '@/components';
import { useMemberCenter, yuan } from '@/features/membership/api';
import { usePurchaseEntry } from '@/features/membership/purchase';
import { usePermissionPrompt } from '@/features/permissions/PermissionPrompt';
import { track } from '@/lib/analytics';

async function requestSave() {
  const r = await MediaLibrary.requestPermissionsAsync(true, ['photo']);
  return { granted: r.granted, canAskAgain: r.canAskAgain };
}

export default function GetCodeSheet() {
  const entry = usePurchaseEntry();
  const center = useMemberCenter();
  const [saving, setSaving] = useState(false);
  const { height } = useWindowDimensions();
  const permission = usePermissionPrompt('savePhotos', requestSave);
  useEffect(() => track('getcode_view'), []);
  const close = () => router.back();
  const c = entry.data?.contact;
  // 企业微信 ID 选填：后台没配就不显示 ID 和「复制 ID」，只用二维码。
  const id = c?.wecom_id.trim() ?? '';
  // 入口被后台关掉了（或这个平台本来就关）：直接退回，不出现会员与价格。
  useEffect(() => {
    if (entry.data && !entry.data.enabled) router.back();
  }, [entry.data]);

  const prices = (center.data?.plans ?? []).filter((p) => p.available).map((p) => `${p.name} ${yuan(p.price_cents)}`).join(' · ');

  const copyID = async () => {
    if (!id) return;
    await Clipboard.setStringAsync(id);
    track('getcode_copy_id');
    toast('已复制 ID，去微信里搜索添加');
  };
  const saveQR = async () => {
    if (!c?.qr_url || saving) return;
    if (!(await permission.ensure())) return;
    setSaving(true);
    try {
      const target = new File(Paths.cache, 'kefu-qrcode.png');
      if (target.exists) target.delete();
      const file = await File.downloadFileAsync(c.qr_url, target);
      await MediaLibrary.Asset.create(file.uri);
      track('getcode_save_qr');
      toast('二维码已保存到相册');
    } catch {
      toast(id ? '保存失败，可以截图或复制 ID 添加' : '保存失败，可以截图后在微信里识别');
    } finally {
      setSaving(false);
    }
  };

  return (
    <View style={styles.root}>
      <BottomSheet visible onClose={close}>
        {!c ? (
          <Loading />
        ) : (
          // 系统字体放大时内容会超过一屏：限制高度、可滚动
          <ScrollView style={{ maxHeight: height * 0.82 }} contentContainerStyle={styles.body} accessibilityLabel="获取兑换码" bounces={false}>
            <View style={styles.head}>
              <Text variant="h2">加客服微信，领取兑换码</Text>
              <Text variant="small">{c.note || '暂未开放在线支付，会员通过客服开通'}</Text>
            </View>
            <View style={styles.card}>
              <View style={styles.qr}>
                {c.qr_url ? (
                  <Image source={{ uri: c.qr_url }} style={styles.qrImage} resizeMode="contain" accessibilityLabel="客服企业微信二维码" />
                ) : (
                  <Icon name="image" size={28} color={semantic.textTertiary} />
                )}
              </View>
              <Text variant="bodyStrong" style={styles.bold}>
                {c.name}
              </Text>
              {c.reply_time ? (
                <View style={styles.time} accessibilityLabel={`服务时间 ${c.reply_time}`}>
                  <Icon name="clock" size={14} color={semantic.textSecondary} />
                  <Text variant="small">服务时间</Text>
                  <Text variant="small" num color={semantic.textPrimary}>
                    {c.reply_time}
                  </Text>
                </View>
              ) : null}
              {id ? (
                <Text variant="small" num>
                  企业微信 ID：{id}
                </Text>
              ) : null}
              <View style={styles.row}>
                {c.qr_url ? <Button title="保存二维码" kind="secondary" size="sm" loading={saving} onPress={() => void saveQR()} /> : null}
                {id ? <Button title="复制 ID" kind="secondary" size="sm" onPress={() => void copyID()} /> : null}
              </View>
            </View>
            <View style={styles.steps}>
              <Step n={1} title="微信扫码，添加客服" desc={id ? '也可以保存二维码到相册识别，或搜索上方 ID' : '也可以保存二维码，在微信里「扫一扫」从相册识别'} />
              <Step n={2} title="告诉客服想开哪种会员" desc={prices || undefined} />
              <Step n={3} title="付款后收到兑换码，回来兑换" desc="时长叠加在当前会员之后，不自动续费" />
            </View>
            <Button title="我已有兑换码，去兑换" block onPress={() => router.replace('/mine/redeem')} />
            <Button title="关闭" kind="text" onPress={close} />
          </ScrollView>
        )}
      </BottomSheet>
      {permission.element}
    </View>
  );
}

function Step({ n, title, desc }: { n: number; title: string; desc?: string }) {
  return (
    <View style={styles.step}>
      <View style={styles.stepNo}>
        <Text variant="small" num={600} color={semantic.surface}>
          {n}
        </Text>
      </View>
      <View style={styles.stepText}>
        <Text variant="caption" color={semantic.textPrimary} style={styles.medium}>
          {title}
        </Text>
        {desc ? <Text variant="small">{desc}</Text> : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: 'transparent' },
  body: { gap: 14 },
  head: { gap: 4 },
  bold: { fontWeight: '700' },
  medium: { fontWeight: '500' },
  card: { alignItems: 'center', gap: 8, paddingVertical: spacing.lg, paddingHorizontal: spacing.lg, borderRadius: 18, borderWidth: 1, borderColor: semantic.border },
  qr: { width: 168, height: 168, borderRadius: 14, borderWidth: 1, borderColor: semantic.inputBorder, alignItems: 'center', justifyContent: 'center', overflow: 'hidden', marginBottom: 4 },
  qrImage: { width: 160, height: 160 },
  time: { flexDirection: 'row', alignItems: 'center', gap: 4, paddingVertical: 4, paddingHorizontal: 10, borderRadius: 999, backgroundColor: semantic.fill },
  row: { flexDirection: 'row', gap: 8, flexWrap: 'wrap', justifyContent: 'center', marginTop: 4 },
  steps: { gap: 12, paddingVertical: 14, paddingHorizontal: 16, borderRadius: 18, backgroundColor: semantic.fill },
  step: { flexDirection: 'row', gap: 12, alignItems: 'flex-start' },
  stepNo: { width: 22, height: 22, borderRadius: 11, backgroundColor: semantic.textPrimary, alignItems: 'center', justifyContent: 'center' },
  stepText: { flex: 1, gap: 2 },
});
