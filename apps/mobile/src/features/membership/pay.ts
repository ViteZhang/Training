import type { Schemas } from '@training/api-client';
import { appConfig } from '@/lib/config';
import { useSession } from '@/lib/session';

export type PayOutcome = { ok: true } | { ok: false; cancelled?: boolean; message: string };

/**
 * 调起支付。服务端用 mock 渠道（只在开发环境）时，向 /dev/pay/mock/{orderNo} 发一条模拟的支付成功回调，走完「下单 → 回调 → 开通」。
 * 微信、支付宝 SDK 与 StoreKit 需要原生模块，等商户号与内购商品就绪后接入（ADR 0011）；在那之前在线支付开关保持关闭。
 */
export async function launchPay(order: Schemas['Order']): Promise<PayOutcome> {
  if (order.prepay?.mock === '1' && appConfig.variant !== 'production') {
    const base = appConfig.apiBaseUrl.replace(/\/api\/v1\/?$/, '');
    try {
      const r = await fetch(`${base}/dev/pay/mock/${encodeURIComponent(order.order_no)}`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${useSession.getState().session?.accessToken ?? ''}` },
      });
      return r.ok ? { ok: true } : { ok: false, message: '模拟支付失败，请重试' };
    } catch {
      return { ok: false, message: '网络不太顺畅，请重试' };
    }
  }
  return { ok: false, message: '当前版本暂不支持这种支付方式，请更新 App 或使用兑换码开通' };
}
