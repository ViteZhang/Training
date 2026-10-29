import { colors, radius, semantic, spacing, timing } from '@training/ui-tokens';
import { useEffect } from 'react';
import { StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { create } from 'zustand';
import { Text } from './Text';

interface ToastState {
  message?: string;
  seq: number;
  show: (message: string) => void;
  hide: () => void;
}

/** 操作反馈（PRD 14）：加入错题本、已保存、已复制等，2 秒消失。任何地方调用 toast('已保存')。 */
export const useToast = create<ToastState>((set) => ({
  seq: 0,
  show: (message) => set((s) => ({ message, seq: s.seq + 1 })),
  hide: () => set({ message: undefined }),
}));

export const toast = (message: string) => useToast.getState().show(message);

/**
 * 根布局里放一次；弹层（BottomSheet、ConfirmDialog）里也各放一个：Modal 是盖在页面上的独立窗口，
 * 根布局的 toast 会被挡住，弹层里操作（如随机换昵称失败）的提示就看不到了。
 */
export function ToastHost() {
  const { message, seq, hide } = useToast();
  const insets = useSafeAreaInsets();
  useEffect(() => {
    if (!message) return;
    const t = setTimeout(hide, timing.toastMs);
    return () => clearTimeout(t);
  }, [message, seq, hide]);
  if (!message) return null;
  return (
    <View pointerEvents="none" style={[styles.wrap, { bottom: insets.bottom + 96 }]}>
      <View style={styles.toast} accessibilityLiveRegion="polite">
        <Text variant="caption" color={colors.white} style={styles.text}>
          {message}
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  // 两侧留足边距、限制最大宽度：长文案换行而不是横跨整屏
  wrap: { position: 'absolute', left: 48, right: 48, alignItems: 'center' },
  toast: { maxWidth: 280, backgroundColor: semantic.toast, paddingHorizontal: spacing.lg, paddingVertical: 10, borderRadius: radius.md },
  text: { fontSize: 14, lineHeight: 20, textAlign: 'center' },
});
