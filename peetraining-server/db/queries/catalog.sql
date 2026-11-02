-- T44 院校专业课库与引导分流。

-- name: SearchCatalogSchools :many
-- 1.1 搜索院校：按名称包含匹配，课多的排前面。
SELECT school, COUNT(*) AS course_count FROM catalog_courses
WHERE status = 'active' AND school LIKE CONCAT('%', sqlc.arg(q), '%')
GROUP BY school ORDER BY course_count DESC, school LIMIT 20;

-- name: ListCatalogCoursesBySchool :many
-- 1.1 按院校列出专业与专业课；带出官方题库立项（院校相同，代码相同，没有代码时名称相同）。
SELECT c.id, c.school, c.major, c.discipline, c.subject_name, c.subject_code, c.textbooks, c.exam_year,
  CAST(COALESCE((SELECT p.id FROM official_projects p WHERE p.school = c.school
    AND (p.subject_code = c.subject_code OR (c.subject_code IS NULL AND p.subject_name = c.subject_name))
    ORDER BY p.stage = 'published' DESC, p.id DESC LIMIT 1), 0) AS UNSIGNED) AS project_id
FROM catalog_courses c
WHERE c.status = 'active' AND c.school = sqlc.arg(school) AND (c.exam_year IS NULL OR sqlc.arg(exam_year) = 0 OR c.exam_year = sqlc.arg(exam_year))
ORDER BY c.major, c.subject_code IS NULL, c.subject_code, c.subject_name;

-- name: GetCatalogCourse :one
SELECT * FROM catalog_courses WHERE id = ? AND status = 'active';

-- name: GetOfficialProjectStage :one
SELECT id, stage, bank_id FROM official_projects WHERE id = ?;

-- name: OfficialBankSummary :one
-- 1.1a 官方题库摘要：版本、真题年份数、知识点数、模拟卷套数、题数。
SELECT
  CAST(COALESCE((SELECT v.version FROM official_versions v WHERE v.project_id = p.id AND v.rolled_back_at IS NULL ORDER BY v.id DESC LIMIT 1), '') AS CHAR) AS version,
  (SELECT COUNT(DISTINCT q.exam_year) FROM questions q WHERE q.bank_id = p.bank_id AND q.owner_user_id IS NULL AND q.status = 'active' AND q.exam_year IS NOT NULL) AS exam_year_count,
  (SELECT COUNT(*) FROM knowledge_points k WHERE k.bank_id = p.bank_id AND k.owner_user_id IS NULL AND k.level = 'point') AS kp_count,
  (SELECT COUNT(*) FROM papers pp WHERE pp.bank_id = p.bank_id AND pp.owner_user_id IS NULL) AS paper_count,
  (SELECT COUNT(*) FROM questions q WHERE q.bank_id = p.bank_id AND q.owner_user_id IS NULL AND q.status = 'active') AS question_count
FROM official_projects p WHERE p.id = ?;

-- name: SetSubjectCatalog :exec
UPDATE subjects SET catalog_course_id = ?, school = ?, major = ? WHERE id = ? AND owner_user_id = ?;

-- name: UpsertCatalogPending :exec
INSERT INTO catalog_pending_courses (group_key, school, major, subject_name, subject_code, request_count, first_at, last_at)
VALUES (?, ?, ?, ?, ?, 0, sqlc.arg(now), sqlc.arg(now))
ON DUPLICATE KEY UPDATE last_at = VALUES(last_at), major = IF(major = '', VALUES(major), major);

-- name: GetCatalogPendingByKey :one
SELECT * FROM catalog_pending_courses WHERE group_key = ?;

-- name: AddCatalogPendingUser :execrows
INSERT IGNORE INTO catalog_pending_users (pending_id, user_id) VALUES (?, ?);

-- name: IncCatalogPendingCount :exec
UPDATE catalog_pending_courses SET request_count = request_count + 1 WHERE id = ?;

-- name: UsersOfCatalogCourse :many
-- 官方题库上线时通知在 1.1 选了该课的用户。
SELECT DISTINCT owner_user_id FROM subjects WHERE catalog_course_id = ?;

-- name: CatalogCoursesOfProject :many
SELECT c.id FROM catalog_courses c JOIN official_projects p ON p.school = c.school
  AND (p.subject_code = c.subject_code OR (c.subject_code IS NULL AND p.subject_name = c.subject_name))
WHERE p.id = ?;

-- name: AdminListCatalogCourses :many
SELECT c.*,
  CAST(COALESCE((SELECT p.stage FROM official_projects p WHERE p.school = c.school
    AND (p.subject_code = c.subject_code OR (c.subject_code IS NULL AND p.subject_name = c.subject_name))
    ORDER BY p.stage = 'published' DESC, p.id DESC LIMIT 1), '') AS CHAR) AS project_stage,
  (SELECT COUNT(*) FROM subjects s WHERE s.catalog_course_id = c.id) AS chosen_count
FROM catalog_courses c
WHERE (sqlc.arg(q) = '' OR c.school LIKE CONCAT('%', sqlc.arg(q), '%') OR c.subject_name LIKE CONCAT('%', sqlc.arg(q), '%') OR c.subject_code = sqlc.arg(q))
ORDER BY c.status, c.school, c.major, c.subject_code, c.subject_name
LIMIT 500;

-- name: AdminGetCatalogCourse :one
SELECT * FROM catalog_courses WHERE id = ?;

-- name: InsertCatalogCourse :execlastid
INSERT INTO catalog_courses (school, major, discipline, subject_name, subject_code, textbooks, exam_year, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateCatalogCourse :exec
UPDATE catalog_courses SET school = ?, major = ?, discipline = ?, subject_name = ?, subject_code = ?, textbooks = ?, exam_year = ?, status = ? WHERE id = ?;

-- name: ListCatalogPending :many
SELECT * FROM catalog_pending_courses WHERE status = 'pending' ORDER BY request_count DESC, last_at DESC LIMIT 200;

-- name: GetCatalogPending :one
SELECT * FROM catalog_pending_courses WHERE id = ?;

-- name: ResolveCatalogPending :exec
UPDATE catalog_pending_courses SET status = ?, catalog_course_id = ?, handled_by = ?, handled_at = ? WHERE id = ? AND status = 'pending';

-- name: LinkPendingSubjects :exec
-- 待入库的课入库或合并后，把当初手动添加这门课的用户的专业课关联上，官方题库上线时能通知到。
UPDATE subjects s JOIN catalog_pending_users u ON u.user_id = s.owner_user_id
SET s.catalog_course_id = sqlc.arg(course_id)
WHERE u.pending_id = sqlc.arg(pending_id) AND s.catalog_course_id IS NULL AND s.school = sqlc.arg(school)
  AND (s.code = sqlc.arg(code) OR s.name = sqlc.arg(name));

-- name: CountOfficialFirstSessions :one
SELECT COUNT(*) FROM practice_sessions WHERE owner_user_id = ? AND kind = 'official_first';

-- name: FindOfficialFirstSession :one
SELECT id FROM practice_sessions WHERE owner_user_id = ? AND kind = 'official_first' ORDER BY id LIMIT 1;
