-- 邀请研友（T26，PRD 6.8、13.4）。

-- name: GetUserByInviteCode :one
SELECT id, status FROM users WHERE invite_code = ?;

-- name: InsertInvite :exec
INSERT IGNORE INTO invites (inviter_id, invitee_id, registered_at) VALUES (?, ?, ?);

-- name: GetPendingInviteForUpdate :one
-- 被邀请人第一次导入资料时激活；锁住这一行，并发确认导入只发一次奖励。
SELECT * FROM invites WHERE invitee_id = ? AND activated_at IS NULL FOR UPDATE;

-- name: LockUserRow :one
-- 锁住邀请人，两个好友同时激活时累计天数不会超过上限。
SELECT id FROM users WHERE id = ? FOR UPDATE;

-- name: SumInviterDays :one
SELECT CAST(COALESCE(SUM(inviter_days), 0) AS SIGNED) AS days FROM invites WHERE inviter_id = ?;

-- name: ActivateInvite :execrows
UPDATE invites SET activated_at = ?, inviter_days = ? WHERE id = ? AND activated_at IS NULL;

-- name: ListMyInvites :many
SELECT id, registered_at, activated_at, inviter_days FROM invites WHERE inviter_id = ? ORDER BY id DESC LIMIT 100;

-- name: CountMyInvites :one
SELECT COUNT(*) FROM invites WHERE inviter_id = ?;
