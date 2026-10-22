-- +goose Up
-- T25 会员与支付：App Store 内购用 appAccountToken 把交易绑到订单；退款单号；会员档位补内购商品（PRD 13.2，后台可改）。
ALTER TABLE orders
  ADD COLUMN app_account_token CHAR(36) NULL COMMENT 'App Store 内购：购买时传给 StoreKit 的 appAccountToken，校验交易属于这个订单' AFTER transaction_id,
  ADD COLUMN idempotency_key VARCHAR(64) NULL AFTER app_account_token,
  ADD UNIQUE KEY uk_orders_idem (owner_user_id, idempotency_key);

ALTER TABLE refunds
  ADD COLUMN refund_no VARCHAR(32) NULL AFTER order_id,
  ADD COLUMN amount_cents INT UNSIGNED NULL AFTER refund_no,
  ADD UNIQUE KEY uk_refunds_no (refund_no);

-- 会员档位沿用 00008 的 pricing（后台 7.8 改价格），补上 App Store 内购商品 ID。
UPDATE rule_params SET value = JSON_SET(value,
  '$.sprint.apple_product_id', 'cn.dreamelab.training.sprint',
  '$.season.apple_product_id', 'cn.dreamelab.training.season',
  '$.monthly.apple_product_id', 'cn.dreamelab.training.monthly'),
  description = 'PRD 13.2 会员档位（测试价，单位分）与 App Store 内购商品 ID'
WHERE param_key = 'pricing';

-- +goose Down
UPDATE rule_params SET value = JSON_REMOVE(value, '$.sprint.apple_product_id', '$.season.apple_product_id', '$.monthly.apple_product_id'),
  description = 'PRD 13.2 会员档位（测试价）' WHERE param_key = 'pricing';
ALTER TABLE refunds DROP INDEX uk_refunds_no, DROP COLUMN amount_cents, DROP COLUMN refund_no;
ALTER TABLE orders DROP INDEX uk_orders_idem, DROP COLUMN idempotency_key, DROP COLUMN app_account_token;
