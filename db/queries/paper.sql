-- 整卷与模拟考试（T21）。每条查询都带归属条件。

-- name: ListBankPapers :many
SELECT p.*, (SELECT COUNT(*) FROM paper_questions pq WHERE pq.paper_id = p.id) AS question_count,
  (SELECT COUNT(*) FROM paper_questions pq JOIN questions q ON q.id = pq.question_id WHERE pq.paper_id = p.id AND q.source = 'ai_generated') AS ai_filled
FROM papers p
WHERE p.bank_id = ? AND p.owner_user_id = ?
ORDER BY p.kind = 'real_exam' DESC, p.exam_year DESC, p.id DESC;

-- name: GetPaper :one
SELECT p.*, b.subject_id FROM papers p JOIN banks b ON b.id = p.bank_id WHERE p.id = ? AND p.owner_user_id = ?;

-- name: ListPaperQuestions :many
SELECT pq.seq, pq.question_id, pq.section, pq.score, q.qtype, q.source FROM paper_questions pq
JOIN questions q ON q.id = pq.question_id
WHERE pq.paper_id = ? AND q.owner_user_id = ?
ORDER BY pq.seq;

-- name: LatestRealExamPaper :one
SELECT * FROM papers WHERE bank_id = ? AND owner_user_id = ? AND kind = 'real_exam' ORDER BY exam_year DESC, id DESC LIMIT 1;

-- name: InsertComposedPaper :execlastid
INSERT INTO papers (owner_user_id, bank_id, kind, title, full_score, actual_score, duration_minutes, structure)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListPaperSessionsOfUser :many
-- 整卷列表的状态：每套卷最近的作答。
SELECT id, paper_id, mode, status, started_at, submitted_at, score, full_score FROM paper_sessions
WHERE owner_user_id = ? AND subject_id = ? AND status <> 'abandoned'
ORDER BY id DESC;

-- name: ActivePaperSession :one
-- 同一用户同一时间只能有一套进行中（含暂停）。
SELECT * FROM paper_sessions WHERE owner_user_id = ? AND status IN ('in_progress', 'paused') ORDER BY id DESC LIMIT 1;

-- name: GetPaperSessionByKey :one
SELECT * FROM paper_sessions WHERE owner_user_id = ? AND idempotency_key = ?;

-- name: InsertPaperSession :execlastid
INSERT INTO paper_sessions (owner_user_id, paper_id, subject_id, paper_kind, paper_title, mode, started_at, deadline_at, full_score,
  counts_for_estimate, quota_period, idempotency_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertPaperSessionItem :exec
INSERT INTO paper_session_items (paper_session_id, seq, question_id, qtype, section, score_max) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetPaperSession :one
SELECT * FROM paper_sessions WHERE id = ? AND owner_user_id = ?;

-- name: GetPaperSessionForUpdate :one
SELECT * FROM paper_sessions WHERE id = ? AND owner_user_id = ? FOR UPDATE;

-- name: ListPaperSessionItems :many
SELECT i.*, q.stem, q.options, q.source, q.required_words, a.score AS got,
  CAST(COALESCE((SELECT g.id FROM gradings g WHERE g.attempt_id = i.attempt_id AND g.status = 'done' ORDER BY g.id DESC LIMIT 1), 0) AS UNSIGNED) AS grading_id
FROM paper_session_items i
JOIN paper_sessions s ON s.id = i.paper_session_id
LEFT JOIN questions q ON q.id = i.question_id
LEFT JOIN attempts a ON a.id = i.attempt_id
WHERE i.paper_session_id = ? AND s.owner_user_id = ?
ORDER BY i.seq;

-- name: UpdatePaperItem :execrows
UPDATE paper_session_items i JOIN paper_sessions s ON s.id = i.paper_session_id
SET i.draft_text = ?, i.marked = ?, i.time_spent_seconds = i.time_spent_seconds + ?
WHERE i.paper_session_id = ? AND i.seq = ? AND s.owner_user_id = ?;

-- name: SetPaperItemAttempt :exec
UPDATE paper_session_items i JOIN paper_sessions s ON s.id = i.paper_session_id
SET i.attempt_id = ? WHERE i.paper_session_id = ? AND i.seq = ? AND s.owner_user_id = ?;

-- name: PausePaperSession :exec
UPDATE paper_sessions SET status = 'paused', paused_at = ? WHERE id = ? AND owner_user_id = ? AND status = 'in_progress';

-- name: ResumePaperSession :exec
UPDATE paper_sessions SET status = 'in_progress', paused_seconds = paused_seconds + ?, paused_at = NULL WHERE id = ? AND owner_user_id = ?;

-- name: RecoverPaperSession :exec
-- 模拟考试中断恢复：补回中断时长，只能用一次（PRD 11.9）。
UPDATE paper_sessions SET deadline_at = ?, interrupted_at = ?, resume_used = 1 WHERE id = ? AND owner_user_id = ? AND resume_used = 0;

-- name: SubmitPaperSession :exec
UPDATE paper_sessions SET status = 'grading', submitted_at = ?, paused_seconds = paused_seconds + ?, paused_at = NULL
WHERE id = ? AND owner_user_id = ? AND status IN ('in_progress', 'paused');

-- name: FinishPaperGrading :exec
UPDATE paper_sessions SET status = 'graded', graded_at = ?, score = ?, report = ? WHERE id = ? AND owner_user_id = ? AND status = 'grading';

-- name: AbandonPaperSession :exec
UPDATE paper_sessions SET status = 'abandoned' WHERE id = ? AND owner_user_id = ? AND status IN ('in_progress', 'paused');

-- name: InsertMessage :exec
INSERT IGNORE INTO messages (owner_user_id, mtype, title, body, link, dedupe_key) VALUES (?, ?, ?, ?, ?, ?);

-- name: DonePaperQuestionIDs :many
-- 已在整卷中做过的题（AI 组卷不再出现做过的真题）。
SELECT DISTINCT i.question_id FROM paper_session_items i JOIN paper_sessions s ON s.id = i.paper_session_id
WHERE s.owner_user_id = ? AND s.status IN ('grading', 'graded') AND i.question_id IS NOT NULL;

-- name: LastAttemptDays :many
-- 每道题最近一次作答的时间（针对卷避开近 30 天做过的题）。
SELECT question_id, MAX(answered_at) AS last_at FROM attempts WHERE owner_user_id = ? GROUP BY question_id;

-- name: QTypeScoreRates :many
-- 用户各题型的平均得分率（估计时间失分，PRD 11.9）。
SELECT q.qtype, CAST(COALESCE(SUM(a.score), 0) AS DECIMAL(12,2)) AS got, CAST(COALESCE(SUM(a.full_score), 0) AS DECIMAL(12,2)) AS full
FROM attempts a JOIN questions q ON q.id = a.question_id
WHERE a.owner_user_id = ? AND a.score IS NOT NULL AND a.full_score IS NOT NULL
GROUP BY q.qtype;

-- name: InsertPaperAttempt :execlastid
-- 整卷里的一道作答（模拟考试记为限时作答，失分诊断「时间不够」只在这里判定）。
INSERT INTO attempts (owner_user_id, question_id, paper_session_id, answer_mode, selected, answer_text, is_correct, score, full_score,
  duration_seconds, timed, answered_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
