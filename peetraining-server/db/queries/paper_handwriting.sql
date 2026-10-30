-- name: ListPaperHandwriting :many
SELECT h.* FROM paper_handwriting h JOIN paper_sessions s ON s.id = h.paper_session_id AND s.owner_user_id = h.owner_user_id
WHERE h.paper_session_id = ? AND h.owner_user_id = ? ORDER BY h.capture_order, h.id;

-- name: GetPaperHandwriting :one
SELECT h.* FROM paper_handwriting h JOIN paper_sessions s ON s.id = h.paper_session_id AND s.owner_user_id = h.owner_user_id
WHERE h.id = ? AND h.owner_user_id = ?;

-- name: InsertPaperHandwriting :execlastid
INSERT INTO paper_handwriting (owner_user_id, paper_session_id, seq, idempotency_key, capture_order, object_keys, input_kind, recognized_text, low_confidence)
VALUES (?, ?, ?, ?, ?, ?, ?, '', JSON_ARRAY());

-- name: ReadyPaperHandwriting :exec
UPDATE paper_handwriting SET status = 'ready', recognized_text = ?, low_confidence = ?, error_message = ''
WHERE id = ? AND owner_user_id = ? AND status IN ('pending', 'failed');

-- name: FailPaperHandwriting :exec
UPDATE paper_handwriting SET status = 'failed', error_message = ?
WHERE id = ? AND owner_user_id = ? AND status IN ('pending', 'failed');

-- name: ConfirmPaperHandwriting :exec
UPDATE paper_handwriting SET status = 'confirmed', recognized_text = ?, error_message = ''
WHERE id = ? AND owner_user_id = ? AND status = 'ready';

-- name: DiscardPaperHandwriting :exec
UPDATE paper_handwriting SET status = 'discarded' WHERE id = ? AND owner_user_id = ? AND status <> 'confirmed';

-- name: AppendPaperHandwriting :exec
UPDATE paper_session_items i JOIN paper_sessions s ON s.id = i.paper_session_id
SET i.draft_text = ? WHERE i.paper_session_id = ? AND i.seq = ? AND s.owner_user_id = ? AND s.status IN ('in_progress', 'paused', 'grading');

-- name: EditPaperHandwriting :exec
UPDATE paper_handwriting SET recognized_text = ?, low_confidence = JSON_ARRAY()
WHERE id = ? AND owner_user_id = ? AND status = 'ready';
