-- 管理后台（T28，PRD 10、dev-spec 第十节）。这里的查询只读计数、状态与元数据，不读用户资料、题目、作答的原文；
-- 原文只能经 content_access_grants 授权、由 notify.RecordAccess 记日志后在 admincontent.sql 里读。

-- name: GetAdminByUsername :one
SELECT * FROM admin_users WHERE username = ?;

-- name: GetAdminByID :one
SELECT * FROM admin_users WHERE id = ?;

-- name: InsertAdmin :execlastid
INSERT INTO admin_users (username, display_name, password_hash, phone, roles, must_change_password) VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateAdminPassword :exec
UPDATE admin_users SET password_hash = ?, must_change_password = 0 WHERE id = ?;

-- name: TouchAdminLogin :exec
UPDATE admin_users SET last_login_at = ? WHERE id = ?;

-- name: InsertAdminSession :exec
INSERT INTO admin_sessions (token_hash, admin_id, expires_at, created_at) VALUES (?, ?, ?, ?);

-- name: GetAdminSession :one
SELECT a.id, a.username, a.display_name, a.roles, a.must_change_password, s.expires_at FROM admin_sessions s JOIN admin_users a ON a.id = s.admin_id
WHERE s.token_hash = ? AND s.expires_at > ? AND a.status = 'active';

-- name: DeleteAdminSession :exec
DELETE FROM admin_sessions WHERE token_hash = ?;

-- name: DeleteExpiredAdminSessions :exec
DELETE FROM admin_sessions WHERE expires_at < ?;

-- name: InsertAdminAudit :exec
INSERT INTO admin_audit_logs (admin_id, action, target_type, target_id, detail, ip, created_at) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListAdminAudit :many
SELECT l.id, l.admin_id, a.display_name, l.action, l.target_type, l.target_id, l.detail, l.created_at
FROM admin_audit_logs l LEFT JOIN admin_users a ON a.id = l.admin_id
WHERE (CAST(sqlc.arg(target_type) AS CHAR) = '' OR (l.target_type = CAST(sqlc.arg(target_type) AS CHAR) AND l.target_id = CAST(sqlc.arg(target_id) AS CHAR)))
  AND (CAST(sqlc.arg(before_id) AS UNSIGNED) = 0 OR l.id < CAST(sqlc.arg(before_id) AS UNSIGNED))
ORDER BY l.id DESC LIMIT 50;

-- name: ListContentAccessLogs :many
SELECT l.id, l.grant_id, l.admin_id, a.display_name, l.user_id, l.target_type, l.target_id, l.created_at
FROM content_access_logs l LEFT JOIN admin_users a ON a.id = l.admin_id
WHERE (CAST(sqlc.arg(user_id) AS UNSIGNED) = 0 OR l.user_id = CAST(sqlc.arg(user_id) AS UNSIGNED)) ORDER BY l.id DESC LIMIT 100;

-- ---------- 7.2 用户 ----------

-- name: AdminSearchUsers :many
-- 搜索手机号、用户 ID、邀请码；筛选：付费、额度用完、解析失败、未导入资料。只返回计数与状态。
SELECT u.id, u.phone, u.invite_code, u.status, u.created_at, u.last_active_at,
  (SELECT COUNT(*) FROM materials m WHERE m.owner_user_id = u.id) AS material_count,
  (SELECT COUNT(*) FROM questions q WHERE q.owner_user_id = u.id AND q.status = 'active') AS question_count,
  (SELECT GROUP_CONCAT(COALESCE(NULLIF(s.code, ''), s.name) ORDER BY s.sort_order SEPARATOR ' · ') FROM subjects s WHERE s.owner_user_id = u.id) AS subjects,
  EXISTS (SELECT 1 FROM memberships ms WHERE ms.owner_user_id = u.id AND ms.revoked_at IS NULL AND ms.starts_at <= CAST(sqlc.arg(now) AS DATETIME) AND ms.ends_at > CAST(sqlc.arg(now) AS DATETIME)) AS is_member
