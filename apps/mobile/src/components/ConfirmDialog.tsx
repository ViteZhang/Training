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
          <Text variant="h3" style={styles.title}>
            {title}
          </Text>
          {message ? (
            <Text variant="caption" style={styles.message}>
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
  mask: { flex: 1, backgroundColor: 'rgba(27,26,23,0.45)', justifyContent: 'center', paddingHorizontal: 40 },
  box: { backgroundColor: semantic.surface, borderRadius: radius.card, paddingHorizontal: 22, paddingTop: 24, paddingBottom: 20 },
  title: { fontSize: 18, lineHeight: 26, textAlign: 'center' },
  message: { marginTop: spacing.sm, textAlign: 'center', lineHeight: 21 },
  actions: { flexDirection: 'row', gap: 10, marginTop: 20 },
  action: { flex: 1, minHeight: 46 },
});
