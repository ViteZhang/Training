-- 题库：知识点、题目、资料（T13）。每条查询都带 owner_user_id 归属条件。

-- name: GetSubjectBank :one
SELECT s.id AS subject_id, s.name, s.full_score, s.is_essay, b.id AS bank_id FROM subjects s JOIN banks b ON b.subject_id = s.id
WHERE s.id = ? AND s.owner_user_id = ? AND b.owner_user_id = ?;

-- name: BankCounts :one
SELECT
  (SELECT COUNT(*) FROM questions q WHERE q.bank_id = sqlc.arg(bank_id) AND q.owner_user_id = sqlc.arg(owner) AND q.status = 'active') AS question_count,
  (SELECT COUNT(*) FROM knowledge_points k WHERE k.bank_id = sqlc.arg(bank_id) AND k.owner_user_id = sqlc.arg(owner) AND k.level = 'point') AS kp_count,
  (SELECT COUNT(*) FROM questions q WHERE q.bank_id = sqlc.arg(bank_id) AND q.owner_user_id = sqlc.arg(owner) AND q.status = 'active' AND q.needs_review = 1) AS question_review_count,
  (SELECT COUNT(*) FROM knowledge_points k WHERE k.bank_id = sqlc.arg(bank_id) AND k.owner_user_id = sqlc.arg(owner) AND k.needs_review = 1) AS kp_review_count,
  (SELECT COUNT(*) FROM materials m WHERE m.bank_id = sqlc.arg(bank_id) AND m.owner_user_id = sqlc.arg(user_id)) AS material_count,
  (SELECT COUNT(*) FROM papers p WHERE p.bank_id = sqlc.arg(bank_id) AND p.owner_user_id = sqlc.arg(owner)) AS paper_count;

-- name: ListBankKPsFull :many
SELECT k.id, k.parent_id, k.level, k.name, k.exam_count, k.needs_review, k.official_kp_id, k.sort_order,
  COALESCE(m.m, 0) AS m, COALESCE(m.state, 'unlearned') AS state
FROM knowledge_points k
LEFT JOIN kp_mastery m ON m.kp_id = k.id AND m.owner_user_id = sqlc.arg(user_id)
WHERE k.bank_id = sqlc.arg(bank_id) AND k.owner_user_id = sqlc.arg(owner)
ORDER BY k.sort_order, k.id;

-- name: GetKP :one
-- 知识点与所在题库、专业课；归属按题库的所有者判断。
SELECT k.*, b.subject_id FROM knowledge_points k JOIN banks b ON b.id = k.bank_id
WHERE k.id = ? AND k.owner_user_id = ?;

-- name: GetKPMastery :one
SELECT * FROM kp_mastery WHERE owner_user_id = ? AND kp_id = ?;

-- name: UpsertKPSelfAssess :exec
-- 三档自评（3.4）：没有作答记录时设 M 与状态，有作答记录时只记录自评（D16）。
INSERT INTO kp_mastery (owner_user_id, kp_id, m, state, viewed, last_self_assess, last_self_assess_at)
VALUES (?, ?, ?, ?, 1, ?, ?)
ON DUPLICATE KEY UPDATE
  m = IF(answered = 1, m, VALUES(m)),
  state = IF(answered = 1, state, VALUES(state)),
  viewed = 1,
  last_self_assess = VALUES(last_self_assess),
  last_self_assess_at = VALUES(last_self_assess_at);

-- name: ListKPRubric :many
SELECT * FROM rubric_points WHERE kp_id = ? AND owner_user_id = ? ORDER BY seq;

-- name: DeleteKPRubric :exec
DELETE FROM rubric_points WHERE kp_id = ? AND owner_user_id = ?;

-- name: ListKPSourcesOfKP :many
SELECT s.material_id, s.page_no, m.file_name FROM kp_sources s JOIN materials m ON m.id = s.material_id
WHERE s.kp_id = ? AND s.owner_user_id = ? ORDER BY s.material_id, s.page_no;

-- name: ListRelatedQuestions :many
SELECT q.id, q.qtype, q.stem, q.source, q.exam_year FROM question_kps qk JOIN questions q ON q.id = qk.question_id
WHERE qk.kp_id = ? AND q.owner_user_id = ? AND q.status = 'active'
ORDER BY qk.is_primary DESC, q.id LIMIT 20;

