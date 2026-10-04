-- 作文资料入库（T11）。每条都带 owner_user_id。

-- name: DeactivateEssayRubrics :exec
UPDATE essay_rubrics SET is_active = 0 WHERE owner_user_id = ? AND subject_id = ?;

-- name: InsertEssayRubric :execlastid
INSERT INTO essay_rubrics (owner_user_id, subject_id, source, name, full_score, dimensions, source_material_id, source_page, is_active, origin)
VALUES (?, ?, 'user_material', ?, ?, ?, ?, ?, 1, ?);

-- name: InsertWritingMethod :execlastid
INSERT INTO writing_methods (owner_user_id, bank_id, title, content, dimension, source_material_id, source_page, origin)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertEssayMaterial :execlastid
INSERT INTO essay_materials (owner_user_id, bank_id, theme, content, source_material_id, source_page, origin)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: InsertModelEssay :execlastid
INSERT INTO model_essays (owner_user_id, bank_id, topic_question_id, title, content, structure, source_material_id, source_page)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: SetQuestionRequiredWords :exec
UPDATE questions SET required_words = ? WHERE id = ? AND owner_user_id = ?;

-- name: ListBankEssayTopics :many
SELECT id, stem FROM questions WHERE bank_id = ? AND owner_user_id = ? AND qtype = 'essay' AND status = 'active' ORDER BY id;

-- name: CountSubjectMaterialCategories :one
-- 作文课判断（PRD 11.12）：这门课已解析的资料里作文类占多少。
SELECT
  CAST(COALESCE(SUM(m.category = 'essay'), 0) AS SIGNED) AS essay,
  CAST(COUNT(*) AS SIGNED) AS total
FROM materials m JOIN banks b ON b.id = m.bank_id
WHERE b.subject_id = ? AND m.owner_user_id = ? AND m.category IS NOT NULL AND m.status IN ('parsed', 'partial');

-- name: SetSubjectEssayAuto :exec
-- 自动判断只在用户没改过时生效（essay_set_by = 'user' 后不再自动改）。
UPDATE subjects SET is_essay = ?, essay_set_by = 'auto'
WHERE id = ? AND owner_user_id = ? AND (essay_set_by IS NULL OR essay_set_by = 'auto');