FROM users u
WHERE (CAST(sqlc.arg(q) AS CHAR) = '' OR u.phone = CAST(sqlc.arg(q) AS CHAR) OR u.invite_code = CAST(sqlc.arg(q) AS CHAR) OR u.id = CAST(sqlc.arg(user_id) AS UNSIGNED))
  AND (CAST(sqlc.arg(filter) AS CHAR) = ''
    OR (CAST(sqlc.arg(filter) AS CHAR) = 'paid' AND EXISTS (SELECT 1 FROM orders o WHERE o.owner_user_id = u.id AND o.status = 'paid'))
    OR (CAST(sqlc.arg(filter) AS CHAR) = 'quota_out' AND EXISTS (SELECT 1 FROM quota_counters c WHERE c.owner_user_id = u.id AND c.quota_type = 'parse_pages' AND c.period_key = 'total' AND c.used + c.reserved >= CAST(sqlc.arg(free_pages) AS SIGNED) + c.bonus))
    OR (CAST(sqlc.arg(filter) AS CHAR) = 'parse_failed' AND EXISTS (SELECT 1 FROM materials m WHERE m.owner_user_id = u.id AND m.status IN ('failed', 'partial')))
    OR (CAST(sqlc.arg(filter) AS CHAR) = 'no_material' AND NOT EXISTS (SELECT 1 FROM materials m WHERE m.owner_user_id = u.id)))
  AND (CAST(sqlc.arg(before_id) AS UNSIGNED) = 0 OR u.id < CAST(sqlc.arg(before_id) AS UNSIGNED))
ORDER BY u.id DESC LIMIT 50;

-- name: AdminGetUser :one
SELECT u.id, u.phone, u.invite_code, u.status, u.created_at, u.last_active_at, u.nickname FROM users u WHERE u.id = ?;

-- name: AdminUserCounts :one
SELECT
  (SELECT COUNT(*) FROM materials m WHERE m.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED)) AS materials,
  (SELECT COUNT(*) FROM materials m WHERE m.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND m.status IN ('failed', 'partial')) AS failed_materials,
  (SELECT CAST(COALESCE(SUM(m.page_count), 0) AS SIGNED) FROM materials m WHERE m.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED)) AS pages,
  (SELECT COUNT(*) FROM questions q WHERE q.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND q.status = 'active') AS questions,
  (SELECT COUNT(*) FROM knowledge_points k WHERE k.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED)) AS knowledge_points,
  (SELECT COUNT(*) FROM paper_sessions p WHERE p.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND p.status = 'graded') AS papers,
  (SELECT COUNT(*) FROM gradings g WHERE g.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND g.status = 'done' AND g.created_at >= CAST(sqlc.arg(week_start) AS DATETIME)) AS week_gradings,
  (SELECT COUNT(*) FROM invites i WHERE i.inviter_id = CAST(sqlc.arg(user_id) AS UNSIGNED)) AS invited,
  (SELECT CAST(COALESCE(SUM(i.inviter_days), 0) AS SIGNED) FROM invites i WHERE i.inviter_id = CAST(sqlc.arg(user_id) AS UNSIGNED)) AS invite_days;

-- name: AdminUserSubjects :many
-- 专业课名称与代码（用户填的元数据）、满分、目标分与最新预估区间（数字）。
SELECT s.id, s.name, s.code, s.full_score, s.target_score,
  CAST(IFNULL((SELECT e.low FROM score_estimates e WHERE e.subject_id = s.id AND e.owner_user_id = s.owner_user_id ORDER BY e.id DESC LIMIT 1), -1) AS SIGNED) AS est_low,
  CAST(IFNULL((SELECT e.high FROM score_estimates e WHERE e.subject_id = s.id AND e.owner_user_id = s.owner_user_id ORDER BY e.id DESC LIMIT 1), -1) AS SIGNED) AS est_high
FROM subjects s WHERE s.owner_user_id = ? ORDER BY s.sort_order;

-- name: AdminSetUserStatus :execrows
UPDATE users SET status = ? WHERE id = ? AND status <> 'deleting';

-- name: AdminRevokeUserTokens :exec
UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL;

-- name: AdminFailedMaterials :many
-- 解析失败或部分失败的资料（只取 ID，用于重新解析）。
SELECT id FROM materials WHERE owner_user_id = ? AND status IN ('failed', 'partial') ORDER BY id DESC LIMIT 20;

-- name: AdminGetUserPhone :one
SELECT phone FROM users WHERE id = ?;

-- ---------- 7.3 会员与订单 ----------

