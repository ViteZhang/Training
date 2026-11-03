-- T46 申请收录。

-- name: UpsertApplicationGroup :exec
INSERT INTO course_application_groups (group_key, school, major, subject_name, subject_code, discipline, status, pending_since, last_applied_at)
VALUES (?, ?, ?, ?, ?, ?, 'pending', sqlc.arg(pending_since), sqlc.arg(now))
ON DUPLICATE KEY UPDATE last_applied_at = VALUES(last_applied_at), major = IF(major = '', VALUES(major), major),
  subject_code = COALESCE(subject_code, VALUES(subject_code)), discipline = IF(discipline = '', VALUES(discipline), discipline);

-- name: LockApplicationGroupByKey :one
SELECT * FROM course_application_groups WHERE group_key = ? FOR UPDATE;

-- name: InsertCourseApplication :execrows
INSERT IGNORE INTO course_applications (owner_user_id, group_id, school, major, subject_name, subject_code, exam_year, entry, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: BumpApplicationGroup :exec
-- 新申请：人数加 1；暂不收录的课有人再申请时重新进入待处理。
UPDATE course_application_groups SET applicant_count = applicant_count + 1,
  status = IF(status = 'rejected', 'pending', status),
  pending_since = IF(status IN ('pending', 'suggested', 'rejected'), COALESCE(pending_since, sqlc.arg(now)), pending_since)
WHERE id = sqlc.arg(id);

-- name: SetApplicationGroupSuggested :execrows
UPDATE course_application_groups SET status = 'suggested' WHERE id = ? AND status = 'pending';

-- name: GetApplicationGroup :one
SELECT * FROM course_application_groups WHERE id = ?;

-- name: MyCourseApplications :many
SELECT a.id, a.school, a.major, a.subject_name, a.subject_code, a.exam_year, a.created_at, g.status, g.applicant_count
FROM course_applications a JOIN course_application_groups g ON g.id = a.group_id
WHERE a.owner_user_id = ? ORDER BY a.id DESC;

-- name: CatalogDiscipline :one
SELECT discipline FROM catalog_courses WHERE school = ? AND (subject_code = sqlc.arg(code) OR subject_name = sqlc.arg(name)) AND discipline <> '' LIMIT 1;

-- name: AdminListApplicationGroups :many
SELECT g.*,
  (SELECT COUNT(*) FROM course_applications a JOIN users u ON u.id = a.owner_user_id WHERE a.group_id = g.id AND u.is_seed = 1) AS seed_count,
  (SELECT COUNT(*) FROM course_applications a WHERE a.group_id = g.id AND EXISTS (
     SELECT 1 FROM memberships m WHERE m.owner_user_id = a.owner_user_id AND m.source IN ('order', 'redeem') AND m.revoked_at IS NULL)) AS paid_count
FROM course_application_groups g
WHERE (sqlc.arg(discipline) = '' OR g.discipline = sqlc.arg(discipline))
  AND (sqlc.arg(only_pending) = FALSE OR g.status IN ('pending', 'suggested'))
ORDER BY g.status IN ('pending', 'suggested') DESC, g.pending_since IS NULL, g.pending_since, g.applicant_count DESC, g.id DESC
LIMIT 500;

-- name: ApplicationStats :one
-- 付费用户：买过会员（订单或兑换码），赠送的不算。
SELECT
  CAST(COALESCE(SUM(status IN ('pending', 'suggested')), 0) AS SIGNED) AS pending,
  CAST(COALESCE(SUM(status = 'suggested'), 0) AS SIGNED) AS suggested,
  CAST(COALESCE(SUM(status IN ('pending', 'suggested') AND pending_since < sqlc.arg(overdue_before)), 0) AS SIGNED) AS overdue,
  (SELECT COUNT(*) FROM course_applications ca WHERE ca.created_at >= sqlc.arg(week_start)) AS week_new,
  COUNT(DISTINCT CONCAT(school, '|', major)) AS school_majors
FROM course_application_groups;

-- name: ListApplicants :many
-- 7.16 右侧详情：只有申请表单信息与标记，不含用户任何资料内容。
SELECT a.owner_user_id, u.nickname, u.is_seed, a.exam_year, a.entry, a.created_at,
  EXISTS (SELECT 1 FROM memberships m WHERE m.owner_user_id = a.owner_user_id AND m.source IN ('order', 'redeem') AND m.revoked_at IS NULL) AS paid
FROM course_applications a JOIN users u ON u.id = a.owner_user_id
WHERE a.group_id = ? ORDER BY a.created_at;

-- name: ApplicantIDs :many
SELECT owner_user_id FROM course_applications WHERE group_id = ?;

-- name: HandleApplicationGroup :exec
UPDATE course_application_groups SET status = ?, reason = ?, project_id = COALESCE(sqlc.narg(project_id), project_id), pending_since = NULL,
  handled_by = ?, handled_at = ? WHERE id = ?;

-- name: ApplicationGroupsOfProject :many
SELECT id FROM course_application_groups WHERE project_id = ? OR group_key = ?;

-- name: SetUserSeed :execrows
UPDATE users SET is_seed = ? WHERE id = ?;

-- name: GetUserSeed :one
SELECT is_seed FROM users WHERE id = ?;
