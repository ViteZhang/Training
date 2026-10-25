// 修改昵称弹层（6.1 我的、6.10 设置）：自己填，或让服务端随机给一个上岸、鼓励类昵称。
import { colors, radius, semantic, spacing } from '@training/ui-tokens';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';
import { BottomSheet, Button, Text, toast } from '@/components';
import { api, unwrap } from '@/lib/api';
import { mineKeys } from './api';

export const NICKNAME_MAX = 16;

/** 打开时才挂载（父组件按开关条件渲染），每次打开都从当前昵称开始编辑。 */
export function NicknameSheet({ current, onClose }: { current: string; onClose: () => void }) {
  const qc = useQueryClient();
  const [value, setValue] = useState(current);

  // 传空昵称时服务端随机分配一个（与注册时的默认昵称同一套）。
  const save = useMutation({
    mutationFn: (nickname: string) => unwrap(api.PATCH('/me', { body: { nickname } })),
    onSuccess: (me, nickname) => {
      qc.setQueryData(mineKeys.me, me);
      if (nickname) {
        toast('昵称已修改');
        onClose();
      } else {
        setValue(me.nickname);
      }
    },
    onError: () => toast('保存失败，请重试'),
  });
  const trimmed = value.trim();

  return (
    <BottomSheet visible onClose={onClose} title="修改昵称">
      <View style={styles.box}>
        <TextInput
          accessibilityLabel="昵称"
          placeholder="给自己起个名字"
          placeholderTextColor={colors.faint}
          value={value}
          onChangeText={setValue}
          maxLength={NICKNAME_MAX}
          autoFocus
          style={styles.input}
        />
        <Text variant="caption" num color={semantic.textTertiary}>
          {[...value].length}/{NICKNAME_MAX}
        </Text>
      </View>
      <View style={styles.actions}>
        <Button title="随机换一个" kind="secondary" style={styles.flex} disabled={save.isPending} onPress={() => save.mutate('')} />
        <Button
          title="保存"
          style={styles.flex}
          loading={save.isPending}
          disabled={!trimmed || trimmed === current}
          onPress={() => save.mutate(trimmed)}
        />
      </View>
    </BottomSheet>
  );
}

const styles = StyleSheet.create({
  box: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    minHeight: 52,
    paddingHorizontal: 16,
    borderRadius: radius.card,
    borderWidth: 1,
    borderColor: semantic.border,
    backgroundColor: semantic.surface,
  },
  input: { flex: 1, minWidth: 0, minHeight: 44, fontSize: 15, padding: 0, color: semantic.textPrimary },
  actions: { flexDirection: 'row', gap: spacing.sm, paddingVertical: spacing.lg },
  flex: { flex: 1 },
});