-- name: UpdateKP :exec
UPDATE knowledge_points SET name = ?, original_text = ?, parent_id = ?, needs_review = ?, origin = ?
WHERE id = ? AND owner_user_id = ?;

-- name: SetKPExplanation :exec
UPDATE knowledge_points SET ai_explanation = ?, ai_explanation_at = ? WHERE id = ? AND owner_user_id = ?;

-- name: DeleteKP :execrows
DELETE FROM knowledge_points WHERE id = ? AND owner_user_id = ?;

-- name: MoveQuestionKPs :exec
-- 合并：把来源知识点的题目关联挂到目标知识点（已挂的跳过）。
INSERT IGNORE INTO question_kps (question_id, kp_id, is_primary, owner_user_id)
SELECT src.question_id, sqlc.arg(target_id), src.is_primary, src.owner_user_id FROM question_kps src WHERE src.kp_id = sqlc.arg(source_id) AND src.owner_user_id = sqlc.arg(owner);

-- name: CopyKPSources :exec
INSERT IGNORE INTO kp_sources (kp_id, material_id, page_no, owner_user_id)
SELECT sqlc.arg(target_id), src.material_id, src.page_no, src.owner_user_id FROM kp_sources src WHERE src.kp_id = sqlc.arg(source_id) AND src.owner_user_id = sqlc.arg(owner);

-- name: MoveKPRubric :exec
UPDATE rubric_points SET kp_id = sqlc.arg(target_id), seq = seq + sqlc.arg(offset) WHERE kp_id = sqlc.arg(source_id) AND owner_user_id = sqlc.arg(owner);

-- name: MoveKPChildren :exec
UPDATE knowledge_points SET parent_id = sqlc.arg(target_id) WHERE parent_id = sqlc.arg(source_id) AND owner_user_id = sqlc.arg(owner);

-- name: InsertKPMasteryCopy :exec
-- 合并掌握度：目标没有记录时沿用来源的；都有时保留作答过的、掌握分取较高者。
INSERT INTO kp_mastery (owner_user_id, kp_id, m, state, viewed, answered, last_self_assess, last_self_assess_at, correct_dates, next_review_on, interval_step)
SELECT owner_user_id, sqlc.arg(target_id), m, state, viewed, answered, last_self_assess, last_self_assess_at, correct_dates, next_review_on, interval_step
FROM kp_mastery src WHERE src.owner_user_id = sqlc.arg(user_id) AND src.kp_id = sqlc.arg(source_id)
ON DUPLICATE KEY UPDATE
  m = GREATEST(kp_mastery.m, VALUES(m)),
  answered = GREATEST(kp_mastery.answered, VALUES(answered)),
  viewed = GREATEST(kp_mastery.viewed, VALUES(viewed));

-- name: ListBankQuestionsFull :many
-- 题目列表（3.1b）：一次取出题库全部题目及作答概况，在服务端筛选、排序与分页（单个题库通常几百到几千题）。
SELECT q.id, q.qtype, LEFT(q.stem, 120) AS stem, q.score, q.source, q.exam_year, q.needs_review, q.generated_from_kp_id, q.created_at,
  CAST(COALESCE((SELECT qk.kp_id FROM question_kps qk WHERE qk.question_id = q.id ORDER BY qk.is_primary DESC, qk.kp_id LIMIT 1), 0) AS UNSIGNED) AS primary_kp_id,
  (SELECT COUNT(*) FROM attempts a WHERE a.question_id = q.id AND a.owner_user_id = sqlc.arg(user_id)) AS attempt_count,
  (SELECT a.score FROM attempts a WHERE a.question_id = q.id AND a.owner_user_id = sqlc.arg(user_id) ORDER BY a.answered_at DESC, a.id DESC LIMIT 1) AS last_score,
  (SELECT MAX(a.answered_at) FROM attempts a WHERE a.question_id = q.id AND a.owner_user_id = sqlc.arg(user_id)) AS last_answered_at,
  EXISTS (SELECT 1 FROM wrong_book w WHERE w.question_id = q.id AND w.owner_user_id = sqlc.arg(user_id) AND w.status = 'active') AS in_wrong_book
