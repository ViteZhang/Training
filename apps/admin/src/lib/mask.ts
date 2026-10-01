/** 手机号默认脱敏显示（PRD 10.1）：138****5678。 */
export function maskPhone(phone: string): string {
  return /^1\d{10}$/.test(phone) ? `${phone.slice(0, 3)}****${phone.slice(7)}` : phone;
}
