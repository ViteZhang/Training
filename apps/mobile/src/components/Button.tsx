import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { ActivityIndicator, Pressable, StyleSheet, View, type PressableProps } from 'react-native';
import { Text } from './Text';

export type ButtonKind = 'primary' | 'secondary' | 'soft' | 'text' | 'danger';

export interface ButtonProps extends Omit<PressableProps, 'children'> {
  title: string;
  kind?: ButtonKind;
  /** md：50 高的页面主按钮；sm：34 高的卡片内胶囊按钮（设计稿「去练」「重做」「设目标分」） */
  size?: 'md' | 'sm';
  loading?: boolean;
  /** 撑满父容器宽度 */
  block?: boolean;
  /** 覆盖文字颜色（如文字按钮用墨色） */
  color?: string;
}

/**
 * 按钮（样式取自设计稿）：主（夜靛实底）、次（白底线色描边）、浅底胶囊、文字、危险（红色描边）。
 * 不可用时换成灰底灰字；可点区域不小于 44×44。
 */
export function Button({ title, kind = 'primary', size = 'md', loading, disabled, block, color, style, ...rest }: ButtonProps) {
  const s = kindStyles[kind];
  const inactive = disabled || loading;
  const sm = size === 'sm';
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ disabled: !!inactive, busy: !!loading }}
      disabled={inactive}
      hitSlop={sm ? 5 : undefined}
      style={(state) => [
        sm ? styles.sm : styles.base,
        s.container,
        block && styles.block,
        disabled && kind !== 'text' && styles.disabled,
        disabled && kind === 'text' && styles.dim,
        state.pressed && styles.pressed,
        typeof style === 'function' ? style(state) : style,
      ]}
      {...rest}
    >
      <View style={styles.row}>
        {loading ? <ActivityIndicator color={s.text.color} style={styles.spinner} /> : null}
        <Text variant={sm ? 'caption' : 'bodyStrong'} style={[s.text, color ? { color } : null, disabled && kind !== 'text' && styles.disabledText]}>
          {title}
        </Text>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  base: {
    minHeight: layout.buttonHeight,
    minWidth: layout.minTouch,
    paddingHorizontal: spacing.xl,
    borderRadius: radius.pill,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sm: {
    minHeight: 34,
    paddingHorizontal: 14,
    borderRadius: radius.pill,
    alignItems: 'center',
    justifyContent: 'center',
    alignSelf: 'center',
  },
  block: { alignSelf: 'stretch' },
  row: { flexDirection: 'row', alignItems: 'center' },
  spinner: { marginRight: spacing.sm },
  disabled: { backgroundColor: semantic.disabledBg, borderColor: semantic.disabledBg },
  disabledText: { color: semantic.disabledText },
  dim: { opacity: 0.4 },
  pressed: { opacity: 0.85 },
});

const kindStyles = {
  primary: StyleSheet.create({ container: { backgroundColor: semantic.primary }, text: { color: semantic.textOnBrand } }),
  secondary: StyleSheet.create({
    container: { backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
    text: { color: semantic.textPrimary, fontWeight: '400' },
  }),
  soft: StyleSheet.create({ container: { backgroundColor: semantic.fill }, text: { color: semantic.textPrimary, fontWeight: '400' } }),
  text: StyleSheet.create({
    container: { backgroundColor: 'transparent', paddingHorizontal: spacing.sm, minHeight: layout.minTouch },
    text: { color: semantic.textSecondary, fontWeight: '400' },
  }),
  danger: StyleSheet.create({
    container: { backgroundColor: semantic.surface, borderWidth: 1, borderColor: '#F0C6C2' },
    text: { color: semantic.danger, fontWeight: '400' },
  }),
};
