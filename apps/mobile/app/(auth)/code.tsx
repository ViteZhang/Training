// 0.3 输入验证码 / 0.3b 验证码错误 / 0.3c 收不到验证码。
// 6 位输满自动登录；支持 iOS 与安卓短信自动填充；60 秒倒计时后可重发；输错清空并提示剩余次数。
import { colors, fontFamily, layout, radius, semantic, spacing } from '@training/ui-tokens';
import { ApiError } from '@training/api-client';
import * as Device from 'expo-device';
import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useRef, useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Screen, Text } from '@/components';
import { saveTokens } from '@/features/auth/actions';
import { formatPhone } from '@/features/auth/phone';
import { routeFor } from '@/features/auth/routing';
import { api, unwrap } from '@/lib/api';
import { deviceId, platform } from '@/lib/device';

const LEN = 6;

export default function Code() {
  const params = useLocalSearchParams<{ phone: string; cooldown?: string }>();
  const phone = params.phone ?? '';
  const [code, setCode] = useState('');
  const [left, setLeft] = useState(Number(params.cooldown ?? 60));
  const [error, setError] = useState<string | null>(null);
  const [expired, setExpired] = useState(false);
  const [busy, setBusy] = useState(false);
  const [help, setHelp] = useState(false);
  const input = useRef<TextInput>(null);

  useEffect(() => {
    if (left <= 0) return;
    const t = setTimeout(() => setLeft(left - 1), 1000);
    return () => clearTimeout(t);
  }, [left]);

  const submit = async (value: string) => {
    setBusy(true);
    setError(null);
    try {
      const res = await unwrap(
        api.POST('/auth/login', {
          body: {
            phone,
            code: value,
            device: { device_id: deviceId(), platform, device_name: Device.modelName ?? Device.deviceName ?? undefined },
          },
        }),
      );
      saveTokens(res);
      router.replace(routeFor({ loggedIn: true, onboardingStep: res.is_new_user ? '1.1' : res.user.onboarding_step }));
    } catch (e) {
      setCode('');
      if (e instanceof ApiError && e.detail?.code_expired) {
        setExpired(true);
        setError('验证码已失效，请重新获取');
      } else if (e instanceof ApiError) {
        setError(e.message);
      } else {
        setError('网络开小差了，请稍后再试');
      }
      input.current?.focus();
    } finally {
      setBusy(false);
    }
  };

  const onChange = (t: string) => {
    const digits = t.replace(/\D/g, '').slice(0, LEN);
    setCode(digits);
    if (error) setError(null);
    if (digits.length === LEN && !busy) void submit(digits);
  };

  const resend = async () => {
    try {
      await unwrap(api.POST('/auth/sms-codes', { body: { phone, purpose: 'login', agree: true } }));
      setLeft(60);
      setExpired(false);
      setError(null);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '网络开小差了，请稍后再试');
    }
  };

  return (
    <Screen>
      <Button title="返回" kind="text" onPress={() => router.back()} style={styles.back} />
      <Text variant="h1">输入验证码</Text>
      <Text variant="body" color={semantic.textSecondary} style={styles.sub}>
        6 位验证码已发送至 +86 {formatPhone(phone)}
      </Text>

      <Pressable onPress={() => input.current?.focus()} style={styles.boxes} accessibilityLabel="验证码输入框">
        {Array.from({ length: LEN }, (_, i) => (
          <View key={i} style={[styles.box, i === code.length && styles.boxActive, error && styles.boxError]}>
            <Text style={styles.digit}>{code[i] ?? ''}</Text>
          </View>
        ))}
      </Pressable>
      <TextInput
        ref={input}
        accessibilityLabel="验证码"
        value={code}
        onChangeText={onChange}
        keyboardType="number-pad"
        textContentType="oneTimeCode"
        autoComplete="sms-otp"
        maxLength={LEN}
        autoFocus
        editable={!busy && !expired}
        style={styles.hidden}
      />

      {error ? (
        <Text variant="caption" color={semantic.danger} style={styles.tip}>
          {error}
        </Text>
      ) : null}

      <View style={styles.row}>
        {left > 0 ? (
          <Text variant="caption">
            <Text variant="caption" style={styles.count}>
              {left}
            </Text>{' '}
            秒后可重新获取
          </Text>
        ) : (
          <Button title="重新获取验证码" kind="text" onPress={() => void resend()} />
        )}
        <Button title="收不到验证码？" kind="text" onPress={() => setHelp(true)} />
      </View>
      <Text variant="caption" style={styles.note}>
        输满 6 位自动登录，支持短信验证码自动填充
      </Text>

      <BottomSheet visible={help} onClose={() => setHelp(false)} title="收不到验证码？">
        {[
          `确认手机号 ${formatPhone(phone)} 是否正确`,
          '看看短信是否被手机的骚扰拦截或垃圾短信收走',
          '信号不好时短信会延迟，稍等 1 分钟再试',
          '同一手机号每天最多获取 10 次验证码',
        ].map((t, i) => (
          <Text key={t} variant="body" style={styles.helpItem}>
            {i + 1}. {t}
          </Text>
        ))}
        <View style={styles.helpActions}>
          <Button
            title="更换手机号"
            kind="secondary"
            onPress={() => {
              setHelp(false);
              router.back();
            }}
            style={styles.flex}
          />
          <Button title="我知道了" onPress={() => setHelp(false)} style={styles.flex} />
        </View>
      </BottomSheet>
    </Screen>
  );
}

const styles = StyleSheet.create({
  back: { alignSelf: 'flex-start', marginTop: spacing.sm, marginBottom: spacing.lg, paddingHorizontal: 0 },
  sub: { marginTop: spacing.sm },
  boxes: { flexDirection: 'row', justifyContent: 'space-between', marginTop: spacing.xxxl },
  box: {
    width: 48,
    height: 56,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: semantic.border,
    backgroundColor: semantic.surface,
    alignItems: 'center',
    justifyContent: 'center',
  },
  boxActive: { borderColor: colors.indigo, borderWidth: 1.5 },
  boxError: { borderColor: semantic.danger },
  digit: { fontFamily: fontFamily.numberSemiBold, fontSize: 24, lineHeight: 30 },
  hidden: { position: 'absolute', opacity: 0, height: 1, width: 1 },
  tip: { marginTop: spacing.md },
  row: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginTop: spacing.lg, minHeight: layout.minTouch },
  count: { fontFamily: fontFamily.numberSemiBold, color: colors.indigo },
  note: { marginTop: spacing.sm },
  helpItem: { marginBottom: spacing.sm },
  helpActions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.lg },
  flex: { flex: 1 },
});
