-- 备考档案与专业课（T07）。每条查询都带 owner_user_id / user_id 归属条件。

-- name: ListExamDatesFrom :many
SELECT * FROM exam_dates WHERE subject_exam_date >= ? ORDER BY exam_year;

-- name: GetExamDate :one
SELECT * FROM exam_dates WHERE exam_year = ?;

-- name: GetStudyProfile :one
SELECT * FROM study_profiles WHERE user_id = ?;

-- name: InsertStudyProfile :exec
INSERT INTO study_profiles (user_id, exam_year, stage, stage_manual, daily_minutes, reminder_times, target_school_major,
  essay_weekly_goal, mock_time_reminders, notify_daily, notify_review_due, notify_task_done)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateStudyProfile :exec
UPDATE study_profiles SET
  exam_year = ?, stage = ?, stage_manual = ?, daily_minutes = ?,
  pending_stage = ?, pending_daily_minutes = ?, pending_effective_on = ?,
  reminder_times = ?, target_school_major = ?, essay_weekly_goal = ?, mock_time_reminders = ?,
  notify_daily = ?, notify_review_due = ?, notify_task_done = ?
WHERE user_id = ?;

-- name: ListSubjects :many
SELECT s.*, b.id AS bank_id,
  (SELECT COUNT(*) FROM questions q WHERE q.bank_id = b.id AND q.status = 'active') AS question_count,
  (SELECT COUNT(*) FROM knowledge_points k WHERE k.bank_id = b.id AND k.level = 'point') AS kp_count,
  (SELECT COUNT(*) FROM materials m WHERE m.bank_id = b.id) AS material_count
FROM subjects s JOIN banks b ON b.subject_id = s.id
WHERE s.owner_user_id = ?
ORDER BY s.sort_order, s.id;

-- name: GetSubject :one
SELECT s.*, b.id AS bank_id FROM subjects s JOIN banks b ON b.subject_id = s.id
WHERE s.id = ? AND s.owner_user_id = ?;

-- name: CountSubjects :one
SELECT COUNT(*) FROM subjects WHERE owner_user_id = ?;

-- name: CreateSubject :execlastid
INSERT INTO subjects (owner_user_id, name, code, full_score, target_score, sort_order) VALUES (?, ?, ?, ?, ?, ?);

-- name: CreateUserBank :execlastid
INSERT INTO banks (source, owner_user_id, subject_id, title, subject_code) VALUES ('user', ?, ?, ?, ?);

-- name: UpdateSubject :exec
UPDATE subjects SET name = ?, code = ?, full_score = ?, target_score = ?, is_essay = ?, essay_set_by = ?
WHERE id = ? AND owner_user_id = ?;

-- name: UpdateBankForSubject :exec
UPDATE banks SET title = ?, subject_code = ? WHERE subject_id = ? AND owner_user_id = ?;

-- name: DeleteSubjectSessions :exec
DELETE FROM paper_sessions WHERE owner_user_id = ? AND subject_id = ?;

-- name: DeleteSubjectPracticeSessions :exec
DELETE FROM practice_sessions WHERE owner_user_id = ? AND subject_id = ?;

-- name: DeleteSubjectExports :exec
DELETE FROM export_jobs WHERE owner_user_id = ? AND subject_id = ?;

-- name: DeleteSubject :execrows
DELETE FROM subjects WHERE id = ? AND owner_user_id = ?;
