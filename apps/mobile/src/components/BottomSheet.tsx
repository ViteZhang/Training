import { radius, semantic, spacing } from '@training/ui-tokens';
import type { ReactNode } from 'react';
import { Modal, Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Text } from './Text';

export interface BottomSheetProps {
  visible: boolean;
  onClose: () => void;
  title?: string;
  children: ReactNode;
  /** 点遮罩不能关闭（如强制更新） */
  dismissible?: boolean;
}

/** 底部弹层（0.2b、0.3c、4.9 等）。 */
export function BottomSheet({ visible, onClose, title, children, dismissible = true }: BottomSheetProps) {
  const insets = useSafeAreaInsets();
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={dismissible ? onClose : undefined}>
      <Pressable style={styles.mask} onPress={dismissible ? onClose : undefined} accessibilityLabel="关闭" />
      <View style={[styles.sheet, { paddingBottom: insets.bottom + spacing.lg }]}>
        <View style={styles.handle} />
        {title ? (
          <Text variant="h3" style={styles.title}>
            {title}
          </Text>
        ) : null}
        {children}
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  mask: { flex: 1, backgroundColor: 'rgba(27,26,23,0.45)' },
  sheet: {
    backgroundColor: semantic.surface,
    borderTopLeftRadius: radius.xl + 4,
    borderTopRightRadius: radius.xl + 4,
    paddingHorizontal: spacing.xl,
    paddingTop: spacing.sm,
  },
  handle: { alignSelf: 'center', width: 36, height: 4, borderRadius: 2, backgroundColor: semantic.border, marginBottom: spacing.md },
  title: { marginBottom: spacing.md },
});
