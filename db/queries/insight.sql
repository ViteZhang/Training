-- 考情分析、知识图谱、作文知识库（T14）。每条查询都带 owner_user_id。

-- name: ListBankExamQuestions :many
SELECT id, qtype, score, exam_year, is_recollection, source_material_id, LEFT(stem, 200) AS stem
FROM questions WHERE bank_id = ? AND owner_user_id = ? AND source = 'exam' AND status = 'active'
ORDER BY exam_year, id;

-- name: ListBankQuestionKPLinks :many
SELECT qk.question_id, qk.kp_id FROM question_kps qk JOIN questions q ON q.id = qk.question_id
WHERE q.bank_id = ? AND q.owner_user_id = ? AND q.status = 'active';

-- name: GetBankInsight :one
SELECT exam_style, exam_style_key, relations_generated_at FROM banks WHERE id = ? AND owner_user_id = ?;

-- name: SetBankExamStyle :exec
UPDATE banks SET exam_style = ?, exam_style_key = ? WHERE id = ? AND owner_user_id = ?;

-- name: MarkRelationsGenerated :exec
UPDATE banks SET relations_generated_at = ? WHERE id = ? AND owner_user_id = ?;

-- name: ListKPRelations :many
SELECT * FROM kp_relations WHERE bank_id = ? AND owner_user_id = ? ORDER BY id;

-- name: InsertKPRelation :execlastid
-- kp_a_id < kp_b_id，同一对只存一条。
INSERT INTO kp_relations (owner_user_id, bank_id, kp_a_id, kp_b_id, relation_type, origin) VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE relation_type = VALUES(relation_type), origin = VALUES(origin), id = LAST_INSERT_ID(id);

-- name: GetKPRelation :one
SELECT * FROM kp_relations WHERE id = ? AND owner_user_id = ?;

-- name: DeleteKPRelation :execrows
DELETE FROM kp_relations WHERE id = ? AND owner_user_id = ?;

-- name: ListWritingMethods :many
SELECT w.*, m.file_name FROM writing_methods w LEFT JOIN materials m ON m.id = w.source_material_id
WHERE w.bank_id = ? AND w.owner_user_id = ? ORDER BY w.id;

-- name: ListEssayMaterialsKB :many
SELECT e.*, m.file_name FROM essay_materials e LEFT JOIN materials m ON m.id = e.source_material_id
WHERE e.bank_id = ? AND e.owner_user_id = ? ORDER BY e.favorite DESC, e.theme, e.id;

-- name: ListModelEssaysKB :many
SELECT e.id, e.title, e.topic_question_id, e.structure, e.source_material_id, e.source_page, m.file_name, q.stem AS topic
FROM model_essays e LEFT JOIN materials m ON m.id = e.source_material_id LEFT JOIN questions q ON q.id = e.topic_question_id
WHERE e.bank_id = ? AND e.owner_user_id = ? ORDER BY e.id;

-- name: ListEssayTopicsKB :many
SELECT q.id, q.stem, q.exam_year, q.required_words,
  (SELECT COUNT(*) FROM model_essays e WHERE e.topic_question_id = q.id) AS model_essay_count
FROM questions q WHERE q.bank_id = ? AND q.owner_user_id = ? AND q.qtype = 'essay' AND q.status = 'active'
ORDER BY q.exam_year DESC, q.id;

-- name: GetActiveEssayRubric :one
-- 当前评分标准：用户资料里识别出的优先，没有时用通用五维度（PRD 11.13）。
SELECT r.*, m.file_name FROM essay_rubrics r LEFT JOIN materials m ON m.id = r.source_material_id
WHERE (r.owner_user_id = sqlc.arg(user_id) AND r.subject_id = sqlc.arg(subject_id) AND r.is_active = 1) OR (r.owner_user_id IS NULL AND r.source = 'generic')
ORDER BY r.owner_user_id IS NULL, r.id DESC LIMIT 1;

-- name: SetEssayMaterialFavorite :execrows
UPDATE essay_materials SET favorite = ? WHERE id = ? AND owner_user_id = ?;

-- name: EssayMaterialExists :one
SELECT EXISTS (SELECT 1 FROM essay_materials WHERE id = ? AND owner_user_id = ?);
