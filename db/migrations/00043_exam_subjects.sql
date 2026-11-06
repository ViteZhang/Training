-- +goose Up
-- T50 考试科目（PRD v3 增补「考试科目」）：一套卷子是一条记录，按「院校 + 学科 + 考试代码 + 科目名称」维护；
-- 专业不再参与去重，只作为选填的「适用专业」展示。命题方式分院校自命题和全国统考：统考科目不属于任何院校（school 为空串），
-- 全国只有一条、只立一个官方题库项目。表名沿用 catalog_courses，旧接口与旧 App 不受影响。
ALTER TABLE catalog_courses
  ADD COLUMN scope ENUM('school','national') NOT NULL DEFAULT 'school' COMMENT '命题方式：院校自命题 / 全国统考（统考的 school 为空串）' AFTER id,
  ADD COLUMN majors JSON NULL COMMENT '适用专业，字符串数组（选填，只用来展示和搜索）' AFTER major,
  ADD COLUMN course_key VARCHAR(200) NULL COMMENT '去重键：命题方式|院校|代码（没有代码时 n:名称）|年份（各年通用为 0）' AFTER exam_year;

UPDATE catalog_courses SET course_key = CONCAT(scope, '|', school, '|',
  IF(subject_code IS NULL OR subject_code = '', CONCAT('n:', subject_name), subject_code), '|', COALESCE(exam_year, 0));

-- 同一院校同一代码原来按专业录了多行：合并成一条（保留 id 最小的），专业收进 majors，引用改指向保留的那条。
UPDATE catalog_courses c
JOIN (SELECT k.keep_id, JSON_ARRAYAGG(d.major) AS ms
      FROM (SELECT course_key, MIN(id) AS keep_id FROM catalog_courses GROUP BY course_key) k
      JOIN (SELECT DISTINCT course_key, major FROM catalog_courses WHERE major <> '') d ON d.course_key = k.course_key
      GROUP BY k.keep_id) m ON m.keep_id = c.id
SET c.majors = m.ms;

UPDATE catalog_courses c
JOIN (SELECT course_key, MIN(id) AS keep_id, MAX(discipline) AS discipline, MIN(status) AS status FROM catalog_courses GROUP BY course_key) k ON k.keep_id = c.id
SET c.discipline = IF(c.discipline = '', k.discipline, c.discipline), c.status = k.status;

UPDATE subjects s
JOIN catalog_courses c ON c.id = s.catalog_course_id
JOIN (SELECT course_key, MIN(id) AS keep_id FROM catalog_courses GROUP BY course_key) k ON k.course_key = c.course_key
SET s.catalog_course_id = k.keep_id WHERE c.id <> k.keep_id;

UPDATE catalog_pending_courses p
JOIN catalog_courses c ON c.id = p.catalog_course_id
JOIN (SELECT course_key, MIN(id) AS keep_id FROM catalog_courses GROUP BY course_key) k ON k.course_key = c.course_key
SET p.catalog_course_id = k.keep_id WHERE c.id <> k.keep_id;

DELETE c FROM catalog_courses c
JOIN (SELECT course_key, MIN(id) AS keep_id FROM catalog_courses GROUP BY course_key) k ON k.course_key = c.course_key
WHERE c.id <> k.keep_id;

ALTER TABLE catalog_courses
  MODIFY COLUMN course_key VARCHAR(200) NOT NULL COMMENT '去重键：命题方式|院校|代码（没有代码时 n:名称）|年份（各年通用为 0）',
  DROP INDEX uk_catalog_courses,
  ADD UNIQUE KEY uk_catalog_courses_key (course_key),
  ADD KEY idx_catalog_courses_scope (scope, discipline);

CREATE TABLE catalog_school_exams (
  school     VARCHAR(64)     NOT NULL,
  course_id  BIGINT UNSIGNED NOT NULL COMMENT '全国统考科目',
  created_by BIGINT UNSIGNED NULL,
  created_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (school, course_id),
  KEY idx_catalog_school_exams_course (course_id),
  CONSTRAINT fk_catalog_school_exams_course FOREIGN KEY (course_id) REFERENCES catalog_courses (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='院校登记使用的全国统考科目（选填：没登记的院校在学科下也能看到统考科目）';

-- 官方题库立项直接挂在考试科目上，不再靠院校、代码字符串匹配。
ALTER TABLE official_projects
  ADD COLUMN catalog_course_id BIGINT UNSIGNED NULL COMMENT '考试科目；为空的是 T50 之前的旧立项，仍按院校 + 代码匹配' AFTER id,
  ADD KEY idx_official_projects_course (catalog_course_id);

UPDATE official_projects p
JOIN catalog_courses c ON c.scope = 'school' AND c.school = p.school
  AND (p.subject_code = c.subject_code OR (c.subject_code IS NULL AND p.subject_name = c.subject_name))
SET p.catalog_course_id = c.id;

-- 7.10 需求洞察改按「院校 + 代码」统计（统考按代码，院校为空串），「标为未规划」也一样。
ALTER TABLE official_demand_marks
  ADD COLUMN school VARCHAR(64) NOT NULL DEFAULT '' COMMENT '院校；全国统考或院校未知时为空串' FIRST,
  DROP PRIMARY KEY,
  ADD PRIMARY KEY (school, subject_code);

-- 第一批全国统考科目。
INSERT INTO catalog_courses (scope, school, major, discipline, subject_name, subject_code, course_key) VALUES
  ('national', '', '', '教育学', '教育学专业基础', '311', 'national||311|0'),
  ('national', '', '', '教育学', '心理学专业基础', '312', 'national||312|0'),
  ('national', '', '', '历史学', '历史学基础', '313', 'national||313|0'),
  ('national', '', '', '教育学', '教育综合', '333', 'national||333|0');

-- +goose Down
-- 合并掉的重复行不恢复。
DELETE FROM official_demand_marks WHERE school <> '';
ALTER TABLE official_demand_marks DROP PRIMARY KEY, ADD PRIMARY KEY (subject_code), DROP COLUMN school;
ALTER TABLE official_projects DROP INDEX idx_official_projects_course, DROP COLUMN catalog_course_id;
DROP TABLE catalog_school_exams;
DELETE FROM catalog_courses WHERE scope = 'national';
ALTER TABLE catalog_courses
  DROP INDEX idx_catalog_courses_scope,
  DROP INDEX uk_catalog_courses_key,
  ADD UNIQUE KEY uk_catalog_courses (school, major, subject_name),
  DROP COLUMN course_key,
  DROP COLUMN majors,
  DROP COLUMN scope;