-- name: AdminListOrders :many
SELECT o.id, o.order_no, o.owner_user_id, o.tier, o.channel, o.amount_cents, o.status, o.paid_at, o.created_at,
  CAST(IFNULL((SELECT r.status FROM refunds r WHERE r.order_id = o.id ORDER BY r.id DESC LIMIT 1), '') AS CHAR) AS refund_status
FROM orders o
WHERE (CAST(sqlc.arg(status) AS CHAR) = '' OR o.status = CAST(sqlc.arg(status) AS CHAR)) AND (CAST(sqlc.arg(user_id) AS UNSIGNED) = 0 OR o.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED))
  AND (CAST(sqlc.arg(before_id) AS UNSIGNED) = 0 OR o.id < CAST(sqlc.arg(before_id) AS UNSIGNED))
ORDER BY o.id DESC LIMIT 50;

-- name: AdminOrderSummary :one
SELECT
  (SELECT CAST(COALESCE(SUM(amount_cents), 0) AS SIGNED) FROM orders WHERE status IN ('paid', 'refunding') AND paid_at >= CAST(sqlc.arg(month_start) AS DATETIME)) AS month_revenue,
  (SELECT COUNT(DISTINCT owner_user_id) FROM orders WHERE status IN ('paid', 'refunding')) AS paid_users,
  (SELECT COUNT(DISTINCT owner_user_id) FROM materials) AS imported_users,
  (SELECT COUNT(*) FROM refunds WHERE status = 'pending') AS pending_refunds;

-- name: AdminSalesByTier :many
SELECT tier, COUNT(*) AS orders, CAST(COALESCE(SUM(amount_cents), 0) AS SIGNED) AS revenue FROM orders
WHERE status IN ('paid', 'refunding') AND paid_at >= ? GROUP BY tier;

-- name: AdminOrderUsage :one
-- 退款处理时显示用户使用情况（PRD 13.3）：开通后解析页数、批改次数、作答题数。
SELECT
  (SELECT CAST(COALESCE(SUM(m.page_count), 0) AS SIGNED) FROM materials m WHERE m.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND m.created_at >= CAST(sqlc.arg(since) AS DATETIME)) AS pages,
  (SELECT COUNT(*) FROM gradings g WHERE g.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND g.created_at >= CAST(sqlc.arg(since) AS DATETIME)) AS gradings,
  (SELECT COUNT(*) FROM attempts a WHERE a.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND a.answered_at >= CAST(sqlc.arg(since) AS DATETIME)) AS answers,
  (SELECT COUNT(*) FROM materials m WHERE m.owner_user_id = CAST(sqlc.arg(user_id) AS UNSIGNED) AND m.status IN ('failed', 'partial')) AS failed_materials;

-- ---------- 7.4 兑换码 ----------

-- name: AdminRedeemSummary :one
SELECT
  (SELECT COUNT(*) FROM redeem_codes) AS generated_count,
  (SELECT COUNT(*) FROM redeem_codes WHERE status = 'used') AS used_count,
  (SELECT COUNT(*) FROM redeem_codes c JOIN redeem_batches b ON b.id = c.batch_id WHERE c.status = 'unused' AND b.status = 'active' AND b.code_expires_at > CAST(sqlc.arg(now) AS DATETIME)) AS available_count,
  (SELECT COUNT(*) FROM redeem_codes c JOIN redeem_batches b ON b.id = c.batch_id WHERE c.status = 'void' OR (c.status = 'unused' AND (b.status = 'disabled' OR b.code_expires_at <= CAST(sqlc.arg(now) AS DATETIME)))) AS inactive_count;

-- name: AdminListBatches :many
SELECT b.*, (SELECT COUNT(*) FROM redeem_codes c WHERE c.batch_id = b.id AND c.status = 'used') AS used_count
FROM redeem_batches b WHERE (CAST(sqlc.arg(before_id) AS UNSIGNED) = 0 OR b.id < CAST(sqlc.arg(before_id) AS UNSIGNED)) ORDER BY b.id DESC LIMIT 50;

-- name: AdminGetBatch :one
SELECT * FROM redeem_batches WHERE id = ?;

-- name: AdminInsertBatch :execlastid
INSERT INTO redeem_batches (name, tier, days, quantity, code_expires_at, channel, created_by) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: AdminInsertCode :exec
INSERT INTO redeem_codes (batch_id, code_hash, code_tail) VALUES (?, ?, ?);

