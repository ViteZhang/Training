-- 拍手写稿与答题规范（T19）。每条查询都带归属条件。

-- name: SetAttemptPhotos :exec
UPDATE attempts SET photo_keys = ? WHERE id = ? AND owner_user_id = ?;

-- name: NormExampleQuestion :one
-- 高分写法：同题型里有采分点的题，用户确认过的采分点、有资料出处的优先。
SELECT q.id, q.stem, q.answer, q.source_material_id, q.source_page
FROM questions q
WHERE q.bank_id = ? AND q.owner_user_id = ? AND q.qtype = ? AND q.status = 'active'
  AND EXISTS (SELECT 1 FROM rubric_points r WHERE r.question_id = q.id)
ORDER BY (SELECT COUNT(*) FROM rubric_points r WHERE r.question_id = q.id AND r.origin = 'user_confirmed') DESC,
  q.source_material_id IS NULL, q.id
LIMIT 1;

-- name: LatestGradingOfQType :one
-- 你上次的写法：最近一次批改完的同题型作答。
SELECT g.id, g.point_results, g.score, g.full_score, a.answer_text, q.id AS question_id, q.stem
FROM gradings g
JOIN attempts a ON a.id = g.attempt_id
JOIN questions q ON q.id = a.question_id
WHERE g.owner_user_id = ? AND g.kind = 'subjective' AND g.status = 'done' AND q.bank_id = ? AND q.qtype = ?
ORDER BY g.id DESC LIMIT 1;

-- name: AnyQuestionOfQType :one
-- 「按结构写一道」用的题：同题型里最近没做过的。
SELECT q.id FROM questions q
WHERE q.bank_id = ? AND q.owner_user_id = ? AND q.qtype = ? AND q.status = 'active'
ORDER BY (SELECT COUNT(*) FROM attempts a WHERE a.question_id = q.id), q.id
LIMIT 1;

-- name: InsertNormGrading :execlastid
INSERT INTO gradings (owner_user_id, attempt_id, kind, trigger_reason, status, point_results, suggestions, model, prompt_version, quota_charged, idempotency_key, finished_at)
VALUES (?, ?, 'norm', 'submit', 'done', ?, ?, ?, ?, 1, ?, ?);
