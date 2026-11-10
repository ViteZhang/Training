-- 官方题库（T30）。官方内容在 source = official 的题库里，owner_user_id 为空；用户添加后复制到用户自己的题库。
-- 用户侧的查询都带 owner_user_id 归属条件。

-- ---------- 立项与进度（7.11） ----------

-- name: InsertOfficialBank :execlastid
INSERT INTO banks (source, owner_user_id, subject_id, title, school_major_tag, subject_code) VALUES ('official', NULL, NULL, ?, ?, ?);

-- name: InsertOfficialProject :execlastid
INSERT INTO official_projects (catalog_course_id, school, major, subject_code, subject_name, bank_id, editor_ids, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: OfficialProjectOfCourse :one
-- 一门考试科目只立一个项目（T50）。
SELECT id FROM official_projects WHERE catalog_course_id = ? ORDER BY id LIMIT 1;

-- name: FindCatalogCourseForProject :one
-- 旧版后台立项只传院校、代码、名称：找到对应的考试科目挂上。院校写「全国统考」的找统考科目。
SELECT id FROM catalog_courses
WHERE (scope = 'school' AND school = sqlc.arg(school) AND (subject_code = sqlc.arg(code) OR (subject_code IS NULL AND subject_name = sqlc.arg(name))))
   OR (scope = 'national' AND sqlc.arg(national) AND subject_code = sqlc.arg(code))
ORDER BY exam_year IS NOT NULL, id LIMIT 1;

-- name: ListOfficialProjects :many
SELECT p.*,
  (SELECT COUNT(*) FROM knowledge_points k WHERE k.bank_id = p.bank_id AND k.owner_user_id IS NULL AND k.level = 'point') AS kp_count,
  (SELECT COUNT(*) FROM questions q WHERE q.bank_id = p.bank_id AND q.owner_user_id IS NULL AND q.status = 'active') AS question_count,
  (SELECT COUNT(*) FROM questions q WHERE q.bank_id = p.bank_id AND q.owner_user_id IS NULL AND q.status = 'active' AND q.source = 'official' AND q.exam_year IS NOT NULL) AS exam_count,
  CAST(COALESCE((SELECT v.version FROM official_versions v WHERE v.project_id = p.id AND v.rolled_back_at IS NULL ORDER BY v.id DESC LIMIT 1), '') AS CHAR) AS version,
  (SELECT COUNT(*) FROM bank_subscriptions s WHERE s.bank_id = p.bank_id) AS subscribers
FROM official_projects p ORDER BY p.id DESC;

-- name: GetOfficialProject :one
SELECT * FROM official_projects WHERE id = ?;

-- name: GetOfficialProjectByBank :one
SELECT * FROM official_projects WHERE bank_id = ?;

-- name: UpdateOfficialProject :exec
UPDATE official_projects SET editor_ids = ?, authorized_materials = ?, stage = ? WHERE id = ?;

-- name: SetOfficialProjectStage :exec
UPDATE official_projects SET stage = ? WHERE id = ?;

-- ---------- 官方内容 ----------

-- name: ListOfficialKPs :many
SELECT id, parent_id, level, name, original_text, reference_text, sort_order FROM knowledge_points WHERE bank_id = ? AND owner_user_id IS NULL ORDER BY level, sort_order, id;

-- name: InsertOfficialKP :execlastid
INSERT INTO knowledge_points (owner_user_id, bank_id, parent_id, level, name, original_text, reference_text, origin, sort_order) VALUES (NULL, ?, ?, ?, ?, ?, ?, 'official', ?);

-- name: UpdateOfficialKP :exec
UPDATE knowledge_points SET name = ?, original_text = ?, reference_text = ? WHERE id = ? AND bank_id = ? AND owner_user_id IS NULL;

-- name: DeleteOfficialKP :exec
DELETE FROM knowledge_points WHERE id = ? AND bank_id = ? AND owner_user_id IS NULL;

-- name: ListOfficialQuestions :many
SELECT id, qtype, stem, options, answer, answer_origin, analysis, reference_text, score, exam_year, status, answer_snapshot_id, group_label, group_majors FROM questions WHERE bank_id = ? AND owner_user_id IS NULL ORDER BY id;

-- name: InsertOfficialQuestion :execlastid
-- answer_origin：official 团队给的答案；ai_generated 由 AI 生成、经编辑审核（用户端显示「AI 参考答案」，T49）。
-- group_label / group_majors：限选题（按专业分组的题，T51）；为空是必答题。
INSERT INTO questions (owner_user_id, bank_id, qtype, stem, options, answer, answer_origin, analysis, reference_text, score, source, exam_year, content_hash, group_label, group_majors)
VALUES (NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'official', ?, ?, ?, ?);

-- name: UpdateOfficialQuestion :exec
UPDATE questions SET qtype = ?, stem = ?, options = ?, answer = ?, answer_origin = ?, analysis = ?, reference_text = ?, score = ?, exam_year = ?, content_hash = ?,
  group_label = ?, group_majors = ?, rubric_version = rubric_version + 1
WHERE id = ? AND bank_id = ? AND owner_user_id IS NULL;

-- name: SetOfficialQuestionStatus :exec
UPDATE questions SET status = ? WHERE id = ? AND bank_id = ? AND owner_user_id IS NULL;

-- name: ListOfficialRubric :many
-- 官方题库的全部采分点（题目的与知识点的）。
SELECT r.id, r.question_id, r.kp_id, r.seq, r.content, r.keywords, r.score FROM rubric_points r
WHERE r.owner_user_id IS NULL AND (r.question_id IN (SELECT q.id FROM questions q WHERE q.bank_id = sqlc.arg(bank_id) AND q.owner_user_id IS NULL)
   OR r.kp_id IN (SELECT k.id FROM knowledge_points k WHERE k.bank_id = sqlc.arg(bank_id) AND k.owner_user_id IS NULL))
ORDER BY r.question_id, r.kp_id, r.seq;

-- name: DeleteOfficialQuestionRubric :exec
DELETE FROM rubric_points WHERE question_id = ? AND owner_user_id IS NULL;

-- name: DeleteOfficialKPRubric :exec
DELETE FROM rubric_points WHERE kp_id = ? AND owner_user_id IS NULL;

-- name: InsertOfficialRubricPoint :exec
INSERT INTO rubric_points (owner_user_id, question_id, kp_id, seq, content, keywords, score, origin) VALUES (?, ?, ?, ?, ?, ?, ?, 'official');

-- name: ListOfficialQuestionKPs :many
SELECT qk.question_id, qk.kp_id, qk.is_primary FROM question_kps qk JOIN questions q ON q.id = qk.question_id WHERE q.bank_id = ? AND q.owner_user_id IS NULL;

-- name: DeleteOfficialQuestionKPs :exec
DELETE FROM question_kps WHERE question_id = ? AND owner_user_id IS NULL;

-- name: InsertOfficialQuestionKP :exec
INSERT IGNORE INTO question_kps (question_id, kp_id, is_primary, owner_user_id) VALUES (?, ?, ?, ?);

-- ---------- 草稿与审核（7.12、7.13） ----------

-- name: InsertOfficialDraft :execlastid
INSERT INTO official_drafts (project_id, editor_id, entity_type, entity_id, change_type, payload) VALUES (?, ?, ?, ?, ?, ?);

-- name: UpdateOfficialDraft :execrows
-- 草稿和被退回的可以改；改完回到草稿。
UPDATE official_drafts SET change_type = ?, entity_id = ?, payload = ?, status = 'draft' WHERE id = ? AND editor_id = ? AND status IN ('draft', 'rejected');

-- name: DeleteOfficialDraft :execrows
DELETE FROM official_drafts WHERE id = ? AND editor_id = ? AND status IN ('draft', 'rejected');

-- name: GetOfficialDraft :one
SELECT * FROM official_drafts WHERE id = ?;

-- name: ListOfficialDrafts :many
SELECT * FROM official_drafts
WHERE project_id = ? AND (CAST(sqlc.arg(status) AS CHAR) = '' OR status = CAST(sqlc.arg(status) AS CHAR))
  AND (CAST(sqlc.arg(editor_id) AS UNSIGNED) = 0 OR editor_id = CAST(sqlc.arg(editor_id) AS UNSIGNED))
ORDER BY id DESC LIMIT 500;

-- name: SubmitOfficialDraft :execrows
UPDATE official_drafts SET status = 'submitted', submitted_at = ?, reject_reason = NULL WHERE id = ? AND editor_id = ? AND status IN ('draft', 'rejected');

-- name: SetOfficialDraftStatus :exec
UPDATE official_drafts SET status = ?, reject_reason = ? WHERE id = ?;

-- name: SetOfficialDraftPayload :exec
UPDATE official_drafts SET payload = ? WHERE id = ?;

-- name: CountEditorReviews :one
-- 编辑已提交过几次（新编辑前 3 次全量审核）。
SELECT COUNT(*) FROM review_tasks t JOIN official_drafts d ON d.id = t.draft_id WHERE d.editor_id = ?;

-- name: InsertReviewTask :execlastid
INSERT INTO review_tasks (draft_id, mode, decision, decided_at) VALUES (?, ?, ?, ?);

-- name: ListPendingReviews :many
SELECT t.id, t.draft_id, t.mode, t.created_at, d.project_id, d.editor_id, d.entity_type, d.entity_id, d.change_type, d.payload
FROM review_tasks t JOIN official_drafts d ON d.id = t.draft_id
WHERE t.decision = 'pending' AND (CAST(sqlc.arg(project_id) AS UNSIGNED) = 0 OR d.project_id = CAST(sqlc.arg(project_id) AS UNSIGNED))
ORDER BY t.id LIMIT 200;

-- name: GetReviewTask :one
SELECT t.*, d.project_id, d.editor_id, d.entity_type, d.entity_id, d.change_type, d.payload, d.status AS draft_status
FROM review_tasks t JOIN official_drafts d ON d.id = t.draft_id WHERE t.id = ?;

-- name: DecideReviewTask :execrows
UPDATE review_tasks SET reviewer_id = ?, decision = ?, reason = ?, edited_payload = ?, decided_at = ? WHERE id = ? AND decision = 'pending';

-- name: ListApprovedDrafts :many
SELECT * FROM official_drafts WHERE project_id = ? AND status = 'approved' ORDER BY id;

-- ---------- 版本（7.14） ----------

-- name: InsertOfficialVersion :execlastid
INSERT INTO official_versions (project_id, version, changelog, snapshot, published_by, published_at) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListOfficialVersions :many
SELECT id, project_id, version, changelog, published_by, published_at, rolled_back_at FROM official_versions WHERE project_id = ? ORDER BY id DESC;

-- name: GetOfficialVersion :one
SELECT * FROM official_versions WHERE id = ? AND project_id = ?;

-- name: MarkOfficialVersionRolledBack :execrows
UPDATE official_versions SET rolled_back_at = ? WHERE id = ? AND rolled_back_at IS NULL;

-- ---------- 需求洞察（7.10，只用统计） ----------

-- name: StatDemand :many
-- 按「院校 + 专业课代码」聚合（T50）：选了全国统考科目的不分院校，院校记为空串；没记院校的旧专业课也是空串。
-- 自建用户数、平均题数、本周新增、填了目标院校的人数、付费人数（只有计数）。
SELECT CAST(IF(cc.scope = 'national', '', COALESCE(s.school, '')) AS CHAR) AS school, s.code,
  COUNT(DISTINCT s.owner_user_id) AS users,
  CAST(COALESCE(AVG((SELECT COUNT(*) FROM questions q WHERE q.bank_id = b.id AND q.owner_user_id = s.owner_user_id AND q.status = 'active')), 0) AS SIGNED) AS avg_questions,
  CAST(SUM(s.created_at >= sqlc.arg(week_start)) AS SIGNED) AS week_new,
  CAST(SUM(EXISTS (SELECT 1 FROM study_profiles p WHERE p.user_id = s.owner_user_id AND p.target_school_major IS NOT NULL AND p.target_school_major <> '')) AS SIGNED) AS with_target,
  CAST(SUM(EXISTS (SELECT 1 FROM orders o WHERE o.owner_user_id = s.owner_user_id AND o.status IN ('paid', 'refunding'))) AS SIGNED) AS paid,
  CAST(COALESCE(AVG((SELECT COUNT(*) FROM import_items i WHERE i.owner_user_id = s.owner_user_id AND i.needs_review = 1 AND i.status = 'pending')), 0) AS SIGNED) AS avg_review
FROM subjects s JOIN banks b ON b.subject_id = s.id
LEFT JOIN catalog_courses cc ON cc.id = s.catalog_course_id
WHERE s.code IS NOT NULL AND s.code <> ''
GROUP BY 1, s.code ORDER BY users DESC LIMIT 200;

-- name: UpsertDemandMark :exec
INSERT INTO official_demand_marks (school, subject_code, status, marked_by) VALUES (?, ?, 'unplanned', ?) ON DUPLICATE KEY UPDATE marked_by = VALUES(marked_by), marked_at = CURRENT_TIMESTAMP(3);

-- name: DeleteDemandMark :exec
DELETE FROM official_demand_marks WHERE school = ? AND subject_code = ?;

-- name: ListDemandMarks :many
SELECT school, subject_code FROM official_demand_marks;

-- ---------- 用户添加官方题库（11.15） ----------

-- name: ListPublishedOfficialBanks :many
SELECT b.id AS bank_id, b.title, b.subject_code, p.id AS project_id, p.school, p.major, p.subject_name,
  CAST(COALESCE((SELECT v.version FROM official_versions v WHERE v.project_id = p.id AND v.rolled_back_at IS NULL ORDER BY v.id DESC LIMIT 1), '') AS CHAR) AS version,
  (SELECT COUNT(*) FROM knowledge_points k WHERE k.bank_id = b.id AND k.owner_user_id IS NULL AND k.level = 'point') AS kp_count,
  (SELECT COUNT(*) FROM questions q WHERE q.bank_id = b.id AND q.owner_user_id IS NULL AND q.status = 'active') AS question_count
FROM banks b JOIN official_projects p ON p.bank_id = b.id
WHERE b.source = 'official' AND EXISTS (SELECT 1 FROM official_versions v WHERE v.project_id = p.id AND v.rolled_back_at IS NULL)
ORDER BY b.id;

-- name: ListMySubscriptions :many
SELECT bank_id, subject_id, added_at FROM bank_subscriptions WHERE owner_user_id = ?;

-- name: InsertSubscription :exec
INSERT INTO bank_subscriptions (owner_user_id, bank_id, subject_id) VALUES (?, ?, ?);

-- name: DeleteSubscription :execrows
DELETE FROM bank_subscriptions WHERE owner_user_id = ? AND bank_id = ?;

-- name: GetSubscription :one
SELECT * FROM bank_subscriptions WHERE owner_user_id = ? AND bank_id = ?;

-- name: ListSubscribers :many
SELECT owner_user_id, subject_id FROM bank_subscriptions WHERE bank_id = ?;

-- name: ListUserKPCopies :many
-- 用户题库里的全部知识点（含自建的，同名合并要用）。
SELECT id, parent_id, level, name, original_text, reference_text, origin, official_kp_id, official_hash FROM knowledge_points WHERE bank_id = ? AND owner_user_id = ?;

-- name: InsertUserKPCopy :execlastid
INSERT INTO knowledge_points (owner_user_id, bank_id, parent_id, level, name, original_text, reference_text, origin, official_kp_id, official_hash, official_new_until, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, 'official', ?, ?, ?, ?);

-- name: UpdateUserKPCopy :exec
UPDATE knowledge_points SET name = ?, original_text = ?, reference_text = ?, official_hash = ? WHERE id = ? AND owner_user_id = ?;

-- name: LinkUserKP :exec
-- 同名知识点合并：保留用户自己的表述和采分点，只记下对应的官方知识点（PRD 11.15）。
UPDATE knowledge_points SET official_kp_id = ? WHERE id = ? AND owner_user_id = ?;

-- name: UnlinkUserKP :exec
UPDATE knowledge_points SET official_kp_id = NULL, official_hash = NULL, official_new_until = NULL, origin = IF(origin = 'official', 'user_confirmed', origin) WHERE id = ? AND owner_user_id = ?;

-- name: DeleteUserKP :exec
DELETE FROM knowledge_points WHERE id = ? AND owner_user_id = ?;

-- name: ListUserQuestionCopies :many
SELECT id, official_question_id, qtype, stem, answer, answer_origin, analysis, reference_text, status, official_hash, group_label, group_majors, exam_year FROM questions WHERE bank_id = ? AND owner_user_id = ? AND official_question_id IS NOT NULL;

-- name: InsertUserQuestionCopy :execlastid
INSERT INTO questions (owner_user_id, bank_id, qtype, stem, options, answer, answer_origin, analysis, reference_text, score, source, exam_year, content_hash, official_question_id, official_hash,
  group_label, group_majors)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'official', ?, ?, ?, ?, ?, ?);

