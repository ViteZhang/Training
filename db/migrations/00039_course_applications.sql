-- T46 申请收录（PRD v3 增补「申请收录」「7.16 申请收录」）。
-- 按「院校 + 专业课名称」归并，有代码时优先按代码（rules.CourseKey）。后台只看申请表单信息，不看用户任何资料内容。

-- +goose Up
ALTER TABLE users ADD COLUMN is_seed TINYINT(1) NOT NULL DEFAULT 0 COMMENT '种子用户，运营在 7.2 标记' AFTER nickname;

CREATE TABLE course_application_groups (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  group_key        VARCHAR(200) NOT NULL,
  school           VARCHAR(64)  NOT NULL,
  major            VARCHAR(64)  NOT NULL DEFAULT '',
  subject_name     VARCHAR(64)  NOT NULL,
  subject_code     VARCHAR(16)  NULL,
  discipline       VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '从院校专业课库带出，7.16 按学科筛选',
  status           ENUM('pending','suggested','contacted','initiated','rejected') NOT NULL DEFAULT 'pending',
  reason           VARCHAR(255) NULL COMMENT '暂不收录的原因（必填）',
  applicant_count  INT UNSIGNED NOT NULL DEFAULT 0,
  pending_since    DATETIME(3)  NULL COMMENT '最早待处理时间；处理后清空，超过 24 小时标红',
  last_applied_at  DATETIME(3)  NOT NULL,
  project_id       BIGINT UNSIGNED NULL COMMENT '立项生成的 7.11 官方题库项目',
  handled_by       BIGINT UNSIGNED NULL,
  handled_at       DATETIME(3)  NULL,
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_course_application_groups (group_key),
  KEY idx_course_application_groups_status (status, pending_since),
  KEY idx_course_application_groups_project (project_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='申请收录按课归并（7.16）';

CREATE TABLE course_applications (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  group_id      BIGINT UNSIGNED NOT NULL,
  school        VARCHAR(64)  NOT NULL,
  major         VARCHAR(64)  NOT NULL,
  subject_name  VARCHAR(64)  NOT NULL,
  subject_code  VARCHAR(16)  NULL,
  exam_year     SMALLINT UNSIGNED NOT NULL,
  entry         ENUM('onboarding','bank','settings') NOT NULL COMMENT '入口：1.1c / 题库页 / 我的 → 备考设置',
  created_at    DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_course_applications (owner_user_id, group_id),
  KEY idx_course_applications_group (group_id, created_at),
  CONSTRAINT fk_course_applications_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_course_applications_group FOREIGN KEY (group_id) REFERENCES course_application_groups (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='申请收录（1.11），同一用户同一门课只能申请一次';

INSERT INTO feature_flags (flag_key, enabled_for_all, description, updated_at) VALUES
  ('course_apply', FALSE, '申请收录（1.11）', UTC_TIMESTAMP(3));

-- +goose Down
DELETE FROM feature_flags WHERE flag_key = 'course_apply';
DROP TABLE course_applications;
DROP TABLE course_application_groups;
ALTER TABLE users DROP COLUMN is_seed;
