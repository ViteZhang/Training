-- 规则参数与功能开关（T06 读取，T07 / T29 管理）。

-- name: ListRuleParams :many
SELECT param_key, value FROM rule_params;

-- name: ListFeatureFlags :many
SELECT flag_key, enabled_for_all FROM feature_flags;

-- name: ListUserFeatureFlags :many
SELECT flag_key FROM feature_flag_users WHERE user_id = ?;
