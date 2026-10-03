-- 作文（T23，PRD 模块 5、11.13）。每条都带 owner_user_id。

-- name: InsertEssay :execlastid
INSERT INTO essays (owner_user_id, subject_id, topic_source, topic_question_id, ai_topic_id, topic_text, required_words, draft_no, parent_essay_id,
  content, timed, idempotency_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?);

-- name: GetEssay :one
SELECT * FROM essays WHERE id = ? AND owner_user_id = ?;

-- name: GetEssayForUpdate :one
SELECT * FROM essays WHERE id = ? AND owner_user_id = ? FOR UPDATE;

-- name: GetEssayByKey :one
SELECT * FROM essays WHERE owner_user_id = ? AND idempotency_key = ?;

-- name: SaveEssayDraft :execrows
-- 草稿（客户端每 5 秒存一次）；提交后不能再改。
UPDATE essays SET content = ?, word_count = ?, duration_seconds = ?, timed = ?, photo_keys = ?
WHERE id = ? AND owner_user_id = ? AND status IN ('draft', 'failed');

-- name: SubmitEssay :exec
UPDATE essays SET status = 'grading', rubric_id = ?, rubric_snapshot = ?, quota_period = ?, submitted_at = ?, grading_round = grading_round + 1,
  fail_reason = NULL
WHERE id = ? AND owner_user_id = ?;

-- name: FinishEssayGrading :exec
UPDATE essays SET status = 'graded', score = ?, full_score = ?, dimension_scores = ?, review = ?, counts_for_estimate = ?, model = ?,
  prompt_version = ?, graded_at = ?
WHERE id = ? AND owner_user_id = ?;

-- name: FailEssayGrading :exec
UPDATE essays SET status = ?, fail_reason = ? WHERE id = ? AND owner_user_id = ?;

-- name: DisputeEssay :exec
UPDATE essays SET disputed = 1, dispute_reason = ?, dispute_note = ?, score_before = score, status = 'grading', grading_round = grading_round + 1
WHERE id = ? AND owner_user_id = ?;

-- name: ListSubjectEssays :many
-- 作文本与作文训练首页：一门课的全部作文，新的在前。
SELECT id, topic_source, topic_question_id, ai_topic_id, topic_text, draft_no, parent_essay_id, word_count, timed, status, score, full_score,
  dimension_scores, counts_for_estimate, rubric_snapshot, created_at, graded_at
FROM essays WHERE owner_user_id = ? AND subject_id = ?
ORDER BY created_at DESC, id DESC;

-- name: ListEstimateEssays :many
-- 作文课预估分（PRD 11.13）：按用户评分细则批改、且以真题限时完成的作文，新的在前。
SELECT score, dimension_scores FROM essays
WHERE owner_user_id = ? AND subject_id = ? AND status = 'graded' AND counts_for_estimate = 1 AND score IS NOT NULL
ORDER BY graded_at DESC, id DESC LIMIT 10;

-- name: InsertEssayAITopic :execlastid
INSERT INTO essay_ai_topics (owner_user_id, subject_id, topic, required_words, note, model, prompt_version, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListEssayAITopics :many
SELECT * FROM essay_ai_topics WHERE owner_user_id = ? AND subject_id = ? ORDER BY id DESC LIMIT 20;

-- name: GetEssayAITopic :one
SELECT * FROM essay_ai_topics WHERE id = ? AND owner_user_id = ?;

-- name: GetEssayTopicQuestion :one
-- 真题作文题：必须是自己题库里的作文题。
SELECT q.id, q.stem, q.required_words, q.exam_year, b.subject_id
FROM questions q JOIN banks b ON b.id = q.bank_id
WHERE q.id = ? AND q.owner_user_id = ? AND q.qtype = 'essay' AND q.status = 'active';

-- name: ListTopicModelEssays :many
-- 范文对比（5.6）：用户导入的同题范文。
SELECT id, title, structure FROM model_essays WHERE owner_user_id = ? AND topic_question_id = ? ORDER BY id;

-- name: GetModelEssay :one
SELECT e.*, m.file_name, q.stem AS topic FROM model_essays e
LEFT JOIN materials m ON m.id = e.source_material_id LEFT JOIN questions q ON q.id = e.topic_question_id
WHERE e.id = ? AND e.owner_user_id = ?;

-- name: ListUserEssayRubrics :many
-- 5.9「你的资料」：从用户资料识别出的评分标准，新的在前。
SELECT r.*, m.file_name FROM essay_rubrics r LEFT JOIN materials m ON m.id = r.source_material_id
WHERE r.owner_user_id = ? AND r.subject_id = ? ORDER BY r.id DESC;

-- name: GetGenericEssayRubric :one
SELECT * FROM essay_rubrics WHERE owner_user_id IS NULL AND source = 'generic' ORDER BY id LIMIT 1;

-- name: ActivateEssayRubric :execrows
UPDATE essay_rubrics SET is_active = 1 WHERE id = ? AND owner_user_id = ?;

-- name: UpdateEssayRubric :execrows
-- 用户编辑评分标准：只影响之后的批改，已批改的作文存了标准快照，分数不变。
UPDATE essay_rubrics SET name = ?, full_score = ?, dimensions = ?, origin = 'user_confirmed' WHERE id = ? AND owner_user_id = ?;
