-- T44 院校专业课库与引导分流（PRD v3 增补「院校专业课库」「引导分流」）。
-- 1.1 的院校搜索与专业课列表都来自运营手工维护的院校专业课库；官方题库状态不存，按 official_projects 实时算。
-- 用户手动添加、库里没有的课汇总成「待入库」，只记归并后的课程与人数，不记用户内容。

-- +goose Up
CREATE TABLE catalog_courses (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  school        VARCHAR(64)  NOT NULL COMMENT '院校全称',
  major         VARCHAR(64)  NOT NULL,
  discipline    VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '学科，如 哲学、文学；1.10、7.16 按它筛选',
  subject_name  VARCHAR(64)  NOT NULL,
  subject_code  VARCHAR(16)  NULL COMMENT '专业课代码，选填',
  textbooks     JSON         NULL COMMENT '参考书目，字符串数组',
  exam_year     SMALLINT UNSIGNED NULL COMMENT '适用考试年份；为空表示各年通用',
  status        ENUM('active','hidden') NOT NULL DEFAULT 'active',
  created_by    BIGINT UNSIGNED NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_catalog_courses (school, major, subject_name),
  KEY idx_catalog_courses_code (subject_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='院校专业课库（后台配置组维护）';

CREATE TABLE catalog_pending_courses (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  group_key      VARCHAR(200) NOT NULL COMMENT '院校 + 代码（有代码时）或院校 + 课程名',
  school         VARCHAR(64)  NOT NULL,
  major          VARCHAR(64)  NOT NULL DEFAULT '',
  subject_name   VARCHAR(64)  NOT NULL,
  subject_code   VARCHAR(16)  NULL,
  request_count  INT UNSIGNED NOT NULL DEFAULT 0,
  status         ENUM('pending','added','merged','ignored') NOT NULL DEFAULT 'pending',
  catalog_course_id BIGINT UNSIGNED NULL COMMENT '入库或合并到的课程',
  handled_by     BIGINT UNSIGNED NULL,
  handled_at     DATETIME(3)  NULL,
  first_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  last_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_catalog_pending (group_key),
  KEY idx_catalog_pending_status (status, last_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户在 1.1 手动添加、待运营入库的课程';

CREATE TABLE catalog_pending_users (
  pending_id BIGINT UNSIGNED NOT NULL,
  user_id    BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (pending_id, user_id),
  CONSTRAINT fk_catalog_pending_users_pending FOREIGN KEY (pending_id) REFERENCES catalog_pending_courses (id) ON DELETE CASCADE,
  CONSTRAINT fk_catalog_pending_users_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='待入库课程的去重计数（同一用户只算一次）';

ALTER TABLE subjects
  ADD COLUMN catalog_course_id BIGINT UNSIGNED NULL COMMENT '1.1 从院校专业课库选的课；官方题库上线时通知选了该课的用户' AFTER code,
  ADD COLUMN school VARCHAR(64) NULL COMMENT '1.1 选的院校' AFTER catalog_course_id,
  ADD COLUMN major VARCHAR(64) NULL AFTER school,
  ADD KEY idx_subjects_catalog (catalog_course_id);

-- 1.1b 官方题库首题：这一组的批改不扣额度、不占体验次数（增补「体验与额度」）。
ALTER TABLE practice_sessions
  MODIFY COLUMN kind ENUM('daily','type_drill','custom','wrong_redo','placement','high_freq','recite','single','official_first') NOT NULL;

INSERT INTO feature_flags (flag_key, enabled_for_all, description, updated_at) VALUES
  ('onboarding_route', FALSE, '引导分流（新 1.1：按院校专业课库匹配官方题库）', UTC_TIMESTAMP(3));

-- 第一批：四川大学哲学（增补「待确认事项」已提供的两门课）。
INSERT INTO catalog_courses (school, major, discipline, subject_name, subject_code, textbooks) VALUES
  ('四川大学', '哲学', '哲学', '中国哲学史', '662', JSON_ARRAY('陈荣捷《中国哲学文献选编》', '冯友兰《中国哲学史》')),
  ('四川大学', '哲学', '哲学', '西方哲学史', '812', JSON_ARRAY('张志伟《西方哲学史》', '北京大学哲学系外国哲学史教研室《西方哲学原著选读》'));

-- +goose Down
DELETE FROM catalog_courses WHERE school = '四川大学' AND subject_code IN ('662', '812');
DELETE FROM feature_flags WHERE flag_key = 'onboarding_route';
ALTER TABLE practice_sessions
  MODIFY COLUMN kind ENUM('daily','type_drill','custom','wrong_redo','placement','high_freq','recite','single') NOT NULL;
ALTER TABLE subjects DROP INDEX idx_subjects_catalog, DROP COLUMN major, DROP COLUMN school, DROP COLUMN catalog_course_id;
DROP TABLE catalog_pending_users;
DROP TABLE catalog_pending_courses;
DROP TABLE catalog_courses;
