import { layout, semantic, typography } from '@training/ui-tokens';
import { Text as RNText, type TextProps as RNTextProps } from 'react-native';

export type TextVariant = keyof typeof typography;

export interface TextProps extends RNTextProps {
  variant?: TextVariant;
  color?: string;
}

/** 文字：按 VI 字号层级，系统字体最大放大到 1.3 倍（dev-spec 第十一节）。 */
export function Text({ variant = 'body', color, style, ...rest }: TextProps) {
  const t = typography[variant];
  return (
    <RNText
      maxFontSizeMultiplier={layout.maxFontScale}
      style={[{ color: semantic.textPrimary }, t, color ? { color } : null, style]}
      {...rest}
    />
  );
}
