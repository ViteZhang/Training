# 0011 支付：安卓微信支付与支付宝，iOS 内购，服务端验签与幂等回调

- 日期：2026-10-03
- 状态：已采纳

## 背景

会员一次性购买、不自动续费（PRD 13.2、13.3）。安卓用微信支付、支付宝 App 支付；iOS 按 App Store 规则只能用 App 内购买。
商户号与内购商品审核就绪前，在线支付开关 online_payment 关闭，只能用兑换码开通。回调可能重复到达、乱序，
网络问题会让 App 重复提交；同一笔钱只能开通一次会员。

## 决定

- 渠道封装在 internal/cloud/pay，只用标准库与已有的 golang-jwt，不引入第三方支付 SDK：
  - 微信支付 APIv3：请求用商户私钥 SHA256-RSA 签名；回调用平台公钥验签（时间戳偏差超过 5 分钟拒绝，防重放），再用 APIv3 密钥 AES-256-GCM 解密，核对商户号与 AppID
  - 支付宝：RSA2 生成 App 支付 orderString；异步通知用支付宝公钥验签（去掉 sign 与 sign_type），核对 app_id
  - App Store：App 用 StoreKit 2 购买时传服务端生成的 appAccountToken（每个订单一个 UUID），购买后把交易号交给服务端；
    服务端用 App Store Server API（ES256 JWT）按交易号查询，先正式环境后沙盒，核对 bundleId、商品 ID、appAccountToken、是否撤销。
    交易信息直接取自 Apple 的 HTTPS 接口，不经过 App，所以不再校验 JWS 证书链
- 下单：订单号 T + 北京时间到秒 + 6 位随机数；同一个 idempotency_key 返回同一订单；金额取规则参数 pricing（后台可改）
- 开通在一个事务里：锁订单行 → 已支付直接返回 → 核对渠道与金额 → membership.Apply（叠加到当前会员之后）→ 标记已支付 → 发消息。
  orders 上 (channel, transaction_id) 唯一，同一笔交易不会开通两个订单
- 回调接口 POST /pay/notify/{channel} 不需要登录，靠渠道签名鉴权；读原始报文验签（不能先解析再序列化），按渠道格式应答
- 退款：先调渠道退款（退款单号 R + 订单号，重试幂等），成功后在事务里改订单状态、收回本单会员、记退款单。App Store 订单由用户向 Apple 申请，
  后台只收回会员（D32）
- 本地与测试用 mock 渠道：回调签名是 HMAC（密钥由 JWT_SECRET 派生）；生产环境不创建 mock，PAY_PROVIDER=real 时只启用配了的渠道，
  配了一半报错。非生产环境注册 POST /dev/pay/mock/{orderNo} 模拟支付成功
- 下单、订单、内购校验受 online_payment 开关控制，关闭时 404；会员中心不拦（关闭时只显示兑换码入口）；回调不拦（开关只对部分用户打开时也要处理）

## 放弃的方案与原因

- 第三方支付 SDK（如 wechatpay-go、gopay）：只用到下单、验签、退款三四个接口，标准库实现更容易审查，少一层依赖
- iOS 上传整张收据（verifyReceipt）：Apple 已弃用；appAccountToken 能把交易绑定到订单，防止拿别人的交易号开通
- 订阅型内购：PRD 明确不自动续费，用非续期订阅 / 消耗型商品即可

## 影响

- 上线前需要：微信支付商户号与 APIv3 密钥、平台公钥；支付宝应用私钥与支付宝公钥；App Store Connect 内购商品（ID 见 pricing）与 App Store Server API 密钥；
  PAY_NOTIFY_BASE_URL 指向生产域名，nginx 放行 /api/v1/pay/notify/
- 金额不符、重复支付只记错误日志，需人工核对（D33）
- 后台 7.3 的订单与退款界面在 T28 调用 payment.Service.Refund
