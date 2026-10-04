-- 今日计划与首页（T16）。每条查询都带 owner_user_id。

-- name: ListPlanUsers :many
-- 每天 0 点给这些用户生成计划：账号正常、建了备考档案、至少有一门专业课。
SELECT u.id FROM users u JOIN study_profiles p ON p.user_id = u.id
WHERE u.status = 'active' AND u.id > ? AND EXISTS (SELECT 1 FROM subjects s WHERE s.owner_user_id = u.id)
ORDER BY u.id LIMIT ?;

-- name: GetDailyPlan :one
SELECT * FROM daily_plans WHERE owner_user_id = ? AND plan_date = ?;

-- name: UpsertDailyPlan :exec
INSERT INTO daily_plans (owner_user_id, plan_date, stage, budget_minutes, plan_groups, generated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE stage = VALUES(stage), budget_minutes = VALUES(budget_minutes), plan_groups = VALUES(plan_groups),
  generated_at = VALUES(generated_at), completed_at = NULL;

-- name: SetDailyPlanCompleted :exec
UPDATE daily_plans SET completed_at = ? WHERE owner_user_id = ? AND plan_date = ? AND completed_at IS NULL;

-- name: ListKPMasteryRows :many
SELECT kp_id, m, state, answered, last_self_assess, next_review_on, recite_next_review_on, recite_interval_step
FROM kp_mastery WHERE owner_user_id = ?;

-- name: ListWrongBookDue :many
-- 到期复习的错题：没排过复习日的、复习日已到的。
SELECT question_id FROM wrong_book
WHERE owner_user_id = ? AND status = 'active' AND (next_review_on IS NULL OR next_review_on <= ?);

-- name: ListAttemptedSince :many
SELECT DISTINCT question_id FROM attempts WHERE owner_user_id = ? AND answered_at >= ?;

-- name: ListRecitedSince :many
SELECT DISTINCT kp_id FROM recite_records WHERE owner_user_id = ? AND created_at >= ?;

-- name: ListActivityDays :many
-- 有作答或背诵的北京时间日期（连续打卡）。
SELECT d.day FROM (
  SELECT CAST(DATE(DATE_ADD(a.answered_at, INTERVAL 8 HOUR)) AS CHAR) AS day FROM attempts a WHERE a.owner_user_id = sqlc.arg(user_id) AND a.answered_at >= sqlc.arg(since)
  UNION
  SELECT CAST(DATE(DATE_ADD(r.created_at, INTERVAL 8 HOUR)) AS CHAR) AS day FROM recite_records r WHERE r.owner_user_id = sqlc.arg(user_id) AND r.created_at >= sqlc.arg(since)
) d ORDER BY d.day DESC;

-- name: ListRecentKPAttempts :many
-- 「以为会了」（PRD 11.2）：自评掌握的知识点近期作答情况。
SELECT qk.kp_id, a.is_correct, a.score, a.full_score, q.qtype
FROM attempts a JOIN question_kps qk ON qk.question_id = a.question_id JOIN questions q ON q.id = a.question_id
WHERE a.owner_user_id = ? AND a.answered_at >= ? AND a.answer_mode <> 'self_assess'
ORDER BY a.answered_at;

-- name: ListAttemptsSince :many
SELECT a.id, a.question_id, a.is_correct, a.score, a.full_score, a.duration_seconds, q.qtype
FROM attempts a JOIN questions q ON q.id = a.question_id
WHERE a.owner_user_id = ? AND a.answered_at >= ?;

-- name: ListGradingLossSince :many
SELECT g.loss FROM gradings g WHERE g.owner_user_id = ? AND g.status = 'done' AND g.created_at >= ? AND g.loss IS NOT NULL;

-- name: CountMasteredSince :one
SELECT COUNT(*) FROM kp_mastery WHERE owner_user_id = ? AND state = 'mastered' AND updated_at >= ?;

-- name: AnswerStagePrompt :exec
-- 2.1e：接受时立即切换阶段（不是手动选择）；不管接受与否，同一目标阶段只弹一次。
UPDATE study_profiles SET stage_prompted = sqlc.arg(target),
  stage = IF(sqlc.arg(accept), sqlc.arg(target), stage),
  stage_manual = IF(sqlc.arg(accept), 0, stage_manual)
WHERE user_id = sqlc.arg(user_id);

-- name: ListActiveImportJobs :many
SELECT j.id, j.status, b.subject_id,
  (SELECT COUNT(*) FROM import_items i WHERE i.job_id = j.id AND i.status <> 'deleted') AS recognized
FROM import_jobs j JOIN banks b ON b.id = j.bank_id
WHERE j.owner_user_id = ? AND j.status IN ('queued', 'running', 'reviewing')
ORDER BY j.id DESC;
