import { radius, semantic, spacing } from '@training/ui-tokens';
import { Modal, StyleSheet, View } from 'react-native';
import { Button } from './Button';
import { Text } from './Text';

export interface ConfirmDialogProps {
  visible: boolean;
  title: string;
  message?: string;
  confirmText?: string;
  cancelText?: string;
  /** 危险操作（删除资料、删除专业课、注销）用红色确认按钮 */
  danger?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/** 确认弹窗：退出训练、交卷、删除、退出登录、注销等危险操作（PRD 14）。 */
export function ConfirmDialog({ visible, title, message, confirmText = '确定', cancelText = '取消', danger, onConfirm, onCancel }: ConfirmDialogProps) {
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel}>
      <View style={styles.mask}>
        <View style={styles.box} accessibilityRole="alert">
          <Text variant="h3">{title}</Text>
          {message ? (
            <Text variant="body" color={semantic.textSecondary} style={styles.message}>
              {message}
            </Text>
          ) : null}
          <View style={styles.actions}>
            <Button title={cancelText} kind="secondary" onPress={onCancel} style={styles.action} />
            <Button title={confirmText} kind={danger ? 'danger' : 'primary'} onPress={onConfirm} style={styles.action} />
          </View>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  mask: { flex: 1, backgroundColor: 'rgba(27,26,23,0.45)', justifyContent: 'center', padding: spacing.xxl },
  box: { backgroundColor: semantic.surface, borderRadius: radius.xl, padding: spacing.xl },
  message: { marginTop: spacing.sm },
  actions: { flexDirection: 'row', gap: spacing.md, marginTop: spacing.xl },
  action: { flex: 1 },
});
