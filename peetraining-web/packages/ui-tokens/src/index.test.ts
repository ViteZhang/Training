import { describe, expect, it } from 'vitest';
import { brand, colors, designColorMap, fontFamily, semantic, typography } from './index';

describe('ui-tokens', () => {
  it('页面颜色与设计稿一致：白底、墨黑主按钮', () => {
    expect(semantic.background).toBe('#FFFFFF');
    expect(semantic.primary).toBe('#1A1A1A');
    expect(semantic.textSecondary).toBe('#6E6E6E');
    expect(semantic.fill).toBe('#F7F7F5');
    expect(semantic.progress).toBe('#F2956B');
    expect(semantic.danger).toBe('#C23B22');
  });

  it('VI 品牌色只在 brand 里，页面色不含夜靛', () => {
    expect(brand.indigo).toBe('#231F55');
    expect(Object.values(colors)).not.toContain(brand.indigo);
    expect(Object.values(semantic)).not.toContain(brand.indigo);
  });

  it('设计稿颜色都能查到 token 名', () => {
    expect(designColorMap['#1A1A1A']).toBe('ink');
    expect(designColorMap['#E6E6E6']).toBe('border');
    expect(semantic.border).toBe('#EFEFEF');
    expect(semantic.inputBorder).toBe('#E6E6E6');
  });

  it('数字字体用设计稿的 DM Sans', () => {
    expect(fontFamily.numberSemiBold).toBe('DMSans-SemiBold');
    expect(typography.score.fontFamily).toBe('DMSans-SemiBold');
  });

  it('字号层级与设计稿一致', () => {
    expect(typography.h1.fontSize).toBe(26);
    expect(typography.h3.fontSize).toBe(17);
    expect(typography.body.fontSize).toBe(15);
    expect(typography.caption.fontSize).toBe(13);
  });
});
