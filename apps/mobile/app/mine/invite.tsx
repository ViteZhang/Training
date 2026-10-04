// 6.8 邀请研友（T26）：我的邀请码、复制、生成海报、分享给微信好友；规则「好友用你的邀请码注册，并导入第一份资料后，
// 双方各得 7 天会员」；邀请记录与已获得天数（PRD 13.4）。受 invite 开关控制，关闭时入口不显示、接口 404。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useQuery } from '@tanstack/react-query';
import * as Clipboard from 'expo-clipboard';
import * as Sharing from 'expo-sharing';
import { router } from 'expo-router';
import { useRef, useState } from 'react';
import { ScrollView, Share, StyleSheet, View } from 'react-native';
import { captureRef } from 'react-native-view-shot';
import { BottomSheet, Button, Card, ErrorState, Loading, Screen, Text, toast } from '@/components';
import { PageHeader } from '@/features/import/ui';
import { ymd } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';
import { appConfig } from '@/lib/config';

export function inviteMessage(code: string, days: number) {
  return `我在用「${appConfig.appName}」备考专业课：导入自己的资料，AI 按采分点批改主观题。注册时填我的邀请码 ${code}，导入第一份资料后我们各得 ${days} 天会员。`;
}

const friendName = (i: number, total: number) => `研友 ${String.fromCharCode(65 + ((total - 1 - i) % 26))}`;

export default function InvitePage() {
  const q = useQuery({ queryKey: ['me', 'invites'], queryFn: () => unwrap(api.GET('/me/invites')) });
  const [poster, setPoster] = useState(false);
  const shot = useRef<View>(null);
  if (q.isPending) return <Screen><Loading /></Screen>;
  if (q.isError || !q.data) return <Screen><ErrorState error={q.error} onRetry={() => void q.refetch()} /></Screen>;
  const d = q.data;
  const capped = d.earned_days >= d.max_days;

  const copy = async () => {
    await Clipboard.setStringAsync(d.code);
    toast('邀请码已复制');
  };
  const share = async () => {
    // 系统分享面板里选微信即可发给好友；不接微信 SDK（D35）。
    await Share.share({ message: inviteMessage(d.code, d.reward_days) });
  };
  const sharePoster = async () => {
    try {
      const uri = await captureRef(shot, { format: 'png', quality: 1 });
      if (await Sharing.isAvailableAsync()) await Sharing.shareAsync(uri, { mimeType: 'image/png', dialogTitle: '分享邀请海报' });
    } catch {
      toast('海报生成失败，请重试');
    }
  };

  return (
    <Screen>
      <PageHeader title="邀请研友" onBack={() => router.back()} />
      <ScrollView contentContainerStyle={styles.body}>
        <View style={styles.hero}>
          <Text variant="h2" color={colors.white}>邀请研友一起备考</Text>
          <Text variant="h3" color={colors.amber}>双方各得 {d.reward_days} 天会员</Text>
          <Text variant="caption" color={colors.white}>好友用你的邀请码注册，并导入第一份资料后生效</Text>
        </View>
        <Card style={styles.codeCard}>
          <Text variant="caption">我的邀请码</Text>
          <Text variant="number" accessibilityLabel={`邀请码 ${d.code}`} style={styles.code}>{d.code}</Text>
          <Button title="复制" kind="secondary" onPress={() => void copy()} />
        </Card>
        <View style={styles.actions}>
          <Button title="生成海报" kind="secondary" style={styles.flex} onPress={() => setPoster(true)} />
          <Button title="分享给微信好友" style={styles.flex} onPress={() => void share()} />
        </View>
        <Text variant="caption">
          每邀请一位好友得 {d.reward_days} 天，累计最多 {d.max_days} 天{capped ? '（已达上限，好友仍可获得奖励）' : ''}
        </Text>

        <Card style={styles.gap}>
          <View style={styles.row}>
            <Text variant="h3" style={styles.flex}>邀请记录</Text>
            <Text variant="bodyStrong" color={semantic.progress}>已获得 {d.earned_days} 天</Text>
          </View>
          {d.records.length === 0 ? <Text variant="caption">还没有好友用你的邀请码注册</Text> : null}
          {d.records.map((r, i) => (
            <View key={`${r.registered_at}-${i}`} style={styles.row}>
              <View style={styles.flex}>
                <Text variant="body">{friendName(i, d.records.length)}</Text>
                <Text variant="caption">
                  {r.activated ? '已导入资料' : '已注册，还没导入资料'} · {ymd(r.registered_at)}
                </Text>
              </View>
              {r.activated ? <Text variant="bodyStrong" color={semantic.progress}>{r.days > 0 ? `+${r.days} 天` : '已达上限'}</Text> : null}
            </View>
          ))}
        </Card>
      </ScrollView>

      <BottomSheet visible={poster} onClose={() => setPoster(false)} title="邀请海报">
        <View ref={shot} collapsable={false} style={styles.poster}>
          <Text variant="h2" color={colors.white}>{appConfig.appName}</Text>
          <Text variant="body" color={colors.white}>导入你的专业课资料，AI 按采分点批改主观题</Text>
          <Text variant="caption" color={colors.white}>注册时填写邀请码</Text>
          <Text variant="number" color={colors.amber} style={styles.code}>{d.code}</Text>
          <Text variant="caption" color={colors.white}>导入第一份资料后，我们各得 {d.reward_days} 天会员</Text>
        </View>
        <Button title="分享海报" block onPress={() => void sharePoster()} />
      </BottomSheet>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: spacing.md, paddingBottom: spacing.xl },
  gap: { gap: spacing.sm },
  flex: { flex: 1 },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 44 },
  hero: { gap: spacing.xs, padding: spacing.lg, borderRadius: radius.lg, backgroundColor: semantic.primary },
  codeCard: { alignItems: 'center', gap: spacing.sm },
  code: { letterSpacing: 4 },
  actions: { flexDirection: 'row', gap: spacing.sm },
  poster: { gap: spacing.sm, alignItems: 'center', padding: spacing.xl, borderRadius: radius.lg, backgroundColor: semantic.primary, marginBottom: spacing.md },
});