-- name: AdminDisableBatch :execrows
UPDATE redeem_batches SET status = 'disabled' WHERE id = ? AND status = 'active';

-- name: AdminBatchCodes :many
-- 批次明细：码只存哈希，明细显示末 3 位、状态、使用人与时间。
SELECT id, code_tail, status, used_by, used_at FROM redeem_codes WHERE batch_id = ? ORDER BY id LIMIT 2000;

-- name: AdminFindCode :one
SELECT c.id, c.batch_id, c.code_tail, c.status, c.used_by, c.used_at, c.membership_id, b.name AS batch_name, b.tier, b.days, b.code_expires_at, b.status AS batch_status
FROM redeem_codes c JOIN redeem_batches b ON b.id = c.batch_id WHERE c.code_hash = ?;

-- name: AdminGetCodeForUpdate :one
SELECT id, status, used_by, membership_id FROM redeem_codes WHERE id = ? FOR UPDATE;

-- name: AdminVoidCode :execrows
UPDATE redeem_codes SET status = 'void' WHERE id = ? AND status <> 'void';

-- ---------- 7.5 资料解析监控 ----------

-- name: AdminParseByFormat :many
-- 近一段时间按格式的解析结果与页数（扫描件单列：PDF 页里没有文字层的记为 image 格式之外的 scanned，见 material.format）。
SELECT m.format, m.status, COUNT(*) AS files, CAST(COALESCE(SUM(m.page_count), 0) AS SIGNED) AS pages
FROM materials m WHERE m.created_at >= ? AND m.status IN ('parsed', 'partial', 'failed') GROUP BY m.format, m.status;

-- name: AdminImportDurations :many
SELECT TIMESTAMPDIFF(SECOND, started_at, finished_at) AS seconds FROM import_jobs
WHERE finished_at IS NOT NULL AND started_at IS NOT NULL AND created_at >= ? ORDER BY id DESC LIMIT 2000;

-- name: AdminListImportJobs :many
-- 失败与部分成功的任务。
SELECT j.id, j.owner_user_id, j.mode, j.status, j.created_at, j.finished_at, j.billed_pages,
  (SELECT COUNT(*) FROM import_job_materials x WHERE x.job_id = j.id) AS files,
  (SELECT COUNT(*) FROM import_job_materials x WHERE x.job_id = j.id AND x.status = 'failed') AS failed_files,
  (SELECT COUNT(*) FROM import_job_materials x WHERE x.job_id = j.id AND x.status = 'partial') AS partial_files
FROM import_jobs j
WHERE EXISTS (SELECT 1 FROM import_job_materials x WHERE x.job_id = j.id AND x.status IN ('failed', 'partial'))
  AND (CAST(sqlc.arg(before_id) AS UNSIGNED) = 0 OR j.id < CAST(sqlc.arg(before_id) AS UNSIGNED))
ORDER BY j.id DESC LIMIT 50;

-- name: AdminGetImportJob :one
SELECT id, owner_user_id, mode, status, created_at, started_at, finished_at, billed_pages, reserved_pages, prompt_versions FROM import_jobs WHERE id = ?;

-- name: AdminImportJobFiles :many
-- 任务详情只显示格式、页数、识别日志与失败环节，不显示文件名和内容（PRD 10.2 7.5）。
SELECT x.material_id, m.format, m.page_count, m.status AS material_status, x.step, x.status, x.attempts, x.fail_reason, x.failed_pages, x.updated_at
FROM import_job_materials x JOIN materials m ON m.id = x.material_id WHERE x.job_id = ? ORDER BY x.material_id;

-- ---------- 7.6 批改异议 ----------

-- name: AdminDisputeStats :one
SELECT
  (SELECT COUNT(*) FROM disputes WHERE created_at >= CAST(sqlc.arg(since) AS DATETIME)) AS disputes,
  (SELECT COUNT(*) FROM gradings WHERE created_at >= CAST(sqlc.arg(since) AS DATETIME) AND trigger_reason IN ('submit', 'pending_resubmit') AND status = 'done') AS gradings,
  (SELECT COUNT(*) FROM disputes WHERE created_at >= CAST(sqlc.arg(since) AS DATETIME) AND score_after IS NOT NULL AND score_after <> score_before) AS changed,
  (SELECT COUNT(*) FROM disputes WHERE created_at >= CAST(sqlc.arg(since) AS DATETIME) AND score_after IS NOT NULL) AS rechecked,
  (SELECT CAST(COALESCE(AVG(TIMESTAMPDIFF(SECOND, created_at, resolved_at)), 0) AS SIGNED) FROM disputes WHERE created_at >= CAST(sqlc.arg(since) AS DATETIME) AND resolved_at IS NOT NULL) AS avg_seconds;

