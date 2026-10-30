-- name: CreateTextbookIndex :execlastid
INSERT INTO textbook_index_generations (book_id,edition_id,revision_id,source_version,content_hash,profile,profile_hash,split_config,created_by,request_key,request_hash,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?);
-- name: GetTextbookIndexRequest :one
SELECT * FROM textbook_index_generations WHERE revision_id=? AND created_by=? AND request_key=?;
-- name: GetTextbookIndex :one
SELECT * FROM textbook_index_generations WHERE id=?;
-- name: ListTextbookIndexes :many
SELECT g.*, (SELECT COUNT(*) FROM textbook_chunks c WHERE c.generation_id=g.id AND c.vector_ready=TRUE) AS completed_chunks
FROM textbook_index_generations g WHERE revision_id=? ORDER BY id DESC;
-- name: StartTextbookIndex :exec
UPDATE textbook_index_generations SET status='processing',collection_name=?,failure_message='',updated_at=? WHERE id=? AND status IN ('queued','processing','failed');
-- name: SetTextbookIndexCount :exec
UPDATE textbook_index_generations SET chunk_count=?,updated_at=? WHERE id=?;
-- name: SetTextbookIndexStatus :exec
UPDATE textbook_index_generations SET status=?,failure_message=?,updated_at=? WHERE id=?;
-- name: SetRevisionIndexStatus :exec
UPDATE textbook_revisions SET index_status=?,ready_generation_id=? WHERE id=?;
-- name: InsertTextbookChunk :exec
INSERT INTO textbook_chunks (generation_id,ordinal,section_no,start_rune,end_rune,heading,body,text_hash)
VALUES (?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE ordinal=VALUES(ordinal);
-- name: GetTextbookChunk :one
SELECT * FROM textbook_chunks WHERE generation_id=? AND ordinal=?;
-- name: CountTextbookChunks :one
SELECT COUNT(*) AS total, CAST(COALESCE(SUM(vector_ready),0) AS SIGNED) AS completed FROM textbook_chunks WHERE generation_id=?;
-- name: MarkTextbookChunkVector :exec
UPDATE textbook_chunks SET vector_ready=TRUE WHERE generation_id=? AND ordinal=?;
-- name: HeartbeatTextbookIndex :exec
UPDATE textbook_index_generations SET updated_at=? WHERE id=?;
-- name: RecoverableTextbookIndexes :many
SELECT id,attempt FROM textbook_index_generations WHERE status='queued' OR (status='processing' AND updated_at < ?) ORDER BY id LIMIT 100;
-- name: SearchTextbookChunks :many
SELECT ordinal, MATCH(body,heading) AGAINST(sqlc.arg(query) IN NATURAL LANGUAGE MODE) AS score
FROM textbook_chunks WHERE generation_id=? HAVING score > 0
ORDER BY score DESC, ordinal LIMIT ?;
-- name: GetTextbookPublicationRequest :one
SELECT * FROM textbook_publication_events WHERE book_id=? AND created_by=? AND request_key=?;
-- name: CreateTextbookPublicationEvent :exec
INSERT INTO textbook_publication_events (book_id,edition_id,revision_id,generation_id,action,summary,created_by,request_key,request_hash,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?);
-- name: ListTextbookPublicationEvents :many
SELECT * FROM textbook_publication_events WHERE book_id=? ORDER BY id DESC LIMIT 100;
-- name: PublishTextbookEdition :exec
UPDATE textbook_editions SET status='published',current_revision_id=?,current_generation_id=?,version=version+1 WHERE id=?;
-- name: PublishTextbookRevision :exec
UPDATE textbook_revisions SET status='published',version=version+1,updated_at=? WHERE id=?;
-- name: SetTextbookEditionAvailability :exec
UPDATE textbook_editions SET status=?,authorized=?,license_note=?,license_expires_at=?,excerpts_enabled=?,version=version+1 WHERE id=?;
-- name: SetTextbookDefaultEdition :exec
UPDATE textbooks SET default_edition_id=?,version=version+1,updated_at=? WHERE id=?;
-- name: ListPublishedTextbookEditions :many
SELECT b.title,b.author,b.kind,b.discipline,b.aliases,b.default_edition_id,e.*
FROM textbook_editions e JOIN textbooks b ON b.id=e.book_id
WHERE e.status='published' AND e.authorized=TRUE AND (e.license_expires_at IS NULL OR e.license_expires_at>?)
ORDER BY e.book_id,e.id;
-- name: LockTextbookIndex :one
SELECT * FROM textbook_index_generations WHERE id=? FOR UPDATE;
-- name: GetTextbookEditionByID :one
SELECT * FROM textbook_editions WHERE id=?;

-- name: RetryTextbookIndex :exec
UPDATE textbook_index_generations SET status='queued',failure_message='',attempt=attempt+1,retry_key=?,updated_at=? WHERE id=?;