-- name: UpdateUserQuestionCopy :exec
UPDATE questions SET qtype = ?, stem = ?, options = ?, answer = ?, answer_origin = ?, analysis = ?, reference_text = ?, score = ?, exam_year = ?, content_hash = ?, official_hash = ?, status = 'active',
  group_label = ?, group_majors = ?, rubric_version = rubric_version + 1
WHERE id = ? AND owner_user_id = ?;

-- name: GetSubjectMyMajors :one
-- 报考方向（T51）：复制官方题库时限选题只留与它匹配的组。
SELECT my_majors FROM subjects WHERE id = ? AND owner_user_id = ?;

-- name: SubscriptionsOfSubject :many
-- 报考方向改了以后，重新对这门专业课上的官方题库（T51）。
SELECT bank_id FROM bank_subscriptions WHERE owner_user_id = ? AND subject_id = ?;

-- name: SetSubjectMajorsIfEmpty :exec
-- 1.1 选填的报考专业记为报考方向，已经有方向的不覆盖（T51）。
UPDATE subjects SET my_majors = ? WHERE id = ? AND owner_user_id = ? AND (my_majors IS NULL OR my_majors = '');

-- name: SetUserQuestionStatus :exec
UPDATE questions SET status = ? WHERE id = ? AND owner_user_id = ?;

