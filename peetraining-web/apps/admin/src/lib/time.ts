import dayjs, { type Dayjs } from 'dayjs';

// 后台时间一律按北京时间（UTC+8）显示和录入，不随浏览器时区变化，方便与用户、日志校对。
// 中国不实行夏令时，固定偏移即可。
const BJ_OFFSET_MS = 8 * 3_600_000;
const p = (n: number) => String(n).padStart(2, '0');

/** 时间点 → 北京时间墙钟「2026-10-03T10:05:09」（不带时区）。 */
export function bjWall(value: string | number | Date) {
  const d = new Date(new Date(value).getTime() + BJ_OFFSET_MS);
  return `${d.getUTCFullYear()}-${p(d.getUTCMonth() + 1)}-${p(d.getUTCDate())}T${p(d.getUTCHours())}:${p(d.getUTCMinutes())}:${p(d.getUTCSeconds())}`;
}

/** ISO 时间 → 2026-10-03 10:05（北京时间）。 */
export function dt(iso?: string | number | Date | null) {
  if (iso === undefined || iso === null || iso === '') return '—';
  if (Number.isNaN(new Date(iso).getTime())) return '—';
  return bjWall(iso).slice(0, 16).replace('T', ' ');
}

/** 北京时间墙钟「2026-10-03T10:05」或「2026-10-03T10:05:09」→ ISO（UTC）。 */
export function bjWallToISO(wall: string) {
  const full = wall.length === 16 ? `${wall}:00` : wall;
  return new Date(`${full}+08:00`).toISOString();
}

/** 日期选择器的值按北京时间理解 → ISO。 */
export function pickerToISO(d: Dayjs) {
  return bjWallToISO(d.format('YYYY-MM-DDTHH:mm:ss'));
}

/** ISO → 日期选择器的值（显示北京时间）。 */
export function isoToPicker(iso: string) {
  return dayjs(bjWall(iso));
}
