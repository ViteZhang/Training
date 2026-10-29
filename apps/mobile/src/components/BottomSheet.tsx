import { radius, semantic, spacing } from '@training/ui-tokens';
import type { ReactNode } from 'react';
import { KeyboardAvoidingView, Modal, Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Text } from './Text';
import { ToastHost } from './Toast';

export interface BottomSheetProps {
  visible: boolean;
  onClose: () => void;
  title?: string;
  children: ReactNode;
  /** 居中弹窗形态（设计稿 0.5 权限说明、0.6 版本更新、0.4b 协议更新） */
  centered?: boolean;
  /** 居中弹窗顶部的图标块 */
  icon?: ReactNode;
  /** 点遮罩不能关闭（如强制更新） */
  dismissible?: boolean;
}

/** 底部弹层（0.2b、0.3c、4.9 等）。 */
export function BottomSheet({ visible, onClose, title, children, dismissible = true, centered, icon }: BottomSheetProps) {
  const insets = useSafeAreaInsets();
  if (centered) {
    return (
      <Modal visible={visible} transparent animationType="fade" onRequestClose={dismissible ? onClose : undefined}>
        <KeyboardAvoidingView behavior="padding" style={styles.center}>
          <Pressable style={StyleSheet.absoluteFill} onPress={dismissible ? onClose : undefined} accessibilityLabel="关闭" />
          <View style={styles.dialog} accessibilityRole="alert">
            {icon ? <View style={styles.icon}>{icon}</View> : null}
            {title ? (
              <Text variant="h3" style={[styles.dialogTitle, icon ? styles.textCenter : null]}>
                {title}
              </Text>
            ) : null}
            {children}
          </View>
        </KeyboardAvoidingView>
        <ToastHost />
      </Modal>
    );
  }
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={dismissible ? onClose : undefined}>
      {/* 弹层里有输入框（改昵称等）时，键盘弹起把整个弹层顶上去，不挡输入框 */}
      <KeyboardAvoidingView behavior="padding" style={styles.fill}>
        <Pressable style={styles.mask} onPress={dismissible ? onClose : undefined} accessibilityLabel="关闭" />
        <View style={[styles.sheet, { paddingBottom: insets.bottom + spacing.lg }]}>
          <View style={styles.handle} />
          {title ? (
            <Text variant="h2" style={styles.title}>
              {title}
            </Text>
          ) : null}
          {children}
        </View>
      </KeyboardAvoidingView>
      <ToastHost />
    </Modal>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  mask: { flex: 1, backgroundColor: semantic.scrim },
  sheet: {
    backgroundColor: semantic.surface,
    borderTopLeftRadius: radius.card,
    borderTopRightRadius: radius.card,
    paddingHorizontal: 22,
    paddingTop: 10,
  },
  handle: { alignSelf: 'center', width: 36, height: 4, borderRadius: 2, backgroundColor: semantic.border, marginBottom: 14 },
  title: { marginBottom: 6 },
  center: { flex: 1, justifyContent: 'center', paddingHorizontal: 36, backgroundColor: semantic.scrim },
  dialog: { backgroundColor: semantic.surface, borderRadius: radius.card, paddingHorizontal: 22, paddingTop: 24, paddingBottom: 20 },
  icon: { alignSelf: 'center', marginBottom: 12 },
  dialogTitle: { fontSize: 18, lineHeight: 26, marginBottom: 8 },
  textCenter: { textAlign: 'center' },
});
