// 6.10 设置：账号与安全；通知（每日训练提醒及时间、复习到期提醒、资料解析和批改完成）；资料与隐私（我的资料、导出题库、协议与政策）；
// 清除缓存（显示大小，不删草稿）；意见反馈；关于（版本号）；退出登录。
import type { Schemas } from '@training/api-client';
import { semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Directory, Paths } from 'expo-file-system';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Switch, View } from 'react-native';
import { BottomSheet, Button, ConfirmDialog, ErrorState, Icon, ListCard, Loading, Screen, Text, toast } from '@/components';
import { logout } from '@/features/auth/actions';
import { PageHeader } from '@/features/import/ui';
import { mineKeys, useMe, useOverview, useProfile } from '@/features/mine/api';
import { api, unwrap } from '@/lib/api';
import { appConfig } from '@/lib/config';

const reminderOptions = ['07:30', '12:30', '19:00', '20:00', '21:00', '22:00'];

function cacheBytes(): number {
  try {
    return new Directory(Paths.cache).size ?? 0;
  } catch {
    return 0;
  }
}

function fmtSize(b: number) {
  if (b >= 1 << 20) return `${Math.round(b / (1 << 20))} MB`;
  if (b >= 1 << 10) return `${Math.round(b / (1 << 10))} KB`;
  return `${b} B`;
}

function Row({ title, desc, onPress, right }: { title: string; desc?: string; onPress?: () => void; right?: React.ReactNode }) {
  return (
    <Pressable accessibilityRole={onPress ? 'button' : undefined} accessibilityLabel={title} disabled={!onPress} onPress={onPress} style={styles.row}>
      <Text variant="body" style={styles.flex}>
        {title}
      </Text>
      {desc ? <Text variant="small">{desc}</Text> : null}
      {right ?? (onPress ? <Icon name="chevron" size={16} color={semantic.textSecondary} /> : null)}
    </Pressable>
  );
}

export default function SettingsPage() {
  const qc = useQueryClient();
  const me = useMe();
  const profile = useProfile();
  const overview = useOverview();
  const [cache, setCache] = useState(cacheBytes);
  const [timeSheet, setTimeSheet] = useState(false);
  const [confirmLogout, setConfirmLogout] = useState(false);
  const save = useMutation({
    mutationFn: (patch: Partial<Schemas['StudyProfileInput']>) => {
      const p = profile.data!;
      const body: Schemas['StudyProfileInput'] = {
        exam_year: p.exam_year,
        stage: p.pending_stage ?? p.stage,
        daily_minutes: (p.pending_daily_minutes ?? p.daily_minutes) as 30 | 45 | 60 | 90,
        ...patch,
      };
      return unwrap(api.PUT('/profile', { body }));
    },
    onSuccess: (p) => qc.setQueryData(mineKeys.profile, p),
    onError: () => toast('保存失败，请重试'),
  });

  if (me.isLoading || profile.isLoading) return <Screen><Loading rows={8} /></Screen>;
  if (me.isError || profile.isError || !me.data || !profile.data) {
    return <Screen><ErrorState error={me.error ?? profile.error} onRetry={() => void Promise.all([me.refetch(), profile.refetch()])} /></Screen>;
  }
  const p = profile.data;
  const time = p.reminder_times[0] ?? '20:00';
  const clearCache = () => {
    try {
      for (const item of new Directory(Paths.cache).list()) item.delete();
    } catch {
      // 系统正在用的缓存文件删不掉，跳过。
    }
    setCache(cacheBytes());
    toast('缓存已清除');
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.scroll}>
        <PageHeader title="设置" onBack={() => router.back()} />
        <Text variant="small" style={styles.section}>账号</Text>
        <ListCard>
          <Row title="账号与安全" desc={me.data.phone_masked} onPress={() => router.push('/settings/account')} />
        </ListCard>

        <Text variant="small" style={styles.section}>通知</Text>
        <ListCard>
          <Row
            title="每日训练提醒"
            desc={p.notify_daily ? `每天 ${time}` : undefined}
            onPress={p.notify_daily ? () => setTimeSheet(true) : undefined}
            right={<Switch accessibilityLabel="每日训练提醒开关" value={p.notify_daily} onValueChange={(v) => save.mutate({ notify_daily: v })} trackColor={{ true: semantic.primary, false: semantic.border }} thumbColor="#FFFFFF" />}
          />
          <Row
            title="复习到期提醒"
            right={<Switch accessibilityLabel="复习到期提醒开关" value={p.notify_review_due} onValueChange={(v) => save.mutate({ notify_review_due: v })} trackColor={{ true: semantic.primary, false: semantic.border }} thumbColor="#FFFFFF" />}
          />
          <Row
            title="资料解析和批改完成"
            right={<Switch accessibilityLabel="资料解析和批改完成提醒开关" value={p.notify_task_done} onValueChange={(v) => save.mutate({ notify_task_done: v })} trackColor={{ true: semantic.primary, false: semantic.border }} thumbColor="#FFFFFF" />}
          />
        </ListCard>

        <Text variant="small" style={styles.section}>资料与隐私</Text>
        <ListCard>
          <Row title="我的资料" desc={overview.data ? `${overview.data.materials} 份` : undefined} onPress={() => router.push('/library')} />
          <Row title="导出题库" onPress={() => router.push('/mine/export')} />
          <Row title="用户协议与隐私政策" onPress={() => router.push('/(auth)/agreement')} />
        </ListCard>

        <Text variant="small" style={styles.section}>通用</Text>
        <ListCard>
          <Row title="清除缓存" desc={fmtSize(cache)} onPress={clearCache} />
          <Row title="意见反馈" onPress={() => router.push('/mine/feedback')} />
          <Row title={`关于${appConfig.appName}`} desc={`v${appConfig.version}${appConfig.variant === 'production' ? '' : ' 内测版'}`} />
        </ListCard>
        <Text variant="small" color={semantic.textSecondary}>
          清除缓存只删除临时文件（导出文档、图片缓存），不会删除没写完的草稿。
        </Text>
        <Button title="退出登录" kind="secondary" color={semantic.danger} onPress={() => setConfirmLogout(true)} />
      </ScrollView>
      <BottomSheet visible={timeSheet} onClose={() => setTimeSheet(false)} title="每日训练提醒时间">
        <View style={styles.times}>
          {reminderOptions.map((t) => (
            <Button
              key={t}
              title={t}
              kind={t === time ? 'primary' : 'secondary'}
              onPress={() => {
                save.mutate({ reminder_times: [t] });
                setTimeSheet(false);
              }}
            />
          ))}
        </View>
      </BottomSheet>
      <ConfirmDialog
        visible={confirmLogout}
        title="退出登录？"
        message="退出后草稿仍保存在本机，重新登录可以继续。"
        confirmText="退出"
        onCancel={() => setConfirmLogout(false)}
        onConfirm={() => {
          setConfirmLogout(false);
          void logout().then(() => router.replace('/(auth)/login'));
        }}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  scroll: { gap: spacing.sm, paddingBottom: spacing.xl },
  flex: { flex: 1 },
  section: { marginTop: spacing.sm, marginLeft: 4 },
  list: { paddingVertical: spacing.xs },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, minHeight: 56 },
  times: { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm, paddingBottom: spacing.lg },
});
