import { describe, expect, it } from 'vitest';
import { colors, legacyColorMap, typography } from './index';

describe('ui-tokens', () => {
  it('VI 主色与功能色', () => {
    expect(colors.indigo).toBe('#231F55');
    expect(colors.amber).toBe('#FFB547');
    expect(colors.paper).toBe('#FAF8F3');
    expect(colors.green).toBe('#2F9E6E');
    expect(colors.blue).toBe('#3E6FD8');
    expect(colors.red).toBe('#D6453D');
  });

  it('设计稿旧色全部映射到 VI 色', () => {
    for (const v of Object.values(legacyColorMap)) {
      expect(Object.values(colors)).toContain(v);
    }
  });

  it('字号层级与设计稿一致，品牌标题沿用 VI H1', () => {
    expect([typography.display.fontSize, typography.display.lineHeight]).toEqual([32, 40]);
    expect(typography.h1.fontSize).toBe(26);
    expect(typography.h3.fontSize).toBe(17);
    expect(typography.body.fontSize).toBe(15);
    expect(typography.caption.fontSize).toBe(13);
  });
});
