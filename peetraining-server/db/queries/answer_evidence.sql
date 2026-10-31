-- name: CreateAnswerSnapshot :execlastid
INSERT INTO answer_evidence_snapshots (owner_user_id,answer_hash,payload,created_at) VALUES (?,?,?,?);
-- name: GetAnswerSnapshot :one
SELECT * FROM answer_evidence_snapshots WHERE id=? AND owner_user_id <=> sqlc.narg(owner_user_id);
-- name: SetQuestionAnswerSnapshot :exec
UPDATE questions SET answer_snapshot_id=? WHERE id=? AND owner_user_id <=> sqlc.narg(owner_user_id);
-- name: LockQuestionAnswer :one
SELECT * FROM questions WHERE id=? AND owner_user_id=? FOR UPDATE;
-- name: LockImportAnswer :one
SELECT * FROM import_items WHERE id=? AND owner_user_id=? FOR UPDATE;
-- name: GetAnswerCandidateRequest :one
SELECT * FROM answer_update_candidates WHERE question_id=? AND owner_user_id=? AND request_key=?;
-- name: CreateAnswerCandidate :execlastid
INSERT INTO answer_update_candidates (owner_user_id,question_id,request_key,base_hash,created_at,updated_at) VALUES (?,?,?,?,?,?);
-- name: GetAnswerCandidate :one
SELECT * FROM answer_update_candidates WHERE id=? AND question_id=? AND owner_user_id=?;
-- name: LockAnswerCandidate :one
SELECT * FROM answer_update_candidates WHERE id=? AND question_id=? AND owner_user_id=? FOR UPDATE;
-- name: ListAnswerCandidates :many
SELECT * FROM answer_update_candidates WHERE question_id=? AND owner_user_id=? ORDER BY id DESC LIMIT 50;
-- name: GetAnswerCandidateJob :one
SELECT id,owner_user_id,question_id,status FROM answer_update_candidates WHERE id=?;
-- name: RecoverAnswerCandidates :many
SELECT id FROM answer_update_candidates WHERE status='queued' OR (status='processing' AND updated_at < ?) ORDER BY id LIMIT 100;
-- name: SetAnswerCandidateProcessing :exec
UPDATE answer_update_candidates SET status='processing',updated_at=? WHERE id=? AND owner_user_id=? AND status IN ('queued','processing');
-- name: CompleteAnswerCandidate :exec
UPDATE answer_update_candidates SET status='ready',answer=?,points=?,snapshot_id=?,updated_at=? WHERE id=? AND owner_user_id=?;
-- name: SetAnswerCandidateStatus :exec
UPDATE answer_update_candidates SET status=?,failure_message=?,updated_at=? WHERE id=? AND question_id=? AND owner_user_id=?;
-- name: FailAnswerCandidate :exec
UPDATE answer_update_candidates SET status='failed',failure_message=?,updated_at=? WHERE id=? AND question_id=? AND owner_user_id=? AND status IN ('queued','processing');
-- name: BumpQuestionAnswerVersion :exec
UPDATE questions SET rubric_version=rubric_version+1 WHERE id=? AND owner_user_id=?;
