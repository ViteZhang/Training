-- 用户与刷新令牌。规矩：查询用户内容一律带 owner_user_id 条件（CLAUDE.md 必须遵守第 4 条）。

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: GetUserByPhone :one
SELECT * FROM users WHERE phone = ?;