-- name: AdminDisputeReasons :many
SELECT reason, COUNT(*) AS n FROM disputes WHERE created_at >= CAST(sqlc.arg(since) AS DATETIME) GROUP BY reason;

-- name: AdminListDisputes :many
SELECT d.id, d.owner_user_id, d.reason, d.allow_access, d.score_before, d.score_after, d.status, d.attribution, d.created_at, d.resolved_at,
  CAST(IFNULL((SELECT g.id FROM content_access_grants g WHERE g.source = 'dispute' AND g.source_id = d.id AND g.revoked_at IS NULL AND g.expires_at > CAST(sqlc.arg(now) AS DATETIME)), 0) AS UNSIGNED) AS grant_id
FROM disputes d
WHERE (CAST(sqlc.arg(status) AS CHAR) = '' OR d.status = CAST(sqlc.arg(status) AS CHAR)) AND (CAST(sqlc.arg(before_id) AS UNSIGNED) = 0 OR d.id < CAST(sqlc.arg(before_id) AS UNSIGNED))
ORDER BY d.id DESC LIMIT 50;

-- name: AdminGetDispute :one
SELECT d.id, d.owner_user_id, d.grading_id, d.regrade_id, d.reason, d.allow_access, d.score_before, d.score_after, d.status, d.attribution, d.created_at, d.resolved_at
FROM disputes d WHERE d.id = ?;

-- name: AdminSetDisputeAttribution :execrows
UPDATE disputes SET attribution = ?, status = ?, handled_by = ?, resolved_at = COALESCE(resolved_at, ?) WHERE id = ?;

-- name: GetGrantBySource :one
SELECT * FROM content_access_grants WHERE source = ? AND source_id = ? AND revoked_at IS NULL AND expires_at > ?;

-- ---------- 7.7 用户反馈 ----------

-- name: AdminFeedbackStats :one
SELECT
  (SELECT COUNT(*) FROM feedbacks WHERE status = 'open') AS open_count,
  (SELECT CAST(COALESCE(AVG(TIMESTAMPDIFF(SECOND, created_at, replied_at)), 0) AS SIGNED) FROM feedbacks WHERE replied_at IS NOT NULL AND created_at >= CAST(sqlc.arg(since) AS DATETIME)) AS avg_reply_seconds,
  (SELECT CAST(COALESCE(AVG(satisfaction), 0) * 100 AS SIGNED) FROM feedbacks WHERE satisfaction IS NOT NULL AND created_at >= CAST(sqlc.arg(since) AS DATETIME)) AS satisfaction_x100;

-- name: AdminFeedbackTypes :many
SELECT ftype, COUNT(*) AS n FROM feedbacks WHERE created_at >= CAST(sqlc.arg(since) AS DATETIME) GROUP BY ftype ORDER BY n DESC;

-- name: AdminListFeedbacks :many
-- 反馈是用户写给客服的，列表显示内容；截图与关联资料要授权才能看。
SELECT f.id, f.owner_user_id, f.ftype, f.content, f.allow_access, f.status, f.created_at, f.replied_at, f.related_material_id
FROM feedbacks f
WHERE (CAST(sqlc.arg(status) AS CHAR) = '' OR f.status = CAST(sqlc.arg(status) AS CHAR)) AND (CAST(sqlc.arg(before_id) AS UNSIGNED) = 0 OR f.id < CAST(sqlc.arg(before_id) AS UNSIGNED))
ORDER BY f.id DESC LIMIT 50;

-- name: AdminGetFeedback :one
SELECT f.id, f.owner_user_id, f.ftype, f.content, f.allow_access, f.status, f.reply, f.replied_at, f.created_at, f.related_material_id, f.screenshot_keys
FROM feedbacks f WHERE f.id = ?;

-- name: AdminMaterialJob :one
-- 反馈关联资料的最近一次解析任务（只取 ID）。
SELECT x.job_id FROM import_job_materials x WHERE x.material_id = ? ORDER BY x.job_id DESC LIMIT 1;

