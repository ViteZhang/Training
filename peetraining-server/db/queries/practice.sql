-- 练习、作答、错题本（T17）。每条查询都带归属条件。

-- name: ListPracticePool :many
-- 选题池：题库里在用的题、主知识点、错题本状态。
SELECT q.id, q.qtype, q.source, q.exam_year,
  CAST(COALESCE((SELECT qk.kp_id FROM question_kps qk WHERE qk.question_id = q.id ORDER BY qk.is_primary DESC, qk.kp_id LIMIT 1), 0) AS UNSIGNED) AS primary_kp,
  CAST(COALESCE((SELECT COUNT(*) FROM attempts a WHERE a.question_id = q.id AND a.owner_user_id = q.owner_user_id), 0) AS UNSIGNED) AS attempt_count
FROM questions q
WHERE q.bank_id = ? AND q.owner_user_id = ? AND q.status = 'active'
ORDER BY q.id;

-- name: InsertPracticeSession :execlastid
INSERT INTO practice_sessions (owner_user_id, subject_id, kind, title, config, question_ids) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetPracticeSession :one
SELECT * FROM practice_sessions WHERE id = ? AND owner_user_id = ?;

-- name: GetPracticeSessionForUpdate :one
SELECT * FROM practice_sessions WHERE id = ? AND owner_user_id = ? FOR UPDATE;

-- name: LatestInProgressSession :one
SELECT * FROM practice_sessions
WHERE owner_user_id = ? AND subject_id = ? AND status = 'in_progress'
ORDER BY started_at DESC, id DESC LIMIT 1;

-- name: SetSessionCursor :exec
UPDATE practice_sessions SET cursor_index = ? WHERE id = ? AND owner_user_id = ?;

-- name: FinishPracticeSession :exec
UPDATE practice_sessions SET status = 'finished', summary = ?, finished_at = ? WHERE id = ? AND owner_user_id = ?;

-- name: SetDailyPlanSession :exec
UPDATE daily_plans SET practice_session_id = ? WHERE owner_user_id = ? AND plan_date = ?;

-- name: CountSessionsSince :one
SELECT COUNT(*) FROM practice_sessions
WHERE owner_user_id = ? AND subject_id = ? AND kind = ? AND status = 'finished' AND started_at >= ?;

-- name: GetAttemptByKey :one
SELECT * FROM attempts WHERE owner_user_id = ? AND idempotency_key = ?;

-- name: InsertAttempt :execlastid
INSERT INTO attempts (owner_user_id, question_id, practice_session_id, answer_mode, selected, answer_text, is_correct, score, full_score,
  revealed_answer, self_assess, duration_seconds, offline, verified_at, idempotency_key, answered_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListSessionAttempts :many
SELECT a.id, a.question_id, a.is_correct, a.revealed_answer, a.self_assess, a.selected, a.duration_seconds, q.qtype, q.source
FROM attempts a JOIN questions q ON q.id = a.question_id
WHERE a.practice_session_id = ? AND a.owner_user_id = ?
ORDER BY a.id;

-- name: CountAttempts :one
SELECT COUNT(*) FROM attempts WHERE owner_user_id = ? AND question_id = ?;

-- name: GetKPMasteryForUpdate :one
SELECT * FROM kp_mastery WHERE owner_user_id = ? AND kp_id = ? FOR UPDATE;

-- name: UpsertKPMasteryAnswer :exec
-- 作答验证后的掌握分、状态、答对日期与复习排期（PRD 11.1–11.3）。
INSERT INTO kp_mastery (owner_user_id, kp_id, m, state, answered, correct_dates, next_review_on, interval_step, decay_applied_on)
VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE m = VALUES(m), state = VALUES(state), answered = 1, correct_dates = VALUES(correct_dates),
  next_review_on = VALUES(next_review_on), interval_step = VALUES(interval_step), decay_applied_on = VALUES(decay_applied_on);

-- name: GetWrongBookForUpdate :one
SELECT * FROM wrong_book WHERE owner_user_id = ? AND question_id = ? FOR UPDATE;

-- name: UpsertWrongBook :exec
INSERT INTO wrong_book (owner_user_id, question_id, status, added_reason, last_loss_type, wrong_count, last_score_rate, correct_dates, next_review_on, added_at, removed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), added_reason = VALUES(added_reason), last_loss_type = VALUES(last_loss_type),
  wrong_count = VALUES(wrong_count), last_score_rate = VALUES(last_score_rate), correct_dates = VALUES(correct_dates),
  next_review_on = VALUES(next_review_on), added_at = VALUES(added_at), removed_at = VALUES(removed_at);

-- name: RemoveWrongBook :execrows
UPDATE wrong_book SET status = 'removed', removed_at = ? WHERE owner_user_id = ? AND question_id = ? AND status = 'active';

-- name: ListWrongBook :many
SELECT w.question_id, w.status, w.added_reason, w.last_loss_type, w.wrong_count, w.last_score_rate, w.next_review_on, w.added_at, w.removed_at,
  q.qtype, q.stem, q.source, q.exam_year,
  CAST(COALESCE((SELECT qk.kp_id FROM question_kps qk WHERE qk.question_id = q.id ORDER BY qk.is_primary DESC, qk.kp_id LIMIT 1), 0) AS UNSIGNED) AS kp_id,
  CAST(COALESCE((SELECT k.name FROM question_kps qk JOIN knowledge_points k ON k.id = qk.kp_id WHERE qk.question_id = q.id ORDER BY qk.is_primary DESC, qk.kp_id LIMIT 1), '') AS CHAR) AS kp_name
FROM wrong_book w JOIN questions q ON q.id = w.question_id
WHERE w.owner_user_id = ? AND q.bank_id = ? AND q.status = 'active'
ORDER BY w.next_review_on IS NULL, w.next_review_on, w.added_at;

-- name: QTypeCounts :many
SELECT qtype, COUNT(*) AS n FROM questions WHERE bank_id = ? AND owner_user_id = ? AND status = 'active' GROUP BY qtype;

-- name: CountReciteDue :one
-- 待背诵：知识点有采分关键词，从没背过或背诵复习日已到。
SELECT COUNT(*) FROM knowledge_points k
LEFT JOIN kp_mastery m ON m.kp_id = k.id AND m.owner_user_id = k.owner_user_id
WHERE k.bank_id = ? AND k.owner_user_id = ? AND k.level = 'point'
  AND (m.recite_next_review_on IS NULL OR m.recite_next_review_on <= ?);

-- name: UpsertQuestionReport :exec
INSERT INTO question_reports (question_id, owner_user_id, reason) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE reason = VALUES(reason), created_at = CURRENT_TIMESTAMP(3);

-- name: BumpQuestionReport :exec
-- 每次报错计数；AI 出的题累计 3 次自动下线（PRD 4.3）。SET 从左到右执行，判断时 report_count 已经加过 1。
UPDATE questions SET report_count = report_count + 1,
  status = IF(source = 'ai_generated' AND report_count >= ?, 'offline', status)
WHERE id = ? AND owner_user_id = ?;

-- name: GetQuestionStatus :one
SELECT status, report_count, source FROM questions WHERE id = ? AND owner_user_id = ?;

-- name: SetQuestionGeneratedFrom :exec
UPDATE questions SET generated_from_kp_id = ?, answer_origin = 'ai_generated' WHERE id = ? AND owner_user_id = ?;
