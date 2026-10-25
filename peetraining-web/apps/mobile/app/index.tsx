// 0.1 启动页：按设计稿 m0_splash（白底、VI Logo、「考研Training」、一句说明），约 1.5 秒。
// 分流：有强制更新 → 0.6b；协议有新版本 → 0.4b；未登录 → 0.2；已登录未完成引导 → 中断的步骤；完成引导 → 2.1。
import { fontFamily, semantic } from '@training/ui-tokens';
import type { Schemas } from '@training/api-client';
import { router } from 'expo-router';
import { useEffect, useState } from 'react';
import { Platform, StyleSheet, View } from 'react-native';
import { Logo, Text } from '@/components';
import { AgreementUpdateDialog } from '@/features/auth/AgreementUpdateDialog';
import { logout } from '@/features/auth/actions';
import { routeFor } from '@/features/auth/routing';
import { UpdateDialog } from '@/features/auth/UpdateDialog';
import { api, refreshSession, unwrap } from '@/lib/api';
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

/** 取启动配置；离线时返回 null，按本地登录态进入，联网后各页面再拉数据。 */
async function fetchBootstrap(): Promise<Schemas['Bootstrap'] | null> {
  try {
    return await unwrap(
      api.GET('/bootstrap', { params: { query: { platform: Platform.OS === 'ios' ? 'ios' : 'android', app_version: appConfig.version } } }),
    );
  } catch {
    return null;
  }
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
      let b = await fetchBootstrap();
      if (b && !b.logged_in && useSession.getState().session) {
        // /bootstrap 登录可选，访问令牌过期（如锁屏久了 App 被系统回收）时按未登录返回、不会触发刷新。
        // 先用刷新令牌换新令牌再取一次；刷新因网络失败时保留登录态，按本地登录态进入。
        if (await refreshSession()) b = await fetchBootstrap();
        else if (useSession.getState().session) b = { ...b, logged_in: true };
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
      <View style={styles.center}>
        <Logo size={84} />
        <Text style={styles.wordmark}>
          考研<Text style={styles.en}>Training</Text>
        </Text>
        <Text variant="body" color={semantic.textSecondary} style={styles.slogan}>
          把你的专业课资料，变成能刷、能批改的题库
        </Text>
      </View>
      <Text variant="small" color={semantic.textTertiary} style={styles.footer}>
        {appConfig.variant === 'production' ? '文科专业课' : '文科专业课 · 内测版'}
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
  root: { flex: 1, backgroundColor: semantic.background },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 18, paddingHorizontal: 22 },
  wordmark: { fontSize: 26, lineHeight: 35, fontWeight: '700', letterSpacing: 1, color: semantic.textPrimary },
  en: { fontFamily: fontFamily.numberSemiBold, letterSpacing: 0 },
  slogan: { fontSize: 14, textAlign: 'center' },
  footer: { textAlign: 'center', paddingBottom: 40 },
});
