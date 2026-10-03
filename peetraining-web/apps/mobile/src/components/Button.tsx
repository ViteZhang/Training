import { layout, radius, semantic, spacing } from '@training/ui-tokens';
import { ActivityIndicator, Pressable, StyleSheet, View, type PressableProps } from 'react-native';
import { Text } from './Text';

export type ButtonKind = 'primary' | 'secondary' | 'text' | 'danger';

export interface ButtonProps extends Omit<PressableProps, 'children'> {
  title: string;
  kind?: ButtonKind;
  loading?: boolean;
  /** 撑满父容器宽度 */
  block?: boolean;
}

/** 按钮：主（夜靛实底）、次（描边）、文字、危险。可点区域不小于 44×44。 */
export function Button({ title, kind = 'primary', loading, disabled, block, style, ...rest }: ButtonProps) {
  const s = kindStyles[kind];
  const inactive = disabled || loading;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ disabled: !!inactive, busy: !!loading }}
      disabled={inactive}
      style={(state) => [
        styles.base,
        s.container,
        block && styles.block,
        inactive && styles.disabled,
        state.pressed && styles.pressed,
        typeof style === 'function' ? style(state) : style,
      ]}
      {...rest}
    >
      <View style={styles.row}>
        {loading ? <ActivityIndicator color={s.text.color} style={styles.spinner} /> : null}
        <Text variant="bodyStrong" style={s.text}>
          {title}
        </Text>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  base: {
    minHeight: layout.minTouch + 4,
    minWidth: layout.minTouch,
    paddingHorizontal: spacing.xl,
    borderRadius: radius.lg,
    alignItems: 'center',
    justifyContent: 'center',
  },
  block: { alignSelf: 'stretch' },
  row: { flexDirection: 'row', alignItems: 'center' },
  spinner: { marginRight: spacing.sm },
  disabled: { opacity: 0.4 },
  pressed: { opacity: 0.85 },
});

const kindStyles = {
  primary: StyleSheet.create({ container: { backgroundColor: semantic.primary }, text: { color: semantic.textOnBrand } }),
  secondary: StyleSheet.create({
    container: { backgroundColor: semantic.surface, borderWidth: 1, borderColor: semantic.border },
    text: { color: semantic.primary },
  }),
  text: StyleSheet.create({ container: { backgroundColor: 'transparent', paddingHorizontal: spacing.sm }, text: { color: semantic.primary } }),
  danger: StyleSheet.create({ container: { backgroundColor: semantic.danger }, text: { color: semantic.textOnBrand } }),
};
