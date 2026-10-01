-- 确认导入后写入题库（T10）。每条都带 owner_user_id。

-- name: InsertQuestion :execlastid
INSERT INTO questions (owner_user_id, bank_id, qtype, stem, options, answer, answer_origin, analysis, score, difficulty,
  source, exam_year, question_no, is_recollection, source_material_id, source_page, content_hash, needs_review, review_reasons)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertRubricPoint :exec
INSERT INTO rubric_points (owner_user_id, question_id, kp_id, seq, content, keywords, score, origin)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertKnowledgePoint :execlastid
INSERT INTO knowledge_points (owner_user_id, bank_id, parent_id, level, name, original_text, source_material_id, source_page, origin, needs_review, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertKPSource :exec
INSERT IGNORE INTO kp_sources (kp_id, material_id, page_no, owner_user_id) VALUES (?, ?, ?, ?);

-- name: InsertQuestionKP :exec
INSERT IGNORE INTO question_kps (question_id, kp_id, is_primary, owner_user_id) VALUES (?, ?, ?, ?);

-- name: GetRealExamPaper :one
SELECT * FROM papers WHERE bank_id = ? AND owner_user_id = ? AND kind = 'real_exam' AND exam_year = ? LIMIT 1;

-- name: InsertPaper :execlastid
INSERT INTO papers (owner_user_id, bank_id, kind, title, exam_year, full_score, actual_score, duration_minutes, structure, missing_note, source_material_id)
VALUES (?, ?, 'real_exam', ?, ?, ?, ?, 180, ?, ?, ?);

-- name: UpdatePaperScores :exec
UPDATE papers SET full_score = ?, actual_score = ?, structure = ?, missing_note = ? WHERE id = ? AND owner_user_id = ?;

-- name: DeletePaperQuestions :exec
DELETE pq FROM paper_questions pq JOIN papers p ON p.id = pq.paper_id WHERE pq.paper_id = ? AND p.owner_user_id = ?;

-- name: InsertPaperQuestion :exec
INSERT INTO paper_questions (paper_id, seq, question_id, section, score) VALUES (?, ?, ?, ?, ?);

-- name: ListExamQuestionsByYear :many
-- 组真题卷：某年份的真题（回忆版不计入，PRD 3.8），按题型、原题号排序。
SELECT id, qtype, question_no, score, source_material_id FROM questions
WHERE bank_id = ? AND owner_user_id = ? AND source = 'exam' AND exam_year = ? AND status = 'active' AND is_recollection = 0
ORDER BY CAST(question_no AS UNSIGNED), id;

-- name: RecountMaterial :exec
-- 参数依次：资料 ID（题目来源）、资料 ID（知识点来源）、资料 ID、所有者。
UPDATE materials m SET
  question_count = (SELECT COUNT(*) FROM questions q WHERE q.source_material_id = ? AND q.status = 'active'),
  kp_count = (SELECT COUNT(DISTINCT s.kp_id) FROM kp_sources s WHERE s.material_id = ?)
WHERE m.id = ? AND m.owner_user_id = ?;

-- name: RecountKPExamCounts :exec
-- 知识点在真题中出现的次数（3.1、3.8；回忆版不计入）。
UPDATE knowledge_points k SET exam_count = (
  SELECT COUNT(DISTINCT q.id) FROM question_kps qk JOIN questions q ON q.id = qk.question_id
  WHERE qk.kp_id = k.id AND q.source = 'exam' AND q.is_recollection = 0 AND q.status = 'active')
WHERE k.bank_id = ? AND k.owner_user_id = ?;

-- name: ListSubjectsWithoutContent :many
-- 还没导入任何资料的专业课（1.8 提示继续导入）。
SELECT s.id FROM subjects s JOIN banks b ON b.subject_id = s.id
WHERE s.owner_user_id = ? AND NOT EXISTS (SELECT 1 FROM materials m WHERE m.bank_id = b.id)
ORDER BY s.id;
