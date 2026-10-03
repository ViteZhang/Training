import { createApiClient, unwrap } from '@training/api-client';
import { clearSession, getToken } from './session';

/**
 * 后台接口客户端：/api/v1/admin，令牌是两步验证后签发的后台会话（8 小时）。
 * 会话过期（401）时清掉登录态，回到登录页。权限只影响界面，安全由后端按 x-roles 保证。
 */
export const api = createApiClient({
  // 绝对地址：同域部署（/admin/ 与 /api/v1/ 在同一个域名下）。
  baseUrl: `${globalThis.location?.origin ?? ''}/api/v1`,
  getAccessToken: getToken,
  refresh: async () => {
    clearSession();
    return undefined;
  },
});

export { unwrap };

/** 金额（分）→ ¥169.00。 */
export function yuan(cents: number) {
  return `¥${(cents / 100).toFixed(2)}`;
}

/** 0.953 → 95.3%。 */
export function pct(v: number) {
  return `${(v * 100).toFixed(1)}%`;
}

/** ISO 时间 → 2026-10-03 10:05（本地时区）。 */
export function dt(iso?: string | null) {
  if (!iso) return '—';
  const d = new Date(iso);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** 秒 → 「3 分 20 秒」。 */
export function dur(seconds: number) {
  if (seconds < 60) return `${seconds} 秒`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`;
  return `${(seconds / 3600).toFixed(1)} 小时`;
}

/** 生成 CSV 并下载（兑换码导出）。 */
export function downloadCSV(name: string, rows: (string | number | undefined | null)[][]) {
  const text = rows.map((r) => r.map((c) => `"${String(c ?? "").replace(/"/g, '""')}"`).join(',')).join('\n');
  const blob = new Blob(['﻿' + text], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
