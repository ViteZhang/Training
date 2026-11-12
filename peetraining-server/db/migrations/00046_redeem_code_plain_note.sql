-- 7.4 兑换码：后台直接查看、复制明文，并给每个码写备注（发给了谁），避免重复发放（D54）。
-- code_plain：新生成的码直接存明文；旧码由后台粘贴当初导出的码补录，服务端核对哈希后才写入。
-- note：发放备注，填了即视为「已发放」；未使用且有备注 = 已发放未激活。

-- +goose Up
ALTER TABLE redeem_codes
  ADD COLUMN code_plain VARCHAR(16)  NULL COMMENT '兑换码明文；旧码补录前为空' AFTER code_tail,
  ADD COLUMN note       VARCHAR(100) NULL COMMENT '发放备注（发给了谁）；有备注即已发放' AFTER membership_id,
  ADD COLUMN noted_by   BIGINT UNSIGNED NULL COMMENT '最后改备注的后台账号' AFTER note,
  ADD COLUMN noted_at   DATETIME(3)  NULL AFTER noted_by;

-- +goose Down
ALTER TABLE redeem_codes DROP COLUMN noted_at, DROP COLUMN noted_by, DROP COLUMN note, DROP COLUMN code_plain;
