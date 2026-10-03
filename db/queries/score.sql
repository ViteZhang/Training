-- 预估分、整卷报告与提分看板（T22，PRD 11.6、4.24、4.25、6.2）。

-- name: ListGradedPaperSessions :many
-- 一门课批改完成的整卷，新的在前（预估分、较上次、趋势、最近成绩）。
SELECT id, paper_id, paper_kind, paper_title, mode, started_at, submitted_at, graded_at, paused_seconds, full_score, score, counts_for_estimate, report
FROM paper_sessions
WHERE owner_user_id = ? AND subject_id = ? AND status = 'graded'
ORDER BY graded_at DESC, id DESC
LIMIT 50;

-- name: ListSubjectRecentRates :many
-- 一门课最近的作答得分（模型分用各题型近 20 题得分率）；自评不算。
SELECT q.qtype, a.score, a.full_score, a.is_correct
FROM attempts a
JOIN questions q ON q.id = a.question_id
JOIN banks b ON b.id = q.bank_id
WHERE a.owner_user_id = sqlc.arg(owner_user_id) AND b.subject_id = sqlc.arg(subject_id) AND b.owner_user_id = a.owner_user_id
  AND a.answer_mode <> 'self_assess'
  AND ((a.score IS NOT NULL AND a.full_score IS NOT NULL AND a.full_score > 0) OR a.is_correct IS NOT NULL)
ORDER BY a.answered_at DESC, a.id DESC
LIMIT 1000;

-- name: InsertScoreEstimate :exec
INSERT INTO score_estimates (owner_user_id, subject_id, low, high, mid, basis_papers, basis_questions, main_gap_qtype, details, trigger_reason, computed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListScoreEstimates :many
-- 一门课的预估分历史，新的在前（当前值、今天的变化、按周趋势）。
SELECT id, low, high, mid, basis_papers, basis_questions, main_gap_qtype, computed_at
FROM score_estimates
WHERE owner_user_id = ? AND subject_id = ? AND computed_at >= ?
ORDER BY computed_at DESC, id DESC;

-- name: LatestScoreEstimate :one
SELECT id, low, high, mid, basis_papers, basis_questions, main_gap_qtype, computed_at
FROM score_estimates
WHERE owner_user_id = ? AND subject_id = ?
ORDER BY computed_at DESC, id DESC
LIMIT 1;

-- name: LastScoreEstimateBefore :one
SELECT id, low, high, mid, computed_at
FROM score_estimates
WHERE owner_user_id = ? AND subject_id = ? AND computed_at < ?
ORDER BY computed_at DESC, id DESC
LIMIT 1;

-- name: ListSubjectGradingLoss :many
-- 一门课近 N 天主观题批改的失分归因（提分看板）。
SELECT g.loss
FROM gradings g
JOIN attempts a ON a.id = g.attempt_id
JOIN questions q ON q.id = a.question_id
JOIN banks b ON b.id = q.bank_id
WHERE g.owner_user_id = sqlc.arg(owner_user_id) AND b.subject_id = sqlc.arg(subject_id) AND b.owner_user_id = g.owner_user_id
  AND g.status = 'done' AND g.created_at >= sqlc.arg(since) AND g.loss IS NOT NULL;

-- name: SubjectOfQuestion :one
SELECT b.subject_id FROM questions q JOIN banks b ON b.id = q.bank_id WHERE q.id = ? AND b.owner_user_id = ?;

-- name: ListSubjectsWithPapers :many
SELECT DISTINCT subject_id FROM paper_sessions WHERE owner_user_id = ? AND status = 'graded' AND counts_for_estimate = 1;
