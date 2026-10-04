-- AI 调用账本与灰度（T10）。不含用户内容，user_hash 是用户 ID 的哈希。

-- name: InsertAICall :exec
INSERT INTO ai_calls (capability, model, prompt_version, user_hash, input_tokens, output_tokens, cost_micro_yuan, latency_ms, success, error_kind, retried)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAIRollout :one
SELECT * FROM ai_rollouts WHERE capability = ?;
