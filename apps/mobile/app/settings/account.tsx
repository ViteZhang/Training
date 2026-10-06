// 6.11 账号与安全：登录方式（手机号脱敏，可更换，更换需新旧号码验证）；登录设备列表，可移除其他设备；注销入口。
import type { Schemas } from '@training/api-client';
import { ApiError } from '@training/api-client';
import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { router } from 'expo-router';
import { useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Card, NavBar, ConfirmDialog, ErrorState, Loading, Screen, Tag, Text, toast } from '@/components';
import { formatPhone, isValidPhone, normalizePhone } from '@/features/auth/phone';
import { api, unwrap } from '@/lib/api';

function shortDate(iso: string) {
  const d = new Date(iso);
  return `${d.getMonth() + 1} 月 ${d.getDate()} 日`;
}

export default function AccountSecurity() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ['me'], queryFn: () => unwrap(api.GET('/me')) });
  const devices = useQuery({ queryKey: ['devices'], queryFn: () => unwrap(api.GET('/me/devices')) });
  const [removing, setRemoving] = useState<Schemas['Device'] | null>(null);
  const [changing, setChanging] = useState(false);

  const remove = useMutation({
    mutationFn: (id: string) => unwrap(api.DELETE('/me/devices/{deviceId}', { params: { path: { deviceId: id } } })),
    onSuccess: () => {
      toast('已移除，该设备需要重新登录');
      void qc.invalidateQueries({ queryKey: ['devices'] });
    },
    onError: (e) => toast(e instanceof ApiError ? e.message : '操作失败，请重试'),
  });

  return (
    <Screen scroll>
      <NavBar title="账号与安全" />

      <Text variant="caption" style={styles.section}>
        登录方式
      </Text>
      <Card>
        {me.isLoading ? <Loading rows={1} /> : null}
        {me.isError ? <ErrorState error={me.error} onRetry={() => void me.refetch()} /> : null}
        {me.data ? (
          <View style={styles.row}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">手机号</Text>
              <Text variant="caption">首期只支持手机号 + 短信验证码登录</Text>
            </View>
            <Text variant="body">{me.data.phone_masked}</Text>
            <Button title="更换" kind="text" onPress={() => setChanging(true)} />
          </View>
        ) : null}
      </Card>

      <Text variant="caption" style={styles.section}>
        安全 · 登录设备
      </Text>
      <Card>
        {devices.isLoading ? <Loading rows={2} /> : null}
        {devices.isError ? <ErrorState error={devices.error} onRetry={() => void devices.refetch()} /> : null}
        {devices.data?.items.map((d) => (
          <View key={d.device_id} style={styles.row}>
            <View style={styles.flex}>
              <Text variant="bodyStrong">{d.device_name || (d.platform === 'ios' ? 'iPhone' : '安卓手机')}</Text>
              <Text variant="caption">{d.current ? '本机' : `最近使用 ${shortDate(d.last_used_at)}`}</Text>
            </View>
            {d.current ? <Tag label="本机" tone="brand" /> : <Button title="移除" kind="text" onPress={() => setRemoving(d)} />}
          </View>
        ))}
      </Card>

      <Text variant="caption" style={styles.section}>
        注销
      </Text>
      <Pressable accessibilityRole="button" onPress={() => router.push('/settings/delete-account')}>
        <Card style={styles.row}>
          <View style={styles.flex}>
            <Text variant="bodyStrong" color={semantic.danger}>
              注销账号
            </Text>
            <Text variant="caption">资料、题库和学习记录会一起删除</Text>
          </View>
        </Card>
      </Pressable>

      <ConfirmDialog
        visible={!!removing}
        title="移除这台设备？"
        message="移除后该设备需要重新登录。"
        confirmText="移除"
        danger
        onCancel={() => setRemoving(null)}
        onConfirm={() => {
          if (removing) remove.mutate(removing.device_id);
          setRemoving(null);
        }}
      />
      {changing && me.data ? (
        <ChangePhoneSheet
          onClose={() => setChanging(false)}
          onDone={() => {
            setChanging(false);
            toast('手机号已更换');
            void qc.invalidateQueries({ queryKey: ['me'] });
          }}
        />
      ) : null}
    </Screen>
  );
}

