-- 缺答案处理（PRD v3 1.7、1.7c、3.3b、4.7b、11.16，10 月 26 日更新）。

-- name: SetQuestionAnswerSource :exec
-- 入库时记下「来自你的资料」的答案怎么来的与出处。
UPDATE questions SET answer_match = ?, answer_material_id = ?, answer_page = ? WHERE id = ? AND owner_user_id = ?;

-- name: SetQuestionAnswer :exec
-- 写入新的参考答案：1.7c 答案文件配对（imported）、AI 参考答案（ai_generated，记 ai_answer_at）。
UPDATE questions SET answer = ?, answer_origin = ?, answer_match = ?, answer_material_id = ?, answer_page = ?,
  analysis = IFNULL(sqlc.narg(analysis), analysis), ai_answer_at = ?, ai_answer_outcome = NULL, ai_answer_reports = 0
WHERE id = ? AND owner_user_id = ?;

-- name: ConfirmAIAnswer :execrows
-- 3.3b、4.7b「答案没问题，确认」：AI 参考答案变为「你已确认」，之后的批改计入预估分（确认前的不追溯）。
UPDATE questions SET answer_origin = 'user_confirmed', ai_answer_outcome = 'confirmed'
WHERE id = ? AND owner_user_id = ? AND answer_origin = 'ai_generated';

-- name: MarkAIAnswerEdited :exec
-- 用户改了 AI 参考答案（「我来改」），7.5 统计修改率。
UPDATE questions SET ai_answer_outcome = 'edited' WHERE id = ? AND owner_user_id = ? AND ai_answer_at IS NOT NULL;

-- name: ReportAIAnswer :execrows
-- 客观题 AI 答案被报错：计数加 1；到 3 次撤回（PRD v3 11.16 第 7 条）。
UPDATE questions SET ai_answer_reports = ai_answer_reports + 1 WHERE id = ? AND owner_user_id = ? AND answer_origin = 'ai_generated';

-- name: GetAIAnswerReports :one
SELECT ai_answer_reports FROM questions WHERE id = ? AND owner_user_id = ?;

-- name: WithdrawAIAnswer :exec
-- 撤回 AI 答案，回到「缺答案」。
UPDATE questions SET answer = NULL, answer_origin = NULL, analysis = NULL, answer_match = NULL, answer_material_id = NULL, answer_page = NULL
WHERE id = ? AND owner_user_id = ? AND answer_origin = 'ai_generated';

-- name: ConfirmQuestionRubric :exec
-- 确认 AI 参考答案时一并确认它的采分点。
UPDATE rubric_points SET origin = 'user_confirmed' WHERE question_id = ? AND owner_user_id = ?;

-- name: ListBankAnsweredQuestions :many
-- 同一门课里有可信答案的题（从用户其他资料匹配答案的候选，PRD v3 11.16 第 2 条）。
SELECT id, qtype, stem, content_hash, exam_year, question_no, answer, answer_material_id, answer_page, source_material_id, source_page
FROM questions
WHERE bank_id = ? AND owner_user_id = ? AND status = 'active' AND answer IS NOT NULL AND answer <> ''
  AND (answer_origin IS NULL OR answer_origin <> 'ai_generated') AND source <> 'ai_generated'
LIMIT 3000;

-- name: ListBankImportAnswers :many
-- 同一门课以前导入过的答案（答案文件、真题汇编后附的答案），按年份 + 题号配对。
SELECT a.id, a.material_id, a.exam_year, a.question_no, a.stem, a.answer, a.page
FROM import_answers a JOIN import_jobs j ON j.id = a.job_id
WHERE j.bank_id = ? AND a.owner_user_id = ? AND a.job_id <> ?
ORDER BY a.id DESC
LIMIT 3000;

-- name: ListBankKPTexts :many
-- 同一门课里有原文表述的知识点：名词解释按名称匹配，其他题交给模型从候选原文里找。
SELECT id, name, original_text, source_material_id, source_page
FROM knowledge_points
WHERE bank_id = ? AND owner_user_id = ? AND level = 'point' AND original_text IS NOT NULL AND original_text <> ''
LIMIT 3000;

-- name: ListBankMissingAnswerQuestions :many
-- 1.7c 补充答案文件的配对对象：题库里还缺答案或只有 AI 参考答案的题。
SELECT id, qtype, stem, exam_year, question_no, score, answer_origin
FROM questions
WHERE bank_id = ? AND owner_user_id = ? AND status = 'active' AND source <> 'ai_generated'
  AND (answer IS NULL OR answer = '' OR answer_origin = 'ai_generated')
ORDER BY exam_year DESC, id
LIMIT 2000;

-- name: InsertAnswerPairing :exec
INSERT INTO answer_pairings (owner_user_id, job_id, target_kind, target_id, import_answer_id, method)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE import_answer_id = IF(status = 'proposed', VALUES(import_answer_id), import_answer_id),
  method = IF(status = 'proposed', VALUES(method), method);

-- name: DeleteProposedPairings :exec
DELETE FROM answer_pairings WHERE job_id = ? AND owner_user_id = ? AND status = 'proposed';

-- name: ListAnswerPairings :many
SELECT * FROM answer_pairings WHERE job_id = ? AND owner_user_id = ? ORDER BY id;

-- name: GetAnswerPairing :one
SELECT * FROM answer_pairings WHERE id = ? AND job_id = ? AND owner_user_id = ?;

-- name: UpdateAnswerPairing :exec
UPDATE answer_pairings SET import_answer_id = ?, method = ? WHERE id = ? AND owner_user_id = ? AND status = 'proposed';

-- name: MarkPairingApplied :exec
UPDATE answer_pairings SET status = 'applied' WHERE id = ? AND owner_user_id = ?;

-- name: GetImportAnswer :one
SELECT * FROM import_answers WHERE id = ? AND owner_user_id = ?;

-- name: CountReferenceOnlyAttempts :one
-- 3.3b：按这份 AI 参考答案批改过的次数（不计入预估分）。
SELECT COUNT(*) FROM attempts WHERE question_id = ? AND owner_user_id = ? AND reference_only = 1 AND score IS NOT NULL;

-- name: LatestReferenceOnlyAttempt :one
SELECT score, full_score FROM attempts WHERE question_id = ? AND owner_user_id = ? AND reference_only = 1 AND score IS NOT NULL
ORDER BY answered_at DESC, id DESC LIMIT 1;

-- name: SetAttemptReferenceOnly :exec
UPDATE attempts SET reference_only = 1 WHERE id = ? AND owner_user_id = ?;

-- name: GetBankSubjectInfo :one
-- 生成 AI 参考答案时要的专业课名称与参考教材（PRD v3 1.3、11.16 第 5 条）。
SELECT s.id, s.name, s.textbooks FROM banks b JOIN subjects s ON s.id = b.subject_id WHERE b.id = ? AND b.owner_user_id = ?;

-- name: SetQuestionRubricPending :exec
-- 答案换了以后采分点重新提取：版本加 1，标「采分点待确认」（历史批改按当时的快照，不受影响）。
UPDATE questions SET rubric_version = rubric_version + 1, needs_review = 1, review_reasons = ? WHERE id = ? AND owner_user_id = ?;

-- name: SetImportJobTarget :exec
UPDATE import_jobs SET target_job_id = ? WHERE id = ? AND owner_user_id = ?;

-- name: MarkQuestionAIAnswer :exec
UPDATE questions SET ai_answer_at = ? WHERE id = ? AND owner_user_id = ?;
