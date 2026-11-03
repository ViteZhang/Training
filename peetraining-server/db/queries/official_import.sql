-- T47 后台 AI 解析导入（团队资料，与用户资料分表）。

-- name: InsertOfficialImportFile :execlastid
INSERT INTO official_import_files (project_id, kind, file_name, format, object_key, size_bytes, content, exam_year, is_recollection, status, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetOfficialImportFile :one
SELECT * FROM official_import_files WHERE id = ?;

-- name: ListOfficialImportFiles :many
SELECT f.id, f.project_id, f.kind, f.file_name, f.format, f.status, f.fail_reason, f.question_count, f.kp_count, f.exam_year, f.created_at,
  (SELECT COUNT(*) FROM official_import_items i WHERE i.file_id = f.id AND i.status <> 'pending') AS reviewed_count
FROM official_import_files f WHERE f.project_id = ? ORDER BY f.id DESC;

-- name: SetOfficialImportStatus :exec
UPDATE official_import_files SET status = ?, fail_reason = ? WHERE id = ?;

-- name: SetOfficialImportParsed :exec
UPDATE official_import_files SET status = 'parsed', fail_reason = NULL, content = ?, question_count = ?, kp_count = ? WHERE id = ?;

-- name: DeleteOfficialImportItems :exec
DELETE FROM official_import_items WHERE file_id = ? AND status = 'pending';

-- name: InsertOfficialImportItem :exec
INSERT INTO official_import_items (file_id, project_id, item_type, seq, payload, citations, source_text, page, confidence)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListOfficialImportItems :many
SELECT * FROM official_import_items WHERE file_id = ? ORDER BY seq, id;

-- name: GetOfficialImportItem :one
SELECT * FROM official_import_items WHERE id = ?;

-- name: ReviewOfficialImportItem :execrows
UPDATE official_import_items SET status = ?, payload = ?, draft_id = ?, reviewed_by = ?, reviewed_at = ? WHERE id = ? AND status = 'pending';

-- name: ListStaleOfficialImports :many
SELECT id FROM official_import_files WHERE status = 'parsing' AND updated_at < ?;
