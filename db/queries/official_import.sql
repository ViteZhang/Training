-- T47 后台 AI 解析导入（团队资料，与用户资料分表）。

-- name: InsertOfficialImportFile :execlastid
INSERT INTO official_import_files (project_id, kind, file_name, format, object_key, size_bytes, content, exam_year, is_recollection, auto_answer, status, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetOfficialImportFile :one
SELECT * FROM official_import_files WHERE id = ?;

-- name: ListOfficialImportFiles :many
SELECT f.id, f.project_id, f.kind, f.file_name, f.format, f.status, f.fail_reason, f.question_count, f.kp_count, f.exam_year, f.created_at,
  f.auto_answer, f.fill_status, f.fill_total, f.fill_done, f.fill_failed,
  (SELECT COUNT(*) FROM official_import_items i WHERE i.file_id = f.id AND i.status <> 'pending') AS reviewed_count
FROM official_import_files f WHERE f.project_id = ? ORDER BY f.id DESC;

-- name: SetOfficialImportStatus :exec
UPDATE official_import_files SET status = ?, fail_reason = ? WHERE id = ?;

-- name: SetOfficialImportParsed :exec
UPDATE official_import_files SET status = 'parsed', fail_reason = NULL, content = ?, question_count = ?, kp_count = ? WHERE id = ?;

-- name: DeleteOfficialImportItems :exec
DELETE FROM official_import_items WHERE file_id = ? AND status = 'pending';

-- name: InsertOfficialImportItem :exec
INSERT INTO official_import_items (file_id, project_id, item_type, seq, payload, citations, source_text, page, confidence, fill_error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListOfficialImportItems :many
SELECT * FROM official_import_items WHERE file_id = ? ORDER BY seq, id;

-- name: GetOfficialImportItem :one
SELECT * FROM official_import_items WHERE id = ?;

-- name: ReviewOfficialImportItem :execrows
UPDATE official_import_items SET status = ?, payload = ?, draft_id = ?, reviewed_by = ?, reviewed_at = ? WHERE id = ? AND status = 'pending';

-- name: ListStaleOfficialImports :many
SELECT id FROM official_import_files WHERE status = 'parsing' AND updated_at < ?;

-- T49 解析后补答案与采分点（单题、整份一键补全）。

-- name: UpdateOfficialImportItemFill :execrows
-- 补全结果写回条目；只改还没核对的条目。
UPDATE official_import_items SET payload = ?, citations = ?, confidence = ?, fill_error = ? WHERE id = ? AND status = 'pending';

-- name: StartOfficialImportFill :execrows
-- 开始一键补全；已经在补全的不重复开始。
UPDATE official_import_files SET fill_status = 'running', fill_total = ?, fill_done = 0, fill_failed = 0
WHERE id = ? AND status = 'parsed' AND fill_status = 'idle';

-- name: SetOfficialImportFillProgress :exec
UPDATE official_import_files SET fill_done = ?, fill_failed = ? WHERE id = ?;

-- name: FinishOfficialImportFill :exec
UPDATE official_import_files SET fill_status = 'idle' WHERE id = ?;

-- name: ListStaleOfficialImportFills :many
SELECT id FROM official_import_files WHERE fill_status = 'running' AND updated_at < ?;

-- name: ListProjectImportQuestionPayloads :many
-- 同一项目里各资料解析出的题目，给缺分值的题参考同题型的默认给分。
SELECT payload FROM official_import_items WHERE project_id = ? AND item_type = 'question' AND status <> 'rejected';

-- name: ListProjectDraftQuestionPayloads :many
-- 同一项目里题目草稿，给缺分值的题参考同题型的默认给分。
SELECT payload FROM official_drafts WHERE project_id = ? AND entity_type = 'question' AND change_type <> 'offline' AND status <> 'rejected';