/** 更换手机号：先验证当前号码，再验证新号码。 */
function ChangePhoneSheet({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [step, setStep] = useState<'old' | 'new'>('old');
  const [oldPhone, setOldPhone] = useState('');
  const [oldCode, setOldCode] = useState('');
  const [newPhone, setNewPhone] = useState('');
  const [newCode, setNewCode] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [sent, setSent] = useState<{ old?: boolean; new?: boolean }>({});

  const sendCode = async (purpose: 'change_phone_old' | 'change_phone_new', phone: string) => {
    setError(null);
    try {
      await unwrap(api.POST('/auth/sms-codes', { body: { phone, purpose } }));
      setSent((s) => ({ ...s, [purpose === 'change_phone_old' ? 'old' : 'new']: true }));
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '发送失败，请重试');
    }
  };
  const submit = async () => {
    setError(null);
    try {
      await unwrap(api.PUT('/me/phone', { body: { old_code: oldCode, new_phone: newPhone, new_code: newCode } }));
      onDone();
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '更换失败，请重试');
    }
  };

  return (
    <BottomSheet visible onClose={onClose} title={step === 'old' ? '验证当前手机号' : '绑定新手机号'}>
      {step === 'old' ? (
        <>
          <PhoneCodeFields phone={oldPhone} setPhone={setOldPhone} code={oldCode} setCode={setOldCode} sent={!!sent.old} onSend={() => void sendCode('change_phone_old', oldPhone)} />
          <Button title="下一步" disabled={oldCode.length !== 6} onPress={() => setStep('new')} style={styles.sheetButton} />
        </>
      ) : (
        <>
          <PhoneCodeFields phone={newPhone} setPhone={setNewPhone} code={newCode} setCode={setNewCode} sent={!!sent.new} onSend={() => void sendCode('change_phone_new', newPhone)} />
          <Button title="确认更换" disabled={newCode.length !== 6} onPress={() => void submit()} style={styles.sheetButton} />
        </>
      )}
      {error ? (
        <Text variant="caption" color={semantic.danger} style={styles.error}>
          {error}
        </Text>
      ) : null}
    </BottomSheet>
  );
}

function PhoneCodeFields(p: { phone: string; setPhone: (v: string) => void; code: string; setCode: (v: string) => void; sent: boolean; onSend: () => void }) {
  return (
    <View style={styles.fields}>
      <TextInput
        accessibilityLabel="手机号"
        placeholder="手机号"
        keyboardType="number-pad"
        value={formatPhone(p.phone)}
        onChangeText={(t) => p.setPhone(normalizePhone(t))}
        style={styles.field}
        maxFontSizeMultiplier={layout.maxFontScale}
      />
      <View style={styles.row}>
        <TextInput
          accessibilityLabel="验证码"
          placeholder="6 位验证码"
          keyboardType="number-pad"
          textContentType="oneTimeCode"
          autoComplete="sms-otp"
          maxLength={6}
          value={p.code}
          onChangeText={(t) => p.setCode(t.replace(/\D/g, ''))}
          style={[styles.field, styles.flex]}
          maxFontSizeMultiplier={layout.maxFontScale}
        />
        <Button title={p.sent ? '重新获取' : '获取验证码'} kind="secondary" disabled={!isValidPhone(p.phone)} onPress={p.onSend} />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  back: { alignSelf: 'flex-start', marginTop: spacing.sm, paddingHorizontal: 0 },
  title: { marginBottom: spacing.sm },
  section: { marginTop: spacing.xl, marginBottom: spacing.sm },
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.md, minHeight: layout.minTouch, paddingVertical: spacing.xs },
  flex: { flex: 1 },
  fields: { gap: spacing.md },
  field: {
    minHeight: 48,
    borderWidth: 1,
    borderColor: semantic.border,
    borderRadius: radius.md,
    paddingHorizontal: spacing.md,
    fontSize: 16,
    color: semantic.textPrimary,
  },
  sheetButton: { marginTop: spacing.lg },
  error: { marginTop: spacing.sm },
});