FROM questions q
WHERE q.bank_id = sqlc.arg(bank_id) AND q.owner_user_id = sqlc.arg(owner) AND q.status = 'active'
ORDER BY q.id;

-- name: GetQuestionFull :one
SELECT q.*, b.subject_id FROM questions q JOIN banks b ON b.id = q.bank_id
WHERE q.id = ? AND q.owner_user_id = ?;

-- name: ListQuestionRubric :many
SELECT * FROM rubric_points WHERE question_id = ? AND owner_user_id = ? ORDER BY seq;

-- name: DeleteQuestionRubric :exec
DELETE FROM rubric_points WHERE question_id = ? AND owner_user_id = ?;

-- name: ListQuestionKPs :many
SELECT k.id, k.name, qk.is_primary FROM question_kps qk JOIN knowledge_points k ON k.id = qk.kp_id
WHERE qk.question_id = ? AND qk.owner_user_id = ? ORDER BY qk.is_primary DESC, k.id;

-- name: DeleteQuestionKPs :exec
DELETE FROM question_kps WHERE question_id = ? AND owner_user_id = ?;

-- name: ListQuestionAttempts :many
-- 我的每次作答（3.3）与最近一次批改的逐点结果、失分归因。
SELECT a.id, a.answered_at, a.score, a.full_score, a.is_correct,
  (SELECT g.point_results FROM gradings g WHERE g.attempt_id = a.id AND g.status = 'done' ORDER BY g.id DESC LIMIT 1) AS point_results,
  (SELECT g.loss FROM gradings g WHERE g.attempt_id = a.id AND g.status = 'done' ORDER BY g.id DESC LIMIT 1) AS loss
FROM attempts a WHERE a.question_id = ? AND a.owner_user_id = ?
ORDER BY a.answered_at DESC, a.id DESC LIMIT 50;

-- name: InWrongBook :one
SELECT EXISTS (SELECT 1 FROM wrong_book WHERE owner_user_id = ? AND question_id = ? AND status = 'active');

-- name: UpdateQuestion :exec
UPDATE questions SET qtype = ?, stem = ?, options = ?, answer = ?, answer_origin = ?, analysis = ?, score = ?, exam_year = ?, source = ?,
  content_hash = ?, rubric_version = ?, needs_review = ?, review_reasons = ?
WHERE id = ? AND owner_user_id = ?;

-- name: DeleteQuestion :execrows
DELETE FROM questions WHERE id = ? AND owner_user_id = ?;

-- name: GetMaterialPage :one
SELECT p.*, m.file_name, m.page_count FROM material_pages p JOIN materials m ON m.id = p.material_id
WHERE p.material_id = ? AND p.page_no = ? AND p.owner_user_id = ? AND m.owner_user_id = ?;

-- name: ListKPsOnPage :many
SELECT k.id, k.name FROM kp_sources s JOIN knowledge_points k ON k.id = s.kp_id
WHERE s.material_id = ? AND s.page_no = ? AND s.owner_user_id = ? ORDER BY k.id LIMIT 30;

-- name: SearchKPs :many
SELECT id, name, original_text, parent_id FROM knowledge_points
WHERE bank_id = sqlc.arg(bank_id) AND owner_user_id = sqlc.arg(owner) AND level = 'point'
  AND (name LIKE sqlc.arg(name_pattern) OR original_text LIKE sqlc.arg(text_pattern))
ORDER BY id LIMIT 20;

-- name: SearchQuestions :many
SELECT id, qtype, stem FROM questions
WHERE bank_id = sqlc.arg(bank_id) AND owner_user_id = sqlc.arg(owner) AND status = 'active' AND stem LIKE sqlc.arg(pattern)
ORDER BY id LIMIT 20;

-- name: SearchMaterialPages :many
SELECT p.material_id, p.page_no, p.text, m.file_name FROM material_pages p JOIN materials m ON m.id = p.material_id
WHERE m.bank_id = sqlc.arg(bank_id) AND p.owner_user_id = sqlc.arg(user_id) AND p.text LIKE sqlc.arg(pattern)
ORDER BY p.material_id, p.page_no LIMIT 20;
