-- 作文（dev-spec 第五节「作文」，PRD 第 9 节模块 5、11.13）。

-- +goose Up
CREATE TABLE essay_rubrics (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NULL COMMENT '通用五维度标准为空',
  subject_id    BIGINT UNSIGNED NULL,
  source        ENUM('user_material','generic') NOT NULL,
  name          VARCHAR(64)  NOT NULL,
  full_score    DECIMAL(6,2) NOT NULL,
  dimensions    JSON         NOT NULL COMMENT '[{name,description,score,bands:[{range,description}]}]',
  source_material_id BIGINT UNSIGNED NULL,
  source_page   INT UNSIGNED NULL,
  is_active     TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '当前使用的标准；改了标准后新写的作文按新标准批改',
  origin        ENUM('user_confirmed','ai_extracted','official') NOT NULL DEFAULT 'ai_extracted',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_essay_rubrics_subject (owner_user_id, subject_id, is_active),
  CONSTRAINT fk_essay_rubrics_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_essay_rubrics_subject FOREIGN KEY (subject_id) REFERENCES subjects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='作文评分标准（5.9）';

CREATE TABLE essays (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NOT NULL,
  subject_id       BIGINT UNSIGNED NOT NULL,
  topic_source     ENUM('exam','ai','custom') NOT NULL COMMENT '真题 / AI 命题 / 自拟',
  topic_question_id BIGINT UNSIGNED NULL,
  topic_text       TEXT         NOT NULL,
  required_words   SMALLINT UNSIGNED NULL,
  draft_no         TINYINT UNSIGNED NOT NULL DEFAULT 1 COMMENT '同题第几稿',
  parent_essay_id  BIGINT UNSIGNED NULL,
  content          MEDIUMTEXT   NULL,
  photo_keys       JSON         NULL,
  word_count       INT UNSIGNED NOT NULL DEFAULT 0,
  timed            TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '按考试时长限时完成',
  duration_seconds INT UNSIGNED NOT NULL DEFAULT 0,
  rubric_id        BIGINT UNSIGNED NULL,
  rubric_snapshot  JSON         NULL,
  status           ENUM('draft','queued_quota','grading','graded','failed') NOT NULL DEFAULT 'draft',
  score            DECIMAL(6,2) NULL,
  full_score       DECIMAL(6,2) NULL,
  dimension_scores JSON         NULL,
  review           JSON         NULL COMMENT '总评、逐段批注、范文对比',
  counts_for_estimate TINYINT(1) NOT NULL DEFAULT 0 COMMENT '按用户评分细则批改且真题限时完成（PRD 11.13）',
  model            VARCHAR(64)  NULL,
  prompt_version   VARCHAR(32)  NULL,
  idempotency_key  VARCHAR(64)  NULL,
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  graded_at        DATETIME(3)  NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_essays_idem (owner_user_id, idempotency_key),
  KEY idx_essays_owner (owner_user_id, subject_id, created_at),
  CONSTRAINT fk_essays_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_essays_subject FOREIGN KEY (subject_id) REFERENCES subjects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='作文本（5.8）';

CREATE TABLE writing_methods (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NULL,
  bank_id       BIGINT UNSIGNED NOT NULL,
  title         VARCHAR(128) NOT NULL,
  content       TEXT         NOT NULL,
  dimension     VARCHAR(32)  NULL COMMENT '对应的评分维度，最弱维度「去学」用',
  source_material_id BIGINT UNSIGNED NULL,
  source_page   INT UNSIGNED NULL,
  origin        ENUM('user_confirmed','ai_extracted','ai_generated','official') NOT NULL,
  mastery_state ENUM('unlearned','learning','consolidating','mastered') NOT NULL DEFAULT 'unlearned',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_writing_methods_bank (bank_id),
  CONSTRAINT fk_writing_methods_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_writing_methods_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='写作方法（3.10）';

CREATE TABLE essay_materials (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NULL,
  bank_id       BIGINT UNSIGNED NOT NULL,
  theme         VARCHAR(64)  NOT NULL,
  content       TEXT         NOT NULL,
  source_material_id BIGINT UNSIGNED NULL,
  source_page   INT UNSIGNED NULL,
  origin        ENUM('user_confirmed','ai_extracted','ai_generated','official') NOT NULL COMMENT 'ai_generated 显示为「AI 补充」',
  favorite      TINYINT(1)   NOT NULL DEFAULT 0,
  exam_count    INT UNSIGNED NOT NULL DEFAULT 0,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_essay_materials_bank (bank_id, theme),
  CONSTRAINT fk_essay_materials_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_essay_materials_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='作文素材（3.10、5.3）';

CREATE TABLE model_essays (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NULL,
  bank_id       BIGINT UNSIGNED NOT NULL,
  topic_question_id BIGINT UNSIGNED NULL COMMENT '按真题题目归类',
  title         VARCHAR(128) NOT NULL,
  content       MEDIUMTEXT   NOT NULL,
  structure     JSON         NULL COMMENT 'AI 结构拆解：开头立意、分论点、升华、结尾',
  source_material_id BIGINT UNSIGNED NULL,
  source_page   INT UNSIGNED NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_model_essays_bank (bank_id, topic_question_id),
  CONSTRAINT fk_model_essays_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_model_essays_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户导入的范文（5.7）';

-- +goose Down
DROP TABLE model_essays;
DROP TABLE essay_materials;
DROP TABLE writing_methods;
DROP TABLE essays;
DROP TABLE essay_rubrics;
