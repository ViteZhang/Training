-- +goose Up
-- 刷新令牌轮换时记下接班令牌。旧令牌已作废又被使用时，如果接班令牌还没用过，说明上次刷新的响应丢了
-- （安卓锁屏后网络被系统掐断），换发一对新令牌，不当作泄露把用户踢下线。
ALTER TABLE refresh_tokens ADD COLUMN rotated_to_id BIGINT UNSIGNED NULL COMMENT '轮换时发出的接班令牌 ID';

-- +goose Down
ALTER TABLE refresh_tokens DROP COLUMN rotated_to_id;