-- ---------- 统计汇总（Worker 每小时写 stats_hourly，7.1 只读它） ----------

-- name: UpsertStat :exec
INSERT INTO stats_hourly (metric, dimension, bucket, value) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE value = VALUES(value);

-- name: ListStats :many
SELECT metric, dimension, bucket, value FROM stats_hourly WHERE bucket >= ? AND bucket <= ? ORDER BY bucket;

-- name: StatDay :one
-- 某天（北京时间，[from, to)）的新增、活跃、作答、解析、付费与收入。
SELECT
  (SELECT COUNT(*) FROM users WHERE created_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND created_at < CAST(sqlc.arg(to_t) AS DATETIME)) AS new_users,
  (SELECT COUNT(*) FROM users WHERE last_active_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND last_active_at < CAST(sqlc.arg(to_t) AS DATETIME)) AS active_users,
  (SELECT COUNT(*) FROM attempts WHERE answered_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND answered_at < CAST(sqlc.arg(to_t) AS DATETIME)) AS answers,
  (SELECT CAST(COALESCE(SUM(page_count), 0) AS SIGNED) FROM materials WHERE created_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND created_at < CAST(sqlc.arg(to_t) AS DATETIME) AND status IN ('parsed', 'partial', 'failed')) AS parse_pages,
  (SELECT COUNT(*) FROM materials WHERE created_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND created_at < CAST(sqlc.arg(to_t) AS DATETIME) AND status = 'parsed') AS parse_ok,
  (SELECT COUNT(*) FROM materials WHERE created_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND created_at < CAST(sqlc.arg(to_t) AS DATETIME) AND status IN ('parsed', 'partial', 'failed')) AS parse_total,
  (SELECT COUNT(DISTINCT owner_user_id) FROM orders WHERE paid_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND paid_at < CAST(sqlc.arg(to_t) AS DATETIME)) AS paying_users,
  (SELECT CAST(COALESCE(SUM(amount_cents), 0) AS SIGNED) FROM orders WHERE paid_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND paid_at < CAST(sqlc.arg(to_t) AS DATETIME) AND status IN ('paid', 'refunding')) AS revenue_cents,
  (SELECT COUNT(*) FROM disputes WHERE created_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND created_at < CAST(sqlc.arg(to_t) AS DATETIME)) AS disputes,
  (SELECT COUNT(*) FROM gradings WHERE created_at >= CAST(sqlc.arg(from_t) AS DATETIME) AND created_at < CAST(sqlc.arg(to_t) AS DATETIME) AND trigger_reason IN ('submit', 'pending_resubmit') AND status = 'done') AS gradings;

-- name: StatImportModes :many
SELECT mode, COUNT(*) AS n FROM import_jobs WHERE created_at >= ? AND created_at < ? GROUP BY mode;

-- name: StatFunnel :one
-- 新用户漏斗（全部用户）：注册 → 导入第一份资料 → 完成首次训练 → 7 日后仍在练 → 付费。
SELECT
  (SELECT COUNT(*) FROM users WHERE status <> 'deleting') AS registered,
  (SELECT COUNT(DISTINCT owner_user_id) FROM materials) AS imported,
  (SELECT COUNT(DISTINCT owner_user_id) FROM attempts) AS trained,
  (SELECT COUNT(*) FROM users u WHERE EXISTS (SELECT 1 FROM attempts a WHERE a.owner_user_id = u.id AND a.answered_at >= DATE_ADD(u.created_at, INTERVAL 7 DAY))) AS retained,
  (SELECT COUNT(DISTINCT owner_user_id) FROM orders WHERE status IN ('paid', 'refunding')) AS paid,
  (SELECT COUNT(DISTINCT owner_user_id) FROM memberships WHERE revoked_at IS NULL AND starts_at <= CAST(sqlc.arg(now) AS DATETIME) AND ends_at > CAST(sqlc.arg(now) AS DATETIME)) AS members;

-- name: StatHotSubjects :many
-- 热门专业课：按用户填的代码聚合（只有代码与人数）。
SELECT code, COUNT(DISTINCT owner_user_id) AS users FROM subjects WHERE code IS NOT NULL AND code <> '' GROUP BY code ORDER BY users DESC LIMIT 20;
