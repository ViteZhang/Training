-- 背诵（T20）。每条查询都带归属条件。

-- name: ListReciteCandidates :many
-- 可背的知识点：有原文表述的知识点，带掌握分与背诵复习日。
SELECT k.id, k.name, k.parent_id, k.original_text, k.source_material_id, k.source_page,
  COALESCE(m.m, 0) AS m, m.recite_next_review_on, COALESCE(m.recite_interval_step, 0) AS recite_interval_step
FROM knowledge_points k
LEFT JOIN kp_mastery m ON m.kp_id = k.id AND m.owner_user_id = k.owner_user_id
WHERE k.bank_id = ? AND k.owner_user_id = ? AND k.level = 'point'
  AND k.original_text IS NOT NULL AND k.original_text <> ''
ORDER BY k.sort_order, k.id;

-- name: GetReciteRecordByKey :one
SELECT * FROM recite_records WHERE owner_user_id = ? AND idempotency_key = ?;

-- name: InsertReciteRecord :execlastid
INSERT INTO recite_records (owner_user_id, kp_id, practice_session_id, mode, result, coverage, idempotency_key, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpsertKPRecite :exec
-- 背诵只改掌握分与背诵复习日，不算作答验证（PRD 11.1 补充）。
INSERT INTO kp_mastery (owner_user_id, kp_id, m, state, viewed, recite_next_review_on, recite_interval_step)
VALUES (?, ?, ?, ?, 1, ?, ?)
ON DUPLICATE KEY UPDATE m = VALUES(m), state = VALUES(state), viewed = 1,
  recite_next_review_on = VALUES(recite_next_review_on), recite_interval_step = VALUES(recite_interval_step);

-- name: ListSessionRecites :many
SELECT kp_id, result, mode FROM recite_records WHERE practice_session_id = ? AND owner_user_id = ? ORDER BY id;

-- name: PreviousFinishedSession :one
SELECT summary FROM practice_sessions
WHERE owner_user_id = ? AND subject_id = ? AND kind = ? AND status = 'finished' AND id < ?
ORDER BY id DESC LIMIT 1;
