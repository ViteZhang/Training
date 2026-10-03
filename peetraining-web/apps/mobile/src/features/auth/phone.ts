/** 手机号输入：只保留数字，最多 11 位，按 3-4-4 分隔显示（PRD 0.2）。 */
export function normalizePhone(input: string): string {
  return input.replace(/\D/g, '').slice(0, 11);
}

export function formatPhone(digits: string): string {
  const d = normalizePhone(digits);
  if (d.length <= 3) return d;
  if (d.length <= 7) return `${d.slice(0, 3)} ${d.slice(3)}`;
  return `${d.slice(0, 3)} ${d.slice(3, 7)} ${d.slice(7)}`;
}

/** 满 11 位且以 13–19 开头才是合法手机号。 */
export function isValidPhone(digits: string): boolean {
  return /^1[3-9]\d{9}$/.test(digits);
}
