-- name: ListVoiceTranscriptions :many
SELECT v.* FROM voice_transcriptions v JOIN practice_sessions s ON s.id = v.practice_session_id AND s.owner_user_id = v.owner_user_id
WHERE v.owner_user_id = ? AND v.practice_session_id = ? AND v.question_id = ? ORDER BY v.capture_order, v.id;

-- name: GetVoiceTranscription :one
SELECT v.* FROM voice_transcriptions v JOIN practice_sessions s ON s.id = v.practice_session_id AND s.owner_user_id = v.owner_user_id
WHERE v.id = ? AND v.owner_user_id = ?;

-- name: InsertVoiceTranscription :execlastid
INSERT INTO voice_transcriptions (owner_user_id, practice_session_id, question_id, idempotency_key, object_key, capture_order, recognized_text)
VALUES (?, ?, ?, ?, ?, ?, '');

-- name: ReadyVoiceTranscription :exec
UPDATE voice_transcriptions SET status = 'ready', recognized_text = ?, error_message = ''
WHERE id = ? AND owner_user_id = ? AND status IN ('pending', 'failed');

-- name: FailVoiceTranscription :exec
UPDATE voice_transcriptions SET status = 'failed', error_message = '语音识别失败，录音已保存，请重试'
WHERE id = ? AND owner_user_id = ? AND status IN ('pending', 'failed');

-- name: DiscardVoiceTranscription :exec
UPDATE voice_transcriptions SET status = 'discarded' WHERE id = ? AND owner_user_id = ?;

-- name: PendingVoiceTranscription :exec
UPDATE voice_transcriptions SET status = 'pending', error_message = ''
WHERE id = ? AND owner_user_id = ? AND status = 'failed';
