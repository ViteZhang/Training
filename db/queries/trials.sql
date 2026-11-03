-- T45 体验一道题、通用体验题与 AI 出题（每条用户内容查询都带 owner_user_id）。

-- name: GetTrialUsage :one
SELECT used FROM trial_usage WHERE owner_user_id = ?;

-- name: LockTrialUsage :one
SELECT used FROM trial_usage WHERE owner_user_id = ? FOR UPDATE;

-- name: EnsureTrialUsage :exec
INSERT IGNORE INTO trial_usage (owner_user_id, used) VALUES (?, 0);

-- name: IncTrialUsage :exec
UPDATE trial_usage SET used = used + 1, first_graded_at = COALESCE(first_graded_at, sqlc.arg(now)) WHERE owner_user_id = sqlc.arg(owner_user_id);

-- name: InsertTrial :execlastid
INSERT INTO trials (owner_user_id, kind, sample_question_id, input_mode, qtype, score, stem, reference_answer, answer_source, rubric)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTrial :one
SELECT * FROM trials WHERE id = ? AND owner_user_id = ?;

-- name: GetTrialByKey :one
SELECT * FROM trials WHERE owner_user_id = ? AND idempotency_key = ?;

-- name: SetTrialGraded :execrows
UPDATE trials SET answer_mode = ?, answer_text = ?, photo_keys = ?, result = ?, total = ?, status = 'graded', idempotency_key = ?, graded_at = ?
WHERE id = ? AND owner_user_id = ? AND status = 'ready';

-- name: SetTrialSaved :exec
UPDATE trials SET status = 'saved', saved_question_id = ? WHERE id = ? AND owner_user_id = ?;

-- name: DeleteTrial :exec
DELETE FROM trials WHERE id = ? AND owner_user_id = ?;

-- name: ListSampleCatalog :many
-- 1.10：已上线的通用体验题，按学科、方向分组。
SELECT d.id AS direction_id, d.discipline, d.name AS direction, q.id, q.qtype, q.score, q.stem, q.textbook
FROM sample_directions d JOIN sample_questions q ON q.direction_id = d.id
WHERE q.status = 'online'
ORDER BY d.discipline, d.sort_order, d.id, q.qtype, q.id;

-- name: GetSampleQuestion :one
SELECT * FROM sample_questions WHERE id = ?;

-- name: GetLiveSampleByKey :one
SELECT * FROM sample_questions WHERE cache_key = ? AND origin = 'ai_live' AND status = 'online' ORDER BY id DESC LIMIT 1;

-- name: ListStemsByCacheKey :many
SELECT stem FROM sample_questions WHERE cache_key = ?;

-- name: InsertSampleQuestion :execlastid
INSERT INTO sample_questions (direction_id, origin, cache_key, subject_label, textbook, qtype, score, stem, answer, rubric, status, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: IncSampleTryCount :exec
UPDATE sample_questions SET try_count = try_count + 1 WHERE id = ?;

-- name: AdminSampleStats :one
SELECT
  (SELECT COUNT(DISTINCT discipline) FROM sample_directions) AS disciplines,
  (SELECT COUNT(*) FROM sample_directions) AS directions,
  (SELECT COUNT(*) FROM sample_questions WHERE direction_id IS NOT NULL AND status <> 'offline') AS questions,
  (SELECT COUNT(*) FROM sample_questions WHERE direction_id IS NOT NULL AND status = 'pending') AS pending,
  (SELECT COUNT(*) FROM trials WHERE kind IN ('sample', 'ai_live') AND graded_at >= sqlc.arg(since)) AS week_tries;

-- name: AdminListSampleDirections :many
SELECT d.*, (SELECT COUNT(*) FROM sample_questions q WHERE q.direction_id = d.id AND q.status <> 'offline') AS question_count
FROM sample_directions d ORDER BY d.discipline, d.sort_order, d.id;

-- name: InsertSampleDirection :execlastid
INSERT INTO sample_directions (discipline, name, sort_order) VALUES (?, ?, ?);

-- name: GetSampleDirection :one
SELECT * FROM sample_directions WHERE id = ?;

-- name: AdminListSampleQuestions :many
SELECT * FROM sample_questions WHERE direction_id = ? ORDER BY status = 'offline', id DESC;

-- name: AdminListLiveSamples :many
SELECT * FROM sample_questions WHERE origin = 'ai_live' ORDER BY id DESC LIMIT 200;

-- name: UpdateSampleQuestion :exec
UPDATE sample_questions SET textbook = ?, qtype = ?, score = ?, stem = ?, answer = ?, rubric = ? WHERE id = ?;

-- name: SetSampleQuestionStatus :exec
UPDATE sample_questions SET status = ?, reviewed_by = ?, reviewed_at = ? WHERE id = ?;

-- name: ListDirectionStems :many
SELECT stem FROM sample_questions WHERE direction_id = ?;
