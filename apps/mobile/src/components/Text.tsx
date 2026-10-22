import { fontFamily, layout, numberFont, semantic, typography } from '@training/ui-tokens';
import { StyleSheet, Text as RNText, type TextProps as RNTextProps, type TextStyle } from 'react-native';

export type TextVariant = keyof typeof typography;

export interface TextProps extends RNTextProps {
  variant?: TextVariant;
  color?: string;
  /**
   * 数字用 DM Sans（设计稿里分数、计数、时间、百分比、「2 / 5」等）。传 true 按样式里的字重取，
   * 也可直接传字重 400 / 500 / 600 / 700。
   */
  num?: boolean | 400 | 500 | 600 | 700;
}

const numberFamilies: string[] = [fontFamily.numberRegular, fontFamily.numberMedium, fontFamily.numberSemiBold, fontFamily.numberBold];

/** 文字：按设计稿字号层级，系统字体最大放大到 1.3 倍（dev-spec 第十一节）。 */
export function Text({ variant = 'body', color, style, num, ...rest }: TextProps) {
  const t = typography[variant];
  const flat: TextStyle = StyleSheet.flatten([{ color: semantic.textPrimary }, t, color ? { color } : null, style]);
  // 安卓上自定义字体不认 fontWeight（会退回系统字体），所以按字重换成对应的 DM Sans 文件，字重本身置为 normal
  if (num) {
    flat.fontFamily = numberFont(typeof num === 'number' ? num : flat.fontWeight);
    flat.fontWeight = 'normal';
  } else if (flat.fontFamily && numberFamilies.includes(flat.fontFamily)) {
    flat.fontWeight = 'normal';
  }
  return <RNText maxFontSizeMultiplier={layout.maxFontScale} style={flat} {...rest} />;
}
