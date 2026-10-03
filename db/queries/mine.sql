-- 我的（T24）：会员时段、兑换码、考后回访、意见反馈、导出题库。每条用户内容查询都带归属条件。

-- name: InsertMembership :execlastid
INSERT INTO memberships (owner_user_id, tier, source, source_ref, starts_at, ends_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListExamEndsFrom :many
-- 冲刺卡至当年初试结束、考季卡至次年初试结束（PRD 13.2）。
SELECT * FROM exam_dates WHERE first_exam_end >= ? ORDER BY exam_year LIMIT 2;

-- name: GetRedeemCodeForUpdate :one
SELECT c.id, c.status, c.batch_id, b.tier, b.days, b.code_expires_at, b.status AS batch_status
FROM redeem_codes c JOIN redeem_batches b ON b.id = c.batch_id
WHERE c.code_hash = ? FOR UPDATE;

-- name: UseRedeemCode :execrows
UPDATE redeem_codes SET status = 'used', used_by = ?, used_at = ?, membership_id = ? WHERE id = ? AND status = 'unused';

-- name: GetSurveyResponse :one
SELECT * FROM survey_responses WHERE owner_user_id = ?;

-- name: InsertSurveyResponse :exec
INSERT INTO survey_responses (owner_user_id, exam_year, scores, retest_result, admission, share_consent, membership_id) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateSurveyResponse :exec
-- 录取结果可以稍后补（6.14）；不重复送会员。
UPDATE survey_responses SET retest_result = ?, admission = ?, share_consent = ? WHERE owner_user_id = ?;

-- name: InsertFeedback :execlastid
INSERT INTO feedbacks (owner_user_id, ftype, content, screenshot_keys, allow_access, related_material_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListFeedbacks :many
SELECT id, ftype, content, screenshot_keys, allow_access, status, reply, replied_at, created_at FROM feedbacks
WHERE owner_user_id = ? ORDER BY id DESC LIMIT 50;

-- name: InsertFeedbackGrant :exec
-- 勾选「允许客服查看相关资料」：72 小时内有效，每次查看都通知用户（PRD 6.13、10.1）。
INSERT INTO content_access_grants (user_id, source, source_id, scope, granted_at, expires_at) VALUES (?, 'feedback', ?, ?, ?, ?);

-- name: OwnsMaterial :one
SELECT EXISTS (SELECT 1 FROM materials WHERE id = ? AND owner_user_id = ?);

-- name: InsertExportJob :execlastid
INSERT INTO export_jobs (owner_user_id, subject_id, options, format, page_estimate, created_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetExportJob :one
SELECT * FROM export_jobs WHERE id = ? AND owner_user_id = ?;

-- name: SetExportJobRunning :exec
UPDATE export_jobs SET status = 'running' WHERE id = ? AND owner_user_id = ? AND status IN ('queued', 'running');

-- name: FinishExportJob :exec
UPDATE export_jobs SET status = 'done', object_key = ?, page_estimate = ?, expires_at = ?, finished_at = ? WHERE id = ? AND owner_user_id = ?;

-- name: FailExportJob :exec
UPDATE export_jobs SET status = 'failed', finished_at = ? WHERE id = ? AND owner_user_id = ?;

-- name: ListExpiredExports :many
-- 24 小时后删除 OSS 对象（定时任务）。
SELECT id, owner_user_id, object_key FROM export_jobs WHERE status = 'done' AND object_key IS NOT NULL AND expires_at < ? LIMIT 200;

-- name: ClearExportObject :exec
UPDATE export_jobs SET object_key = NULL WHERE id = ? AND owner_user_id = ?;

-- name: ExportQuestions :many
-- 导出的题目与参考答案：按题型、年份排；AI 变式题单独标。
SELECT q.id, q.qtype, q.stem, q.options, q.answer, q.score, q.source, q.exam_year
FROM questions q
WHERE q.bank_id = ? AND q.owner_user_id = ? AND q.status = 'active'
ORDER BY FIELD(q.qtype, 'single_choice', 'multi_choice', 'true_false', 'fill_blank', 'term', 'short_answer', 'discussion', 'essay', 'calculation', 'other'),
  q.exam_year DESC, q.id;

-- name: ExportRubricPoints :many
SELECT r.question_id, r.kp_id, r.seq, r.content, r.score FROM rubric_points r
JOIN questions q ON q.id = r.question_id
WHERE q.bank_id = ? AND r.owner_user_id = ?
ORDER BY r.question_id, r.seq;

-- name: ExportKnowledgePoints :many
SELECT k.id, k.parent_id, k.level, k.name, k.original_text FROM knowledge_points k
WHERE k.bank_id = ? AND k.owner_user_id = ?
ORDER BY k.sort_order, k.id;

-- name: ExportWrongBook :many
-- 错题和我的作答：最近一次作答原文与失分原因。
SELECT w.question_id, w.last_loss_type, w.wrong_count, q.qtype, q.stem, q.answer,
  (SELECT a.answer_text FROM attempts a WHERE a.question_id = w.question_id AND a.owner_user_id = w.owner_user_id ORDER BY a.answered_at DESC, a.id DESC LIMIT 1) AS my_answer
FROM wrong_book w JOIN questions q ON q.id = w.question_id
WHERE w.owner_user_id = ? AND q.bank_id = ? AND w.status = 'active'
ORDER BY w.added_at DESC;

-- name: MeOverview :one
-- 6.1 我的：资料份数、题数、错题本待重做、作文篇数。
SELECT
  (SELECT COUNT(*) FROM materials m WHERE m.owner_user_id = sqlc.arg(user_id)) AS materials,
  (SELECT COUNT(*) FROM questions q WHERE q.owner_user_id = sqlc.arg(owner) AND q.status = 'active') AS questions,
  (SELECT COUNT(*) FROM wrong_book w WHERE w.owner_user_id = sqlc.arg(user_id) AND w.status = 'active') AS wrong,
  (SELECT COUNT(*) FROM essays e WHERE e.owner_user_id = sqlc.arg(user_id) AND e.status = 'graded') AS essays;
