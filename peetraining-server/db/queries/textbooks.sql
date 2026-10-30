-- T38 官方教材库：仅共享教材，不读取用户私有题目或原文。
-- name: CreateTextbook :execlastid
INSERT INTO textbooks (title, author, kind, discipline, aliases, created_by, request_key, request_hash, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTextbook :one
SELECT * FROM textbooks WHERE id = ?;

-- name: LockTextbook :one
SELECT * FROM textbooks WHERE id = ? FOR UPDATE;

-- name: GetTextbookRequest :one
SELECT * FROM textbooks WHERE created_by = ? AND request_key = ?;

-- name: UpdateTextbookMetadata :execrows
UPDATE textbooks SET title = ?, author = ?, kind = ?, discipline = ?, aliases = ?, version = version + 1, updated_at = ?
WHERE id = ? AND version = ?;

-- name: ListTextbooks :many
SELECT * FROM textbooks
WHERE (CAST(sqlc.arg(search) AS CHAR) = '' OR LOCATE(CAST(sqlc.arg(search) AS CHAR), CONCAT(title, ' ', author, ' ', CAST(aliases AS CHAR))) > 0)
AND (CAST(sqlc.arg(kind) AS CHAR) = '' OR kind = CAST(sqlc.arg(kind) AS CHAR))
AND (CAST(sqlc.arg(discipline) AS CHAR) = '' OR discipline = CAST(sqlc.arg(discipline) AS CHAR))
ORDER BY id DESC LIMIT ? OFFSET ?;

-- name: CountTextbooks :one
SELECT COUNT(*) FROM textbooks
WHERE (CAST(sqlc.arg(search) AS CHAR) = '' OR LOCATE(CAST(sqlc.arg(search) AS CHAR), CONCAT(title, ' ', author, ' ', CAST(aliases AS CHAR))) > 0)
AND (CAST(sqlc.arg(kind) AS CHAR) = '' OR kind = CAST(sqlc.arg(kind) AS CHAR))
AND (CAST(sqlc.arg(discipline) AS CHAR) = '' OR discipline = CAST(sqlc.arg(discipline) AS CHAR));

-- name: CreateTextbookEdition :execlastid
INSERT INTO textbook_editions (book_id, edition, publisher, publication_year, isbn, license_note, authorized, scope_note, created_by, request_key, request_hash, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListTextbookEditions :many
SELECT * FROM textbook_editions WHERE book_id = ? ORDER BY id;

-- name: GetTextbookEdition :one
SELECT * FROM textbook_editions WHERE id = ? AND book_id = ?;

-- name: LockTextbookEdition :one
SELECT * FROM textbook_editions WHERE id = ? AND book_id = ? FOR UPDATE;

-- name: GetTextbookEditionRequest :one
SELECT * FROM textbook_editions WHERE book_id = ? AND created_by = ? AND request_key = ?;

-- name: ListTextbookRevisions :many
SELECT id, edition_id, revision_no, version, status, content_hash, index_status, ready_generation_id, created_at, updated_at
FROM textbook_revisions WHERE edition_id = ? ORDER BY revision_no DESC;

-- name: GetTextbookRevision :one
SELECT * FROM textbook_revisions WHERE id = ? AND edition_id = ?;

-- name: LockTextbookRevision :one
SELECT * FROM textbook_revisions WHERE id = ? AND edition_id = ? FOR UPDATE;

-- name: GetTextbookRevisionRequest :one
SELECT * FROM textbook_revisions WHERE edition_id = ? AND created_by = ? AND request_key = ?;

-- name: NextTextbookRevisionNo :one
SELECT CAST(COALESCE(MAX(revision_no), 0) + 1 AS UNSIGNED) FROM textbook_revisions WHERE edition_id = ?;

-- name: CreateTextbookRevision :execlastid
INSERT INTO textbook_revisions (edition_id, revision_no, sections, content_hash, created_by, request_key, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: SaveTextbookRevision :execrows
UPDATE textbook_revisions SET sections = ?, content_hash = ?, status = ?, version = version + 1,
 index_status = 'stale', ready_generation_id = NULL, reviewed_by = ?, reviewed_at = ?, updated_at = ?
WHERE id = ? AND edition_id = ? AND version = ? AND status <> 'published';
