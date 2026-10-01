import { radius, spacing, timing } from '@training/ui-tokens';
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

/** 放在根布局里一次。 */
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
        <Text variant="body" color="#FFFFFF">
          {message}
        </Text>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { position: 'absolute', left: 0, right: 0, alignItems: 'center' },
  toast: { backgroundColor: 'rgba(27,26,23,0.88)', paddingHorizontal: spacing.xl, paddingVertical: spacing.md, borderRadius: radius.md },
});
