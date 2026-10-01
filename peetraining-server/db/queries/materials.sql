-- 资料（T08）。每条查询都带 owner_user_id 归属条件。

-- name: GetBankForSubject :one
SELECT b.* FROM banks b JOIN subjects s ON s.id = b.subject_id
WHERE s.id = ? AND s.owner_user_id = ? AND b.owner_user_id = ?;

-- name: GetMaterialBySha :one
SELECT * FROM materials WHERE owner_user_id = ? AND sha256 = ?;

-- name: CreateMaterial :execlastid
INSERT INTO materials (owner_user_id, bank_id, category, file_name, format, size_bytes, page_count, billed_pages, sha256, status, right_confirmed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: SetMaterialObjectKey :exec
UPDATE materials SET object_key = ? WHERE id = ? AND owner_user_id = ?;

-- name: GetMaterial :one
SELECT m.*, b.subject_id FROM materials m JOIN banks b ON b.id = m.bank_id
WHERE m.id = ? AND m.owner_user_id = ?;

-- name: UpdateMaterialStatus :exec
UPDATE materials SET status = ?, fail_reason = ? WHERE id = ? AND owner_user_id = ?;

-- name: UpdateMaterialPages :exec
UPDATE materials SET page_count = ?, billed_pages = ? WHERE id = ? AND owner_user_id = ?;

-- name: UpdateMaterialCategory :exec
UPDATE materials SET category = ?, sub_type = ? WHERE id = ? AND owner_user_id = ?;

-- name: ListMaterialsByBank :many
SELECT m.*,
  (SELECT COUNT(DISTINCT p.id) FROM papers p WHERE p.source_material_id = m.id) AS paper_count,
  (SELECT COUNT(*) FROM questions q WHERE q.source_material_id = m.id AND q.needs_review = 1 AND q.status = 'active') AS needs_review_count
FROM materials m
WHERE m.bank_id = ? AND m.owner_user_id = ?
ORDER BY m.created_at DESC, m.id DESC;

-- name: UpsertMaterialPage :exec
INSERT INTO material_pages (material_id, page_no, owner_user_id, text, tables, low_confidence)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE text = VALUES(text), tables = VALUES(tables), low_confidence = VALUES(low_confidence);

-- name: ListMaterialPages :many
SELECT * FROM material_pages WHERE material_id = ? AND owner_user_id = ? ORDER BY page_no;

-- name: CountMaterialImpact :one
-- 删除资料前说明连带影响（3.1d）。调用前已用 GetMaterial 核对资料属于当前用户，下面按资料 ID 统计。
SELECT
  (SELECT COUNT(*) FROM questions q WHERE q.source_material_id = sqlc.arg(material_id)) AS question_count,
  (SELECT COUNT(*) FROM attempts a JOIN questions q ON q.id = a.question_id
     WHERE q.source_material_id = sqlc.arg(material_id)) AS attempt_count,
  (SELECT COUNT(*) FROM wrong_book w JOIN questions q ON q.id = w.question_id
     WHERE q.source_material_id = sqlc.arg(material_id) AND w.status = 'active') AS wrong_count,
  (SELECT COUNT(DISTINCT ps.id) FROM paper_sessions ps JOIN papers p ON p.id = ps.paper_id
     WHERE p.source_material_id = sqlc.arg(material_id)) AS paper_session_count;

-- name: ListKPsOnlyFromMaterial :many
-- 只来自这份资料的知识点：它有来源记录，且全部来源都是这份资料。
-- 调用前已核对资料属于当前用户；再限定在资料所在的题库内。
SELECT k.id FROM knowledge_points k
WHERE k.bank_id = sqlc.arg(bank_id)
  AND EXISTS (SELECT 1 FROM kp_sources s WHERE s.kp_id = k.id AND s.material_id = sqlc.arg(material_id))
  AND NOT EXISTS (SELECT 1 FROM kp_sources s WHERE s.kp_id = k.id AND s.material_id <> sqlc.arg(material_id));

-- name: CountKPsFromMaterial :one
SELECT COUNT(DISTINCT kp_id) FROM kp_sources WHERE material_id = ?;

-- name: DeleteQuestionsFromMaterial :exec
DELETE FROM questions WHERE source_material_id = ? AND bank_id = ?;

-- name: DeleteKnowledgePoint :exec
DELETE FROM knowledge_points WHERE id = ? AND bank_id = ?;

-- name: DetachPapersFromMaterial :exec
-- 用它做过的整卷成绩保留：试卷本身删除，paper_sessions.paper_id 置空（外键 SET NULL）。
DELETE FROM papers WHERE source_material_id = ? AND bank_id = ?;

-- name: DeleteMaterial :execrows
DELETE FROM materials WHERE id = ? AND owner_user_id = ?;

-- name: RepointKPSources :exec
-- 保留下来的知识点（其他资料也有）把「出处」改指到另一份资料，避免指向已删除的资料。
-- 三个参数依次是：要删除的资料 ID、题库 ID、要删除的资料 ID。
UPDATE knowledge_points k
JOIN (
  SELECT s.kp_id, MIN(s.material_id) AS alt_material_id FROM kp_sources s
  WHERE s.material_id <> ? GROUP BY s.kp_id
) alt ON alt.kp_id = k.id
SET k.source_material_id = alt.alt_material_id,
    k.source_page = (SELECT MIN(s2.page_no) FROM kp_sources s2 WHERE s2.kp_id = k.id AND s2.material_id = alt.alt_material_id)
WHERE k.bank_id = ? AND k.source_material_id = ?;

-- name: DeleteMaterialPagesAfter :exec
-- 重跑取文本时删掉多出来的旧页（这次页数变少）。
DELETE FROM material_pages WHERE material_id = ? AND owner_user_id = ? AND page_no > ?;
