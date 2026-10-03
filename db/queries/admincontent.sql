-- 后台授权查看（PRD 10.1、ADR 0006）：只有这里的查询读用户内容原文。调用方必须先经 notify.RecordAccess 校验授权、
-- 记 content_access_logs 并通知用户，再用授权所属的用户 ID 读（每条查询都带归属条件）。

-- name: GrantedMaterial :one
SELECT id, format, page_count, status, created_at FROM materials WHERE id = ? AND owner_user_id = ?;

-- name: GrantedMaterialPages :many
SELECT page_no, text FROM material_pages WHERE material_id = ? AND owner_user_id = ? ORDER BY page_no LIMIT 30;

-- name: GrantedGrading :one
-- 异议那次批改：题目、采分点快照（来源、识别出的、用户补充后的）、作答原文、逐点结果与分数。
SELECT g.id, g.score, g.full_score, g.rubric_snapshot, g.point_results, a.answer_text, q.stem, q.qtype
FROM gradings g JOIN attempts a ON a.id = g.attempt_id JOIN questions q ON q.id = a.question_id
WHERE g.id = ? AND g.owner_user_id = ?;
