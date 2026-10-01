-- 导入流水线（T10）。每条查询都带 owner_user_id 归属条件。

-- name: CreateImportJob :execlastid
INSERT INTO import_jobs (owner_user_id, bank_id, mode, status, reserved_pages) VALUES (?, ?, ?, 'queued', 0);

-- name: AddImportJobMaterial :exec
INSERT INTO import_job_materials (job_id, material_id, owner_user_id) VALUES (?, ?, ?);

-- name: GetImportJob :one
SELECT j.*, b.subject_id, s.name AS subject_name, s.full_score AS subject_full_score FROM import_jobs j
JOIN banks b ON b.id = j.bank_id JOIN subjects s ON s.id = b.subject_id
WHERE j.id = ? AND j.owner_user_id = ?;

-- name: ListImportJobs :many
SELECT j.*, b.subject_id FROM import_jobs j JOIN banks b ON b.id = j.bank_id
WHERE j.owner_user_id = ? ORDER BY j.created_at DESC, j.id DESC LIMIT 50;

-- name: ListImportJobMaterials :many
SELECT jm.*, m.file_name, m.format, m.billed_pages, m.page_count,
  (SELECT COUNT(*) FROM import_items i WHERE i.job_id = jm.job_id AND i.material_id = jm.material_id AND i.status <> 'deleted') AS recognized_count
FROM import_job_materials jm JOIN materials m ON m.id = jm.material_id
WHERE jm.job_id = ? AND jm.owner_user_id = ?
ORDER BY m.id;

-- name: GetImportJobMaterial :one
SELECT * FROM import_job_materials WHERE job_id = ? AND material_id = ? AND owner_user_id = ?;

-- name: UpdateImportJobMaterial :exec
UPDATE import_job_materials SET step = ?, status = ?, fail_reason = ?, attempts = ?
WHERE job_id = ? AND material_id = ? AND owner_user_id = ?;

-- name: DeleteImportJobMaterial :execrows
DELETE FROM import_job_materials WHERE job_id = ? AND material_id = ? AND owner_user_id = ?;

-- name: UpdateImportJobStatus :exec
-- 时间由调用方给（数据库时间一律 UTC）；不改的时间传 NULL。
UPDATE import_jobs SET status = sqlc.arg(status), fail_reason = sqlc.narg(fail_reason),
  started_at = COALESCE(started_at, sqlc.narg(started_at)),
  finished_at = COALESCE(sqlc.narg(finished_at), finished_at),
  confirmed_at = COALESCE(sqlc.narg(confirmed_at), confirmed_at)
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: AddImportJobPages :exec
UPDATE import_jobs SET reserved_pages = reserved_pages + sqlc.arg(reserved), billed_pages = billed_pages + sqlc.arg(billed)
WHERE id = ? AND owner_user_id = ?;

-- name: SetImportJobPromptVersions :exec
UPDATE import_jobs SET prompt_versions = ? WHERE id = ? AND owner_user_id = ?;

-- name: UpsertImportItem :exec
-- 重跑同一步时按 dedupe_key 覆盖：用户还没动过（pending）的条目更新内容，已改过或已确认的保留用户的版本。
INSERT INTO import_items (owner_user_id, job_id, material_id, item_type, seq, payload, confidence, needs_review, review_reasons, dedupe_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  payload = IF(status = 'pending', VALUES(payload), payload),
  confidence = IF(status = 'pending', VALUES(confidence), confidence),
  needs_review = IF(status = 'pending', VALUES(needs_review), needs_review),
  review_reasons = IF(status = 'pending', VALUES(review_reasons), review_reasons);

-- name: ListJobItems :many
SELECT * FROM import_items WHERE job_id = ? AND owner_user_id = ? AND status <> 'deleted' ORDER BY seq, id;

-- name: ListImportItemsPage :many
SELECT * FROM import_items
WHERE job_id = sqlc.arg(job_id) AND owner_user_id = sqlc.arg(owner_user_id) AND status <> 'deleted' AND id > sqlc.arg(after_id)
  AND (sqlc.arg(filter) = 'all'
    OR (sqlc.arg(filter) = 'needs_review' AND needs_review = 1)
    OR (sqlc.arg(filter) = 'subjective' AND item_type = 'question' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.qtype')) IN ('term', 'short_answer', 'discussion', 'essay'))
    OR (sqlc.arg(filter) = 'objective' AND item_type = 'question' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.qtype')) NOT IN ('term', 'short_answer', 'discussion', 'essay')))
ORDER BY id LIMIT ?;

-- name: CountImportItems :one
SELECT
  CAST(COALESCE(SUM(item_type = 'question'), 0) AS SIGNED) AS questions,
  CAST(COALESCE(SUM(item_type = 'knowledge_point'), 0) AS SIGNED) AS knowledge_points,
  CAST(COALESCE(SUM(needs_review = 1), 0) AS SIGNED) AS needs_review,
  CAST(COALESCE(SUM(item_type = 'question' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.qtype')) IN ('term', 'short_answer', 'discussion', 'essay')), 0) AS SIGNED) AS subjective,
  CAST(COALESCE(SUM(item_type = 'question' AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.qtype')) NOT IN ('term', 'short_answer', 'discussion', 'essay')), 0) AS SIGNED) AS objective,
  CAST(COALESCE(SUM(status = 'confirmed' AND created_entity_id IS NOT NULL), 0) AS SIGNED) AS confirmed,
  CAST(COALESCE(SUM(item_type IN ('essay_topic', 'essay_rubric', 'writing_method', 'essay_material', 'model_essay')), 0) AS SIGNED) AS essay_items
FROM import_items WHERE job_id = ? AND owner_user_id = ? AND status <> 'deleted';

-- name: GetImportItem :one
SELECT i.*, j.bank_id FROM import_items i JOIN import_jobs j ON j.id = i.job_id
WHERE i.id = ? AND i.owner_user_id = ?;

-- name: UpdateImportItem :exec
UPDATE import_items SET payload = ?, status = ?, needs_review = ?, review_reasons = ?, duplicate_of_question_id = ?
WHERE id = ? AND owner_user_id = ?;

-- name: SetImportItemCreated :exec
UPDATE import_items SET status = 'confirmed', created_entity_id = ? WHERE id = ? AND owner_user_id = ?;

-- name: DeleteImportItemsOfMaterial :exec
-- 从任务里移除文件（1.6b）时，它还没确认的条目一起删掉。
DELETE FROM import_items WHERE job_id = ? AND material_id = ? AND owner_user_id = ? AND created_entity_id IS NULL;

-- name: UpsertImportAnswer :exec
INSERT INTO import_answers (owner_user_id, job_id, material_id, exam_year, question_no, answer, page, dedupe_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE answer = VALUES(answer), page = VALUES(page);

-- name: ListImportAnswers :many
SELECT * FROM import_answers WHERE job_id = ? AND owner_user_id = ? ORDER BY id;

-- name: ListBankQuestionsForDedupe :many
SELECT id, qtype, stem, content_hash FROM questions WHERE bank_id = ? AND owner_user_id = ? AND status = 'active';

-- name: ListBankKPs :many
SELECT id, parent_id, level, name FROM knowledge_points WHERE bank_id = ? AND owner_user_id = ? ORDER BY sort_order, id;
