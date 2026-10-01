// 0.2 手机号登录 / 0.2a 已输入手机号 / 0.2b 未同意协议。
// 号码满 11 位且以 13–19 开头才可获取验证码；协议默认不勾选；未注册的手机号验证后自动创建账号。
import { semantic, spacing, radius, layout } from '@training/ui-tokens';
import { ApiError } from '@training/api-client';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Icon, Screen, Text } from '@/components';
import { formatPhone, isValidPhone, normalizePhone } from '@/features/auth/phone';
import { api, unwrap } from '@/lib/api';

export default function Login() {
  const [phone, setPhone] = useState('');
  const [agreed, setAgreed] = useState(false);
  const [agreeSheet, setAgreeSheet] = useState(false);
  const [highlight, setHighlight] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const valid = isValidPhone(phone);
  const showFormatError = phone.length === 11 && !valid;

  const send = async () => {
    setBusy(true);
    setError(null);
    try {
      await unwrap(api.POST('/auth/sms-codes', { body: { phone, purpose: 'login', agree: true } }));
      router.push({ pathname: '/(auth)/code', params: { phone } });
    } catch (e) {
      // 60 秒冷却内重复进入：直接去输入验证码页。
      if (e instanceof ApiError && e.detail?.reason === 'cooldown') {
        router.push({ pathname: '/(auth)/code', params: { phone, cooldown: String(e.detail.retry_after_seconds ?? 60) } });
      } else {
        setError(e instanceof ApiError ? e.message : '网络开小差了，请稍后再试');
      }
    } finally {
      setBusy(false);
    }
  };

  const onGetCode = () => {
    if (!agreed) {
      setAgreeSheet(true);
      return;
    }
    void send();
  };

  return (
    <Screen>
      <View style={styles.header}>
        <Text variant="h1">登录考研Training</Text>
        <Text variant="body" color={semantic.textSecondary} style={styles.sub}>
          题库和学习进度都保存在账号里，换手机也不会丢
        </Text>
      </View>

      <View style={[styles.inputRow, showFormatError && styles.inputError]}>
        <Text variant="bodyStrong">+86</Text>
        <View style={styles.divider} />
        <TextInput
          accessibilityLabel="手机号"
          placeholder="手机号"
          placeholderTextColor={semantic.textSecondary}
          keyboardType="number-pad"
          textContentType="telephoneNumber"
          autoComplete="tel"
          maxLength={13}
          value={formatPhone(phone)}
          onChangeText={(t) => setPhone(normalizePhone(t))}
          style={styles.input}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
      </View>
      {showFormatError ? (
        <Text variant="caption" color={semantic.danger} style={styles.tip}>
          请输入正确的手机号
        </Text>
      ) : null}
      {error ? (
        <Text variant="caption" color={semantic.danger} style={styles.tip}>
          {error}
        </Text>
      ) : null}

      <Button title="获取验证码" disabled={!valid} loading={busy} onPress={onGetCode} style={styles.button} />

      <Pressable
        accessibilityRole="checkbox"
        accessibilityState={{ checked: agreed }}
        onPress={() => {
          setAgreed(!agreed);
          setHighlight(false);
        }}
        style={[styles.agreeRow, highlight && styles.agreeHighlight]}
      >
        <View style={[styles.checkbox, agreed && styles.checkboxOn]}>{agreed ? <Icon name="check" size={14} color="#FFFFFF" /> : null}</View>
        <Text variant="caption" style={styles.agreeText}>
          已阅读并同意
          <Text variant="caption" color={semantic.info} onPress={() => router.push('/(auth)/agreement?kind=user')}>
            《用户协议》
          </Text>
          和
          <Text variant="caption" color={semantic.info} onPress={() => router.push('/(auth)/agreement?kind=privacy')}>
            《隐私政策》
          </Text>
          ，未注册的手机号验证后自动创建账号
        </Text>
      </Pressable>

      <Text variant="caption" style={styles.footer}>
        首期仅支持手机号验证码登录
      </Text>

      <BottomSheet visible={agreeSheet} onClose={() => setAgreeSheet(false)} title="请阅读并同意以下条款">
        <Text variant="body">
          为保障你的权益，登录前请阅读并同意
          <Text variant="body" color={semantic.info} onPress={() => router.push('/(auth)/agreement?kind=user')}>
            《用户协议》
          </Text>
          和
          <Text variant="body" color={semantic.info} onPress={() => router.push('/(auth)/agreement?kind=privacy')}>
            《隐私政策》
          </Text>
          。你上传的资料和题目仅本人可见。
        </Text>
        <View style={styles.sheetActions}>
          <Button
            title="同意并获取验证码"
            onPress={() => {
              setAgreed(true);
              setAgreeSheet(false);
              void send();
            }}
          />
          <Button
            title="不同意"
            kind="text"
            onPress={() => {
              setAgreeSheet(false);
              setHighlight(true);
            }}
          />
        </View>
      </BottomSheet>
    </Screen>
  );
}

const styles = StyleSheet.create({
  header: { marginTop: 56, marginBottom: spacing.xxxl },
  sub: { marginTop: spacing.sm },
  inputRow: {
    flexDirection: 'row',
    alignItems: 'center',
    minHeight: 56,
    paddingHorizontal: spacing.lg,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: semantic.border,
    backgroundColor: semantic.surface,
  },
  inputError: { borderColor: semantic.danger },
  divider: { width: 1, height: 20, backgroundColor: semantic.border, marginHorizontal: spacing.md },
  input: { flex: 1, fontSize: 18, color: semantic.textPrimary, minHeight: layout.minTouch },
  tip: { marginTop: spacing.sm },
  button: { marginTop: spacing.xl },
  agreeRow: { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.sm, marginTop: spacing.lg, padding: spacing.xs, borderRadius: radius.sm },
  agreeHighlight: { backgroundColor: semantic.amberSoft },
  checkbox: { width: 18, height: 18, marginTop: 1, borderRadius: 9, borderWidth: 1.5, borderColor: semantic.textSecondary, alignItems: 'center', justifyContent: 'center' },
  checkboxOn: { backgroundColor: semantic.primary, borderColor: semantic.primary },
  agreeText: { flex: 1 },
  footer: { position: 'absolute', bottom: 40, alignSelf: 'center' },
  sheetActions: { gap: spacing.sm, marginTop: spacing.xl },
});
