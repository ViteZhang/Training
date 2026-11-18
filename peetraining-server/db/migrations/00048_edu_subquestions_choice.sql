-- 教育学 311/333：子问、分问采分点、选答组/计分槽与考试体系。
-- 只新增表/字段，不改旧数据；子问分值允许 NULL，表示原卷未提供。

ALTER TABLE official_projects
  ADD COLUMN exam_system VARCHAR(32) NOT NULL DEFAULT 'school' COMMENT 'school 院校自命题 / national 全国统考' AFTER major;

ALTER TABLE catalog_courses
  ADD COLUMN exam_system VARCHAR(32) NOT NULL DEFAULT 'school' COMMENT 'school 院校自命题 / national 全国统考' AFTER scope;

CREATE TABLE question_subquestions (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  question_id   BIGINT UNSIGNED NOT NULL,
  ordinal       SMALLINT UNSIGNED NOT NULL COMMENT '大题内顺序，从 1 开始',
  label         VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '展示序号，如 (1)',
  stem          TEXT         NOT NULL COMMENT '设问原文',
  answer        MEDIUMTEXT   NULL COMMENT '分问参考答案',
  full_score    DECIMAL(6,2) NULL COMMENT 'NULL 表示原卷未给分问分值',
  score_source  ENUM('original','team') NULL COMMENT 'original 原题给定 / team 团队制定（须审核）',
  needs_review  TINYINT(1)   NOT NULL DEFAULT 1,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_question_subquestions (question_id, ordinal),
  CONSTRAINT fk_question_subquestions_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='大题子问（教育学多子问）';

ALTER TABLE rubric_points
  ADD COLUMN subquestion_id BIGINT UNSIGNED NULL COMMENT '属于子问的采分点；NULL 表示整题采分点' AFTER question_id,
  ADD KEY idx_rubric_points_subquestion (subquestion_id),
  ADD CONSTRAINT fk_rubric_points_subquestion FOREIGN KEY (subquestion_id) REFERENCES question_subquestions (id) ON DELETE CASCADE;

CREATE TABLE paper_choice_groups (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  paper_id     BIGINT UNSIGNED NOT NULL,
  slot_no      SMALLINT UNSIGNED NOT NULL COMMENT '卷面题号，如 56',
  slot_score   DECIMAL(6,2) NOT NULL COMMENT '该槽计分分值，如 30',
  choose_count SMALLINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '计入卷面的候选数',
  rule         ENUM('choose_one','both_count_first') NOT NULL DEFAULT 'choose_one' COMMENT '2026 311 第56题：双答计Ⅰ',
  description  TEXT NULL COMMENT '原卷选答说明原文',
  created_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at   DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_paper_choice_groups (paper_id, slot_no),
  CONSTRAINT fk_paper_choice_groups_paper FOREIGN KEY (paper_id) REFERENCES papers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='卷面选答组（与专业限选无关）';

CREATE TABLE paper_choice_group_items (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  group_id    BIGINT UNSIGNED NOT NULL,
  question_id BIGINT UNSIGNED NOT NULL,
  seq         SMALLINT UNSIGNED NOT NULL DEFAULT 1,
  PRIMARY KEY (id),
  UNIQUE KEY uk_paper_choice_group_items (group_id, question_id),
  KEY idx_paper_choice_group_items_question (question_id),
  CONSTRAINT fk_paper_choice_group_items_group FOREIGN KEY (group_id) REFERENCES paper_choice_groups (id) ON DELETE CASCADE,
  CONSTRAINT fk_paper_choice_group_items_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='选答组候选大题';

CREATE TABLE paper_slots (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  paper_id         BIGINT UNSIGNED NOT NULL,
  slot_no          SMALLINT UNSIGNED NOT NULL,
  question_id      BIGINT UNSIGNED NULL COMMENT '普通题槽位',
  choice_group_id  BIGINT UNSIGNED NULL COMMENT '选答槽位',
  score            DECIMAL(6,2) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_paper_slots (paper_id, slot_no),
  KEY idx_paper_slots_group (choice_group_id),
  CONSTRAINT fk_paper_slots_paper FOREIGN KEY (paper_id) REFERENCES papers (id) ON DELETE CASCADE,
  CONSTRAINT fk_paper_slots_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE SET NULL,
  CONSTRAINT fk_paper_slots_group FOREIGN KEY (choice_group_id) REFERENCES paper_choice_groups (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='卷面计分槽；311 第56题两个候选共享一个槽';
