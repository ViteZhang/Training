-- +goose Up
-- PRD v3 3.4、6.5a、7.4、7.8、13.3（10 月 26 日更新）：在线支付开通前通过客服企业微信售卖兑换码。
-- 1) 购买入口按平台开关（安卓默认开、iOS 默认关，提审通过后打开）与客服企业微信信息，存在 rule_params，后台 7.8 改，立即生效。
-- 2) 后台「单个发码 · 手动收款」：客服收款后生成一个码，同时记一笔渠道为企业微信客服的已支付订单，收入计入 7.3。
INSERT INTO rule_params (param_key, value, description) VALUES
('purchase_entry', JSON_OBJECT(
  'android', TRUE,
  'ios', FALSE,
  'wecom', JSON_OBJECT(
    'name', '',
    'wecom_id', '',
    'qr_key', '',
    'reply_time', '工作日 9:00–21:00，一般 30 分钟内回复',
    'note', '暂未开放在线支付，加客服企业微信获取兑换码'
  )
), '3.4 / 7.8 购买入口：按平台显示会员购买与兑换码入口；6.5a 客服企业微信名称、ID、二维码（OSS 对象键）、回复时间、说明文案');

ALTER TABLE orders
  MODIFY COLUMN channel ENUM('wechat','alipay','apple_iap','wecom') NOT NULL COMMENT 'wecom = 企业微信客服手动收款（7.4 单个发码）',
  ADD COLUMN pay_method VARCHAR(32) NULL COMMENT '手动收款的收款方式，如企业微信转账、收款码' AFTER channel,
  ADD COLUMN buyer_phone VARCHAR(20) NULL COMMENT '手动收款时客服填的购买人手机号，后台脱敏显示' AFTER pay_method,
  ADD COLUMN remark VARCHAR(255) NULL AFTER buyer_phone,
  ADD COLUMN redeem_code_id BIGINT UNSIGNED NULL COMMENT '手动收款生成的兑换码；兑换后订单补上 owner_user_id 与 membership_id' AFTER membership_id,
  ADD COLUMN created_by BIGINT UNSIGNED NULL COMMENT '手动收款：经办的后台账号' AFTER redeem_code_id,
  ADD UNIQUE KEY uk_orders_redeem_code (redeem_code_id);

-- +goose Down
ALTER TABLE orders
  DROP INDEX uk_orders_redeem_code,
  DROP COLUMN created_by,
  DROP COLUMN redeem_code_id,
  DROP COLUMN remark,
  DROP COLUMN buyer_phone,
  DROP COLUMN pay_method,
  MODIFY COLUMN channel ENUM('wechat','alipay','apple_iap') NOT NULL;
DELETE FROM rule_params WHERE param_key = 'purchase_entry';
