-- 额度计数与流水（T08，PRD 13.1）。扣减时先锁计数行，防止并发超额；流水的幂等键防止重复扣。

-- name: EnsureQuotaCounter :exec
INSERT IGNORE INTO quota_counters (owner_user_id, quota_type, period_key) VALUES (?, ?, ?);

-- name: LockQuotaCounter :one
SELECT * FROM quota_counters WHERE owner_user_id = ? AND quota_type = ? AND period_key = ? FOR UPDATE;

-- name: GetQuotaCounter :one
SELECT * FROM quota_counters WHERE owner_user_id = ? AND quota_type = ? AND period_key = ?;

-- name: UpdateQuotaCounter :exec
UPDATE quota_counters SET used = ?, reserved = ? WHERE owner_user_id = ? AND quota_type = ? AND period_key = ?;

-- name: InsertQuotaLedger :exec
INSERT INTO quota_ledger (owner_user_id, quota_type, period_key, action, amount, ref_type, ref_id, idempotency_key, note)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetQuotaLedgerByKey :one
SELECT * FROM quota_ledger WHERE owner_user_id = ? AND idempotency_key = ?;
