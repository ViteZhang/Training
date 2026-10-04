# T25 会员与支付

- 仓库：后端 + 前端（先后端，后前端）
- 依赖：T24
- 计划完成：10/11
- 状态：后端完成，前端完成

## 目标

会员权益和额度在全站一致生效；在线支付做好但默认关闭。

## 范围 · 后端部分（在 peetraining-server 里做，先合并并打契约标签）

- 会员档位与有效期（冲刺卡至当年初试结束、考季卡至次年初试结束、月卡 30 天）；时长叠加
- 全站额度检查统一走 quota 包；会员不限次的权益
- 支付（开关）：微信支付、支付宝 App 支付下单与回调验签；App Store 内购收据验证；回调幂等；订单与退款；写 ADR 0011

## 范围 · 前端部分（在 peetraining-web 里做，先执行 pnpm gen:api）

- 6.5 会员中心（支付关闭时只显示兑换码入口）、6.6 支付结果

## 参考

- docs/prd.md 第 13 节
- docs/design/pages/m6_member、m6_payresult

## 验收

- 兑换会员后，批改、出题、解析额度立即不限
- 支付用 mock 走通下单 → 回调 → 开通；重复回调不重复开通

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - 会员档位：membership.Span 统一算起止（月卡 30 天、冲刺卡至当年初试结束、考季卡至次年初试结束，叠加到当前会员之后），兑换码、回访、订单共用
  - 额度：全站额度检查早已统一走 quota 包，会员规则来自 rule_params.quota（批改、出题、整卷、作文、导入不限，解析每月 1000 页）；兑换后下一次检查立即按会员规则
  - 会员中心 GET /membership/plans：档位价格读 rule_params.pricing，带「现在购买的有效期截止」与权益对比（quota.Benefits）；payment_enabled 为开关打开且有可用渠道
  - 支付 internal/payment + internal/cloud/pay：微信 APIv3、支付宝 RSA2、App Store Server API，标准库实现；回调开通在一个事务里锁订单行，重复回调直接返回；
    下单、订单、内购校验受 online_payment 开关控制（关闭 404）；退款服务收回本单会员（后台入口在 T28）
  - 契约 api-v0.12：/membership/plans、/orders、/orders/{orderNo}、/orders/{orderNo}/apple-verify、/pay/notify/{channel}
  - 迁移 00015：orders 加 app_account_token、idempotency_key；refunds 加 refund_no、amount_cents；pricing 补内购商品 ID
  - 前端：6.5 会员中心（支付关闭时只有兑换码入口）、6.6 支付结果（轮询订单）
- 偏离计划的地方与原因：
  - 退款只做服务与测试，后台 7.3 的界面与接口放在 T28（卡片依赖关系 T28 依赖 T25）
  - App 端调起微信、支付宝、StoreKit 需要原生 SDK 与商户号，本卡只接到「下单 → 拿到调起参数」；开发环境用「模拟支付成功」走完整流程。原生 SDK 接入记为上线前事项
- 需要手动验证的步骤：
  - 商户号就绪后：PAY_PROVIDER=real 配好密钥，对自己的账号打开 online_payment，真机各渠道付 1 单（可临时把 pricing 价格改为 1 分），确认回调开通与 6.6 显示
  - App Store 沙盒账号购买，确认 apple-verify 开通；在 App Store Connect 建好 pricing 里的三个商品 ID
  - 退款：T28 后台完成后各渠道退 1 单，确认到账与会员收回
