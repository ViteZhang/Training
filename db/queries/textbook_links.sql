-- name: SetSubjectTextbookSelections :exec
UPDATE subjects SET textbook_selections=? WHERE id=? AND owner_user_id=?;
-- name: GetBankTextbookSelections :one
SELECT s.textbook_selections FROM subjects s JOIN banks b ON b.subject_id=s.id WHERE b.id=? AND s.owner_user_id=?;
-- name: ListProjectTextbooks :many
SELECT e.*,b.title,b.author,b.version AS catalog_version FROM official_project_textbooks p JOIN textbook_editions e ON e.id=p.edition_id JOIN textbooks b ON b.id=e.book_id WHERE p.project_id=? ORDER BY e.id;
-- name: DeleteProjectTextbooks :exec
DELETE FROM official_project_textbooks WHERE project_id=?;
-- name: AddProjectTextbook :exec
INSERT INTO official_project_textbooks(project_id,edition_id,created_at) VALUES(?,?,?);
-- name: SaveOfficialAnswerCandidate :exec
INSERT INTO official_answer_candidate_evidence(project_id,snapshot_id,content_hash) VALUES(?,?,?);
-- name: GetOfficialAnswerCandidate :one
SELECT content_hash FROM official_answer_candidate_evidence WHERE project_id=? AND snapshot_id=?;
-- name: LockOfficialProject :one
SELECT * FROM official_projects WHERE id=? FOR UPDATE;

-- name: GetProjectTextbookWrite :one
SELECT request_hash FROM official_project_textbook_writes WHERE project_id=? AND request_key=?;
-- name: SaveProjectTextbookWrite :exec
INSERT INTO official_project_textbook_writes(project_id,request_key,request_hash)VALUES(?,?,?);

-- name: ListLegacyTextbookReferences :many
SELECT r.id FROM official_references r ORDER BY r.id;
-- name: LockLegacyTextbookReference :one
SELECT r.*,p.subject_name FROM official_references r JOIN official_projects p ON p.id=r.project_id WHERE r.id=? FOR UPDATE;
-- name: ListLegacyTextbookSegments :many
SELECT * FROM official_reference_segments WHERE reference_id=? ORDER BY seq FOR UPDATE;
-- name: GetLegacyTextbookMigration :one
SELECT * FROM legacy_textbook_migrations WHERE reference_id=?;
-- name: SaveLegacyTextbookMigration :exec
INSERT INTO legacy_textbook_migrations(reference_id,book_id,edition_id,revision_id,created_at,source_hash,manifest,source_project_id,source_title)VALUES(?,?,?,?,?,?,?,?,?);

-- name: BumpProjectTextbookVersion :exec
UPDATE official_projects SET textbook_link_version=textbook_link_version+1 WHERE id=?;

-- name: GetOfficialAnswerCandidateByBank :one
SELECT c.content_hash FROM official_answer_candidate_evidence c JOIN official_projects p ON p.id=c.project_id WHERE p.bank_id=? AND c.snapshot_id=?;
