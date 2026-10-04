-- 主观题批改、待批改、异议（T18）。每条查询都带归属条件。

-- name: GetGradingByKey :one
SELECT * FROM gradings WHERE owner_user_id = ? AND idempotency_key = ?;

-- name: GetGrading :one
SELECT g.*, a.question_id, a.answer_text, a.timed, a.answered_at, a.practice_session_id
FROM gradings g JOIN attempts a ON a.id = g.attempt_id
WHERE g.id = ? AND g.owner_user_id = ?;

-- name: InsertGrading :execlastid
INSERT INTO gradings (owner_user_id, attempt_id, kind, trigger_reason, parent_grading_id, status, rubric_version, rubric_snapshot,
  score, full_score, point_results, loss, suggestions, model, prompt_version, quota_charged, idempotency_key, finished_at)
VALUES (?, ?, 'subjective', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: CompleteGrading :exec
-- 待批改提交后写入结果。
UPDATE gradings SET status = 'done', trigger_reason = 'pending_resubmit', rubric_version = ?, rubric_snapshot = ?, score = ?, full_score = ?,
  point_results = ?, loss = ?, suggestions = ?, model = ?, prompt_version = ?, quota_charged = 1, finished_at = ?
WHERE id = ? AND owner_user_id = ? AND status = 'queued_quota';

-- name: ListQueuedGradings :many
SELECT g.id, g.attempt_id, g.created_at, a.question_id, q.qtype, q.stem
FROM gradings g JOIN attempts a ON a.id = g.attempt_id JOIN questions q ON q.id = a.question_id
WHERE g.owner_user_id = ? AND g.status = 'queued_quota'
ORDER BY g.id;

-- name: SetAttemptScore :exec
UPDATE attempts SET score = ?, full_score = ? WHERE id = ? AND owner_user_id = ?;

-- name: CountDisputes :one
SELECT COUNT(*) FROM disputes WHERE grading_id = ? AND owner_user_id = ?;

-- name: InsertDispute :execlastid
INSERT INTO disputes (owner_user_id, grading_id, reason, note, allow_access, regrade_id, score_before, score_after, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'rechecked');

-- name: InsertContentAccessGrant :exec
INSERT INTO content_access_grants (user_id, source, source_id, scope, expires_at) VALUES (?, 'dispute', ?, ?, ?);

-- name: DisputedGradings :many
-- 已复核过的批改（含被复核重批出来的新批改），每次批改只能复核一次。
SELECT d.grading_id, d.regrade_id FROM disputes d WHERE d.owner_user_id = ? AND (d.grading_id = ? OR d.regrade_id = ?);

-- name: LatestDoneGrading :one
SELECT * FROM gradings WHERE attempt_id = ? AND owner_user_id = ? AND status = 'done' ORDER BY id DESC LIMIT 1;

-- name: CorrectWrongBookRate :exec
UPDATE wrong_book SET last_score_rate = ? WHERE owner_user_id = ? AND question_id = ? AND status = 'active';

-- name: DropPartialWrongBook :exec
-- 重批后拿满分：这次批改收录的「没拿满分」错题撤回。
UPDATE wrong_book SET status = 'removed', removed_at = ?
WHERE owner_user_id = ? AND question_id = ? AND status = 'active' AND added_reason = 'partial' AND wrong_count <= 1;

-- name: SetAttemptTimed :exec
UPDATE attempts SET timed = 1 WHERE id = ? AND owner_user_id = ?;
