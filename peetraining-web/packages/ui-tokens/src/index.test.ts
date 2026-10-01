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

  it('字号层级与 VI 第 05 板一致', () => {
    expect([typography.h1.fontSize, typography.h1.lineHeight]).toEqual([32, 40]);
    expect([typography.h2.fontSize, typography.h2.lineHeight]).toEqual([22, 30]);
    expect([typography.body.fontSize, typography.body.lineHeight]).toEqual([16, 24]);
    expect([typography.caption.fontSize, typography.caption.lineHeight]).toEqual([13, 18]);
    expect([typography.score.fontSize, typography.score.lineHeight]).toEqual([48, 52]);
  });
});
