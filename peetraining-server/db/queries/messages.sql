-- 消息中心（T27，PRD 2.3）：保留 30 天；用户查询都带归属条件。批量发送（公告、协议更新、复习到期）用 INSERT ... SELECT，
-- dedupe_key 保证同一事件对同一用户只发一条，任务重复执行也不会重复发。

-- name: ListMessages :many
SELECT id, mtype, title, body, link, read_at, created_at FROM messages
WHERE owner_user_id = sqlc.arg(owner) AND created_at >= sqlc.arg(since) AND (sqlc.arg(before_id) = 0 OR id < sqlc.arg(before_id))
ORDER BY id DESC LIMIT ?;

-- name: CountUnreadMessages :one
SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND read_at IS NULL AND created_at >= ?;

-- name: MarkMessageRead :execrows
UPDATE messages SET read_at = ? WHERE id = ? AND owner_user_id = ? AND read_at IS NULL;

-- name: GetMessageOwned :one
SELECT id FROM messages WHERE id = ? AND owner_user_id = ?;

-- name: MarkAllMessagesRead :execrows
UPDATE messages SET read_at = ? WHERE owner_user_id = ? AND read_at IS NULL;

-- name: DeleteOldMessages :execrows
-- 系统清理任务：保留 30 天。
DELETE FROM messages WHERE created_at < ? LIMIT 5000;

-- name: ListDueAnnouncements :many
SELECT id, title, body, audience FROM announcements WHERE sent_at IS NULL AND (scheduled_at IS NULL OR scheduled_at <= ?) ORDER BY id LIMIT 20;

-- name: SendAnnouncementToAll :execrows
INSERT IGNORE INTO messages (owner_user_id, mtype, title, body, dedupe_key)
SELECT u.id, 'announcement', sqlc.arg(title), sqlc.arg(body), sqlc.arg(dedupe_key) FROM users u WHERE u.status = 'active';

-- name: SendAnnouncementToUsers :execrows
INSERT IGNORE INTO messages (owner_user_id, mtype, title, body, dedupe_key)
SELECT u.id, 'announcement', sqlc.arg(title), sqlc.arg(body), sqlc.arg(dedupe_key) FROM users u
WHERE u.status = 'active' AND JSON_CONTAINS(sqlc.arg(user_ids), CAST(u.id AS JSON));

-- name: MarkAnnouncementSent :exec
UPDATE announcements SET sent_at = ?, sent_count = ? WHERE id = ? AND sent_at IS NULL;

-- name: ListRecentAgreements :many
-- 30 天内发布的协议新版本：给还没同意的用户发「协议更新」（新注册的用户登录时已同意当前版本）。
SELECT id, kind, version, title, change_summary FROM agreements WHERE published_at IS NOT NULL AND published_at <= ? AND published_at >= ?;

-- name: SendAgreementUpdate :execrows
INSERT IGNORE INTO messages (owner_user_id, mtype, title, body, link, dedupe_key)
SELECT u.id, 'agreement_update', sqlc.arg(title), sqlc.arg(body), sqlc.arg(link), sqlc.arg(dedupe_key) FROM users u
WHERE u.status = 'active'
  AND NOT EXISTS (SELECT 1 FROM agreement_acceptances a WHERE a.user_id = u.id AND a.agreement_id = sqlc.arg(agreement_id));

-- name: SendReviewDue :execrows
-- 复习到期（北京时间当天）：打开了「复习到期提醒」、有到期知识点或错题的用户，每天一条。
INSERT IGNORE INTO messages (owner_user_id, mtype, title, body, link, dedupe_key)
SELECT p.user_id, 'review_due', '今天有内容到期复习',
  CONCAT('到期知识点 ', (SELECT COUNT(*) FROM kp_mastery k WHERE k.owner_user_id = p.user_id AND k.next_review_on <= sqlc.arg(day)),
         ' 个、到期错题 ', (SELECT COUNT(*) FROM wrong_book w WHERE w.owner_user_id = p.user_id AND w.status = 'active' AND w.next_review_on <= sqlc.arg(day)),
         ' 道，已排进今日训练'),
  sqlc.arg(link), sqlc.arg(dedupe_key)
FROM study_profiles p JOIN users u ON u.id = p.user_id
WHERE u.status = 'active' AND p.notify_review_due = 1
  AND (EXISTS (SELECT 1 FROM kp_mastery k WHERE k.owner_user_id = p.user_id AND k.next_review_on <= sqlc.arg(day))
    OR EXISTS (SELECT 1 FROM wrong_book w WHERE w.owner_user_id = p.user_id AND w.status = 'active' AND w.next_review_on <= sqlc.arg(day)));

-- name: GetActiveGrant :one
-- 后台授权查看（PRD 10.1）：只有有效期内、未撤销的授权能读。
SELECT * FROM content_access_grants WHERE id = ? AND revoked_at IS NULL AND expires_at > ?;

-- name: InsertContentAccessLog :exec
INSERT INTO content_access_logs (grant_id, admin_id, user_id, target_type, target_id, created_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: ReplyFeedback :execrows
-- 客服回复（7.7）：回复写回反馈并发到消息中心。
UPDATE feedbacks SET reply = ?, replied_by = ?, replied_at = ?, status = 'replied' WHERE id = ?;

-- name: GetFeedbackOwner :one
SELECT owner_user_id FROM feedbacks WHERE id = ?;
