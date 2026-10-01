-- 用户、刷新令牌、协议、App 版本、会员状态（T06）。
-- 规矩：查询用户内容一律带归属条件（CLAUDE.md 必须遵守第 4 条）；这里的表以 user_id / id 为归属。

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByPhone :one
SELECT * FROM users WHERE phone = ?;

-- name: CreateUser :execlastid
INSERT INTO users (phone, invite_code) VALUES (?, ?);

-- name: UpdateUserNickname :exec
UPDATE users SET nickname = ? WHERE id = ?;

-- name: UpdateUserOnboarding :exec
UPDATE users SET onboarding_step = ? WHERE id = ?;

-- name: UpdateUserPhone :exec
UPDATE users SET phone = ? WHERE id = ?;

-- name: TouchUserActive :exec
UPDATE users SET last_active_at = ? WHERE id = ?;

-- name: RequestUserDeletion :exec
UPDATE users SET status = 'deleting', deletion_requested_at = ?, deletion_due_at = ? WHERE id = ? AND status = 'active';

-- name: CancelUserDeletion :execrows
UPDATE users SET status = 'active', deletion_requested_at = NULL, deletion_due_at = NULL WHERE id = ? AND status = 'deleting';

-- name: ListUsersDueForDeletion :many
SELECT id FROM users WHERE status = 'deleting' AND deletion_due_at <= ? ORDER BY id LIMIT ?;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = ? AND status = 'deleting';

-- name: InsertRefreshToken :exec
INSERT INTO refresh_tokens (user_id, device_id, device_name, platform, token_hash, expires_at, last_used_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = ?;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL;

-- name: RevokeDeviceTokens :execrows
UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND device_id = ? AND revoked_at IS NULL;

-- name: ListUserActiveTokens :many
-- 登录设备列表：同一设备可能有多条有效令牌（并发刷新），由调用方按 device_id 去重。
SELECT device_id, device_name, platform, COALESCE(last_used_at, created_at) AS last_used_at
FROM refresh_tokens
WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?
ORDER BY last_used_at DESC;

-- name: GetLatestAgreement :one
SELECT * FROM agreements
WHERE kind = ? AND published_at IS NOT NULL AND published_at <= ?
ORDER BY effective_at DESC, id DESC LIMIT 1;

-- name: ListLatestAgreements :many
SELECT a.* FROM agreements a
JOIN (
  SELECT a2.kind, MAX(a2.effective_at) AS effective_at FROM agreements a2
  WHERE a2.published_at IS NOT NULL AND a2.published_at <= ? GROUP BY a2.kind
) latest ON latest.kind = a.kind AND latest.effective_at = a.effective_at
WHERE a.published_at IS NOT NULL;

-- name: ListAcceptedAgreementIDs :many
SELECT agreement_id FROM agreement_acceptances WHERE user_id = ?;

-- name: AcceptAgreement :exec
INSERT IGNORE INTO agreement_acceptances (user_id, agreement_id) VALUES (?, ?);

-- name: GetPublishedAgreementByID :one
SELECT * FROM agreements WHERE id = ? AND published_at IS NOT NULL;

-- name: GetAppVersion :one
SELECT * FROM app_versions WHERE platform = ?;

-- name: GetCurrentMembership :one
-- 覆盖此刻、未收回的会员时段。
SELECT * FROM memberships
WHERE owner_user_id = ? AND revoked_at IS NULL AND starts_at <= ? AND ends_at > ?
ORDER BY ends_at DESC LIMIT 1;

-- name: GetLatestMembershipEnd :one
-- 时长叠加后的最晚结束时间（会员条上显示「有效期至」）。
SELECT * FROM memberships
WHERE owner_user_id = ? AND revoked_at IS NULL AND ends_at > ?
ORDER BY ends_at DESC LIMIT 1;

-- name: RevokeAllUserTokens :exec
UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL;