-- name: UnlinkUserQuestion :exec
-- 移除官方题库时用户改过的题留下来：有年份的官方真题变成用户自己的真题（T57，仍在真题卷里），其他的变成习题。
UPDATE questions SET official_question_id = NULL, official_hash = NULL,
  source = IF(source = 'official', IF(exam_year IS NULL, 'exercise', 'exam'), source) WHERE id = ? AND owner_user_id = ?;

-- name: DeleteUserQuestion :exec
DELETE FROM questions WHERE id = ? AND owner_user_id = ?;

-- name: ListUserRubricForCopies :many
SELECT r.question_id, r.kp_id, r.seq, r.content FROM rubric_points r WHERE r.owner_user_id = sqlc.arg(owner)
  AND (r.question_id IN (SELECT q.id FROM questions q WHERE q.bank_id = sqlc.arg(bank_id) AND q.owner_user_id = sqlc.arg(owner) AND q.official_question_id IS NOT NULL)
    OR r.kp_id IN (SELECT k.id FROM knowledge_points k WHERE k.bank_id = sqlc.arg(bank_id) AND k.owner_user_id = sqlc.arg(owner) AND k.official_kp_id IS NOT NULL))
ORDER BY r.question_id, r.kp_id, r.seq;

-- name: DeleteUserQuestionRubric :exec
DELETE FROM rubric_points WHERE question_id = ? AND owner_user_id = ?;

