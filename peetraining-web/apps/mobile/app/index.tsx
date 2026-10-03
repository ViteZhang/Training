// 0.1 启动页：按 VI 竖版组合（夜靛底、琥珀「考研」、白色 Training），约 1.5 秒。
// 分流：有强制更新 → 0.6b；协议有新版本 → 0.4b；未登录 → 0.2；已登录未完成引导 → 中断的步骤；完成引导 → 2.1。
import { colors, fontFamily, spacing } from '@training/ui-tokens';
import type { Schemas } from '@training/api-client';
import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { Platform, StyleSheet, View } from 'react-native';
import { Logo, Text } from '@/components';
import { AgreementUpdateDialog } from '@/features/auth/AgreementUpdateDialog';
import { logout } from '@/features/auth/actions';
import { routeFor } from '@/features/auth/routing';
import { UpdateDialog } from '@/features/auth/UpdateDialog';
import { api, unwrap } from '@/lib/api';
import { appConfig } from '@/lib/config';
import { useSession } from '@/lib/session';
import { storage } from '@/lib/storage';

export const SPLASH_MS = 1500;

type Pending = { kind: 'update'; update: Schemas['AppUpdate'] } | { kind: 'agreements'; list: Schemas['AgreementSummary'][] };

/** 启动后去哪里：未登录 → 0.2；协议有新版本 → 0.4b；否则按引导进度分流。 */
function proceed(b: Schemas['Bootstrap'], setPending: (p: Pending | null) => void) {
  const loggedIn = !!b.logged_in && !!useSession.getState().session;
  if (loggedIn && b.agreements_to_accept?.length) {
    setPending({ kind: 'agreements', list: b.agreements_to_accept });
    return;
  }
  if (!loggedIn && useSession.getState().session) useSession.getState().clear();
  router.replace(routeFor({ loggedIn, onboardingStep: b.onboarding_step }));
}

/** 先看版本更新：强制更新不可跳过；普通更新每个版本只提示一次。 */
function decide(b: Schemas['Bootstrap'], setPending: (p: Pending | null) => void) {
  const update = b.update;
  if (update && (update.force || (update.has_update && storage.getString('update_prompted') !== update.latest_version))) {
    setPending({ kind: 'update', update });
    return;
  }
  proceed(b, setPending);
}

export default function Splash() {
  const [boot, setBoot] = useState<Schemas['Bootstrap'] | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);

  useEffect(() => {
    const started = Date.now();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = async () => {
      let b: Schemas['Bootstrap'] | null = null;
      try {
        b = await unwrap(
          api.GET('/bootstrap', { params: { query: { platform: Platform.OS === 'ios' ? 'ios' : 'android', app_version: appConfig.version } } }),
        );
      } catch {
        // 离线时按本地登录态进入，联网后各页面再拉数据。
      }
      const result = b ?? { server_time: '', app_name: appConfig.appName, flags: {}, logged_in: !!useSession.getState().session };
      timer = setTimeout(() => {
        setBoot(result);
        decide(result, setPending);
      }, Math.max(0, SPLASH_MS - (Date.now() - started)));
    };
    void load();
    return () => clearTimeout(timer);
  }, []);

  return (
    <View style={styles.root}>
      <Logo size={96} />
      <View style={styles.wordmark}>
        <Text style={styles.cn}>考研</Text>
        <Text style={styles.en}>Training</Text>
      </View>
      <Text variant="body" color="#C4C0E0" style={styles.slogan}>
        把你的专业课资料，变成能刷、能批改的题库
      </Text>
      <Text variant="caption" color="#8F8BBF" style={styles.footer}>
        文科专业课 · {appConfig.variant === 'production' ? '' : '内测版'}
      </Text>
      {pending?.kind === 'update' ? (
        <UpdateDialog
          update={pending.update}
          onLater={() => {
            storage.set('update_prompted', pending.update.latest_version);
            setPending(null);
            if (boot) proceed(boot, setPending);
          }}
        />
      ) : null}
      {pending?.kind === 'agreements' ? (
        <AgreementUpdateDialog
          agreements={pending.list}
          onAccepted={() => {
            setPending(null);
            router.replace(routeFor({ loggedIn: true, onboardingStep: boot?.onboarding_step }));
          }}
          onDeclined={() => {
            setPending(null);
            void logout().then(() => router.replace('/(auth)/login'));
          }}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: colors.indigo, alignItems: 'center', justifyContent: 'center', paddingHorizontal: spacing.xxl },
  wordmark: { flexDirection: 'row', alignItems: 'baseline', marginTop: spacing.xl, gap: spacing.xs },
  cn: { fontFamily: fontFamily.serifHeavy, fontSize: 32, lineHeight: 40, color: colors.amber },
  en: { fontFamily: fontFamily.numberBold, fontSize: 30, lineHeight: 40, color: colors.white },
  slogan: { marginTop: spacing.md, textAlign: 'center' },
  footer: { position: 'absolute', bottom: 48 },
});
