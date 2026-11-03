import dayjs from 'dayjs';
import { describe, expect, it } from 'vitest';
import { bjWall, bjWallToISO, dt, isoToPicker, pickerToISO } from './time';

describe('北京时间', () => {
  it('显示不随浏览器时区变化', () => {
    expect(dt('2026-11-02T18:30:00Z')).toBe('2026-11-03 02:30');
    expect(dt('2026-11-03T10:05:00+08:00')).toBe('2026-11-03 10:05');
    expect(dt(null)).toBe('—');
    expect(dt('bad')).toBe('—');
  });
  it('录入按北京时间换算成 UTC', () => {
    expect(bjWallToISO('2026-11-03T08:00')).toBe('2026-11-03T00:00:00.000Z');
    expect(bjWallToISO('2026-12-31T23:59:59')).toBe('2026-12-31T15:59:59.000Z');
    expect(pickerToISO(dayjs('2026-11-03T08:00:00'))).toBe('2026-11-03T00:00:00.000Z');
  });
  it('回显与录入互逆', () => {
    const iso = '2026-11-02T18:30:00.000Z';
    expect(bjWall(iso)).toBe('2026-11-03T02:30:00');
    expect(pickerToISO(isoToPicker(iso))).toBe(iso);
  });
});
