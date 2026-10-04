-- name: AICallHealth :one
-- 告警：一段时间内的模型调用次数与失败次数（ai_calls 只有计数，不存输入输出）。
SELECT COUNT(*) AS calls, CAST(COALESCE(SUM(success = 0), 0) AS SIGNED) AS failed FROM ai_calls WHERE created_at >= ?;
