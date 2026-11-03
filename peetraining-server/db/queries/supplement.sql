-- T48 题库「官方 / 我的」与补充资料提示（每条都带 owner_user_id）。

-- name: KPQuestionSources :many
-- 每个知识点（按主知识点）下官方题与我的题数。
SELECT qk.kp_id,
  CAST(SUM(q.official_question_id IS NOT NULL) AS SIGNED) AS official_count,
  CAST(SUM(q.official_question_id IS NULL) AS SIGNED) AS mine_count
FROM question_kps qk JOIN questions q ON q.id = qk.question_id
WHERE q.bank_id = ? AND q.owner_user_id = ? AND q.status = 'active' AND qk.is_primary = 1
GROUP BY qk.kp_id;

-- name: RecentAttemptsWithKP :many
-- 最近一段时间的作答与题目的主知识点（补充资料提示按板块算得分率）。
SELECT a.question_id, a.is_correct, a.score, a.full_score,
  CAST(COALESCE((SELECT qk.kp_id FROM question_kps qk WHERE qk.question_id = a.question_id ORDER BY qk.is_primary DESC, qk.kp_id LIMIT 1), 0) AS UNSIGNED) AS kp_id
FROM attempts a JOIN questions q ON q.id = a.question_id
WHERE a.owner_user_id = ? AND q.bank_id = ? AND a.answered_at >= ? AND a.answer_mode <> 'self_assess';

-- name: HasTeacherDispute :one
SELECT COUNT(*) FROM disputes d JOIN gradings g ON g.id = d.grading_id JOIN attempts a ON a.id = g.attempt_id JOIN questions q ON q.id = a.question_id
WHERE d.owner_user_id = ? AND q.bank_id = ? AND d.reason = 'teacher_differs';

-- name: CountActiveWrongBook :one
SELECT COUNT(*) FROM wrong_book w JOIN questions q ON q.id = w.question_id
WHERE w.owner_user_id = ? AND q.bank_id = ? AND w.status = 'active';

-- name: ListSupplementDismissals :many
SELECT prompt_key, snooze_until FROM supplement_prompt_dismissals WHERE owner_user_id = ? AND subject_id = ?;

-- name: UpsertSupplementDismissal :exec
INSERT INTO supplement_prompt_dismissals (owner_user_id, subject_id, prompt_key, snooze_until) VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE snooze_until = VALUES(snooze_until);

-- name: UserCreatedAt :one
SELECT created_at FROM users WHERE id = ?;

-- name: HasOfficialSubscription :one
SELECT COUNT(*) FROM bank_subscriptions WHERE owner_user_id = ? AND subject_id = ?;
