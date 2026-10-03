-- 会员与支付（T25）。用户发起的查询都带归属条件；支付回调没有登录用户，按全局唯一的订单号定位（验签之后）。

-- name: InsertOrder :execlastid
INSERT INTO orders (order_no, owner_user_id, tier, channel, amount_cents, app_account_token, idempotency_key, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetOrderByIdem :one
SELECT * FROM orders WHERE owner_user_id = ? AND idempotency_key = ?;

-- name: GetMyOrder :one
SELECT * FROM orders WHERE order_no = ? AND owner_user_id = ?;

-- name: GetOrderForUpdate :one
-- 回调与退款：锁住订单行，重复回调串行执行，第二次看到已支付直接返回。
SELECT * FROM orders WHERE order_no = ? FOR UPDATE;

-- name: GetOrderByTransaction :one
SELECT * FROM orders WHERE channel = ? AND transaction_id = ?;

-- name: MarkOrderPaid :execrows
UPDATE orders SET status = 'paid', transaction_id = ?, notify_hash = ?, membership_id = ?, paid_at = ? WHERE id = ? AND status = 'created';

-- name: MarkOrderRefunded :execrows
UPDATE orders SET status = 'refunded' WHERE id = ? AND status = 'paid';

-- name: RevokeMembership :exec
UPDATE memberships SET revoked_at = ? WHERE id = ? AND owner_user_id = ? AND revoked_at IS NULL;

-- name: InsertRefund :execlastid
INSERT INTO refunds (order_id, refund_no, amount_cents, reason, status, handled_by, handled_at, created_at) VALUES (?, ?, ?, ?, 'done', ?, ?, ?);
