-- name: CreateTextbookImport :execlastid
INSERT INTO textbook_imports (revision_id,file_name,format,size_bytes,sha256,created_by,request_key,request_hash,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?);
-- name: GetTextbookImport :one
SELECT * FROM textbook_imports WHERE id=? AND revision_id=?;
-- name: LockTextbookImport :one
SELECT * FROM textbook_imports WHERE id=? AND revision_id=? FOR UPDATE;
-- name: GetTextbookImportByID :one
SELECT * FROM textbook_imports WHERE id=?;
-- name: GetTextbookImportRequest :one
SELECT * FROM textbook_imports WHERE revision_id=? AND created_by=? AND request_key=?;
-- name: CountTextbookImports :one
SELECT COUNT(*) FROM textbook_imports WHERE revision_id=?;
-- name: ListTextbookImports :many
SELECT i.*, CAST((SELECT COUNT(*) FROM textbook_import_pages p WHERE p.import_id=i.id) AS UNSIGNED) AS completed_pages,
CAST((SELECT COUNT(*) FROM textbook_import_pages p WHERE p.import_id=i.id AND p.uncertain=TRUE) AS UNSIGNED) AS uncertain_pages
FROM textbook_imports i WHERE revision_id=? ORDER BY id DESC;
-- name: QueueTextbookImport :exec
UPDATE textbook_imports SET status='queued',attempt=attempt+1,retry_key=?,failure_message='',updated_at=? WHERE id=?;
-- name: UpdateTextbookImportState :exec
UPDATE textbook_imports SET status=?,failure_message=?,updated_at=? WHERE id=?;
-- name: SealTextbookImport :exec
UPDATE textbook_imports SET sealed=TRUE,updated_at=? WHERE id=?;
-- name: SetTextbookImportPageCount :exec
UPDATE textbook_imports SET page_count=?,updated_at=? WHERE id=?;
-- name: ApplyTextbookImport :exec
UPDATE textbook_imports SET status='applied',applied_mode=?,updated_at=? WHERE id=?;
-- name: GetTextbookImportPage :one
SELECT * FROM textbook_import_pages WHERE import_id=? AND page_no=?;
-- name: ListTextbookImportPages :many
SELECT * FROM textbook_import_pages WHERE import_id=? ORDER BY page_no;
-- name: SaveTextbookImportPage :exec
INSERT INTO textbook_import_pages (import_id,page_no,payload,uncertain,model,prompt_version) VALUES (?,?,?,?,?,?);
-- name: RecoverTextbookImports :many
SELECT id,attempt FROM textbook_imports WHERE status='queued' OR (status='processing' AND updated_at<?) ORDER BY id LIMIT 100;
-- name: EnsureTextbookDailyBudget :exec
INSERT IGNORE INTO textbook_daily_budget (budget_day) VALUES (?);
-- name: LockTextbookDailyBudget :one
SELECT pdf_pages FROM textbook_daily_budget WHERE budget_day=? FOR UPDATE;
-- name: AddTextbookDailyBudget :exec
UPDATE textbook_daily_budget SET pdf_pages=pdf_pages+? WHERE budget_day=?;
-- name: GetTextbookRevisionIdentity :one
SELECT id,status FROM textbook_revisions WHERE id=? AND edition_id=?;
