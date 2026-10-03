-- 管理后台：配置与系统（T29：7.8、7.9、7.15）。这里只有配置表，不涉及用户内容。

-- name: ListRuleParamsFull :many
SELECT param_key, value, description, version, updated_by, updated_at FROM rule_params ORDER BY param_key;

-- name: GetRuleParam :one
SELECT param_key, value, description, version, updated_by, updated_at FROM rule_params WHERE param_key = ?;

-- name: UpdateRuleParam :execrows
-- 乐观锁：version 不一致说明别人刚改过，返回 0 行。
UPDATE rule_params SET value = ?, version = version + 1, updated_by = ? WHERE param_key = ? AND version = ?;

-- name: ListAllAgreements :many
SELECT id, kind, version, title, change_summary, effective_at, published_at, created_at FROM agreements ORDER BY kind, id DESC;

-- name: GetAgreement :one
SELECT * FROM agreements WHERE id = ?;

-- name: InsertAgreement :execlastid
INSERT INTO agreements (kind, version, title, body, change_summary, effective_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateAgreementDraft :execrows
UPDATE agreements SET title = ?, body = ?, change_summary = ?, effective_at = ? WHERE id = ? AND published_at IS NULL;

-- name: PublishAgreement :execrows
UPDATE agreements SET published_at = ? WHERE id = ? AND published_at IS NULL;

-- name: ListExamDates :many
SELECT * FROM exam_dates ORDER BY exam_year;

-- name: UpsertExamDate :exec
INSERT INTO exam_dates (exam_year, label, first_exam_start, first_exam_end, subject_exam_date) VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE label = VALUES(label), first_exam_start = VALUES(first_exam_start), first_exam_end = VALUES(first_exam_end), subject_exam_date = VALUES(subject_exam_date);

-- name: ListFeatureFlagsFull :many
SELECT f.flag_key, f.description, f.enabled_for_all, f.updated_at,
  CAST(COALESCE((SELECT GROUP_CONCAT(u.user_id ORDER BY u.user_id) FROM feature_flag_users u WHERE u.flag_key = f.flag_key), '') AS CHAR) AS user_ids
FROM feature_flags f ORDER BY f.flag_key;

-- name: UpdateFeatureFlag :execrows
UPDATE feature_flags SET enabled_for_all = ?, updated_by = ? WHERE flag_key = ?;

-- name: DeleteFlagUsers :exec
DELETE FROM feature_flag_users WHERE flag_key = ?;

-- name: InsertFlagUser :exec
INSERT IGNORE INTO feature_flag_users (flag_key, user_id) SELECT ?, id FROM users WHERE id = ?;

-- name: ListAppVersions :many
SELECT * FROM app_versions ORDER BY platform;

-- name: UpsertAppVersion :exec
INSERT INTO app_versions (platform, latest_version, min_version, download_url, release_notes) VALUES (?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE latest_version = VALUES(latest_version), min_version = VALUES(min_version), download_url = VALUES(download_url), release_notes = VALUES(release_notes);

-- name: ListAIRollouts :many
SELECT * FROM ai_rollouts ORDER BY capability;

-- name: UpsertAIRollout :exec
INSERT INTO ai_rollouts (capability, stable_model, stable_prompt, candidate_model, candidate_prompt, candidate_percent, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE stable_model = VALUES(stable_model), stable_prompt = VALUES(stable_prompt), candidate_model = VALUES(candidate_model),
  candidate_prompt = VALUES(candidate_prompt), candidate_percent = VALUES(candidate_percent), updated_by = VALUES(updated_by);

-- name: AIStatsByVersion :many
-- 7.8 AI 任务：按能力与「模型 + 提示词版本」统计调用次数、成功率、单次成本、耗时（ai_calls 不存输入输出原文）。
SELECT capability, model, prompt_version, COUNT(*) AS calls, CAST(SUM(success) AS SIGNED) AS ok,
  CAST(COALESCE(SUM(cost_micro_yuan), 0) AS SIGNED) AS cost, CAST(COALESCE(AVG(latency_ms), 0) AS SIGNED) AS latency
FROM ai_calls WHERE created_at >= ? GROUP BY capability, model, prompt_version;

-- name: AICostSince :one
SELECT CAST(COALESCE(SUM(cost_micro_yuan), 0) AS SIGNED) FROM ai_calls WHERE created_at >= ?;

-- name: SumStat :one
-- 聚合表里某指标在一段时间的和（如活跃用户日累计、收入）。
SELECT CAST(COALESCE(SUM(value), 0) AS DECIMAL(18,4)) FROM stats_hourly WHERE metric = ? AND dimension = '' AND bucket >= ? AND bucket <= ?;

-- name: ListAnnouncements :many
SELECT * FROM announcements ORDER BY id DESC LIMIT 100;

-- name: InsertAnnouncement :execlastid
INSERT INTO announcements (title, body, audience, with_popup, scheduled_at, created_by) VALUES (?, ?, ?, ?, ?, ?);

-- name: DeleteUnsentAnnouncement :execrows
DELETE FROM announcements WHERE id = ? AND sent_at IS NULL;

-- name: ListAdmins :many
SELECT id, username, display_name, phone, roles, status, must_change_password, last_login_at, created_at FROM admin_users ORDER BY id;

-- name: UpdateAdmin :execrows
UPDATE admin_users SET display_name = ?, phone = ?, roles = ?, status = ? WHERE id = ?;

-- name: ResetAdminPassword :exec
UPDATE admin_users SET password_hash = ?, must_change_password = 1 WHERE id = ?;

-- name: DeleteAdminSessionsFor :exec
DELETE FROM admin_sessions WHERE admin_id = ?;