-- name: DeleteUserKPRubric :exec
DELETE FROM rubric_points WHERE kp_id = ? AND owner_user_id = ?;

-- name: DeleteUserQuestionKPs :exec
DELETE FROM question_kps WHERE question_id = ? AND owner_user_id = ?;

-- name: ReciteReviewAgain :exec
-- 官方采分点有实质变化时，对应背诵重新进入复习（PRD 11.15）。
UPDATE kp_mastery SET recite_next_review_on = ?, recite_interval_step = 0 WHERE kp_id = ? AND owner_user_id = ? AND recite_next_review_on IS NOT NULL;

-- name: NotifyOfficialLaunch :execrows
-- 官方题库首次上线：自建了同一专业课（代码相同）、能看到官方题库开关的用户收到提醒（PRD 5.6）。
INSERT IGNORE INTO messages (owner_user_id, mtype, title, body, link, dedupe_key)
SELECT DISTINCT s.owner_user_id, 'official_bank', sqlc.arg(title), sqlc.arg(body), sqlc.arg(link), sqlc.arg(dedupe_key)
FROM subjects s JOIN users u ON u.id = s.owner_user_id
WHERE s.code = sqlc.arg(code) AND u.status = 'active'
  AND (EXISTS (SELECT 1 FROM feature_flags f WHERE f.flag_key = 'official_bank' AND f.enabled_for_all = 1)
    OR EXISTS (SELECT 1 FROM feature_flag_users fu WHERE fu.flag_key = 'official_bank' AND fu.user_id = s.owner_user_id));

-- ---------- 7.11 官方参考素材 ----------

-- name: ListOfficialReferences :many
SELECT * FROM official_references WHERE project_id = ? ORDER BY id;

-- name: CreateOfficialReference :execlastid
INSERT INTO official_references (project_id, title, kind, license_note, segments, chars, created_by) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: InsertOfficialReferenceSegment :exec
INSERT INTO official_reference_segments (reference_id, seq, heading, text) VALUES (?, ?, ?, ?);

-- name: DeleteOfficialReference :execrows
DELETE FROM official_references WHERE id = ? AND project_id = ?;

-- name: ListOfficialReferenceSegments :many
SELECT s.id, s.reference_id, s.seq, s.heading, s.text, r.title
FROM official_reference_segments s
JOIN official_references r ON r.id = s.reference_id
WHERE r.project_id = ?
ORDER BY s.reference_id, s.seq;
