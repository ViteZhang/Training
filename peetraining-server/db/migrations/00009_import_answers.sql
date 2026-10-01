-- +goose Up
-- 答案在单独文件或书后时，结构化阶段识别出的「只有答案」的条目先存在这里，
-- 等同一任务的全部文件结构化完后按「年份 + 题号」配对（dev-spec 第六节第 7 步）。
CREATE TABLE import_answers (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  job_id        BIGINT UNSIGNED NOT NULL,
  material_id   BIGINT UNSIGNED NOT NULL,
  exam_year     SMALLINT UNSIGNED NULL,
  question_no   VARCHAR(16)  NOT NULL,
  answer        MEDIUMTEXT   NOT NULL,
  page          INT UNSIGNED NOT NULL DEFAULT 0,
  dedupe_key    CHAR(64)     NOT NULL COMMENT '同一任务重跑时防止重复写入',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_import_answers_dedupe (job_id, dedupe_key),
  KEY idx_import_answers_owner (owner_user_id),
  CONSTRAINT fk_import_answers_job FOREIGN KEY (job_id) REFERENCES import_jobs (id) ON DELETE CASCADE,
  CONSTRAINT fk_import_answers_material FOREIGN KEY (material_id) REFERENCES materials (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='导入中只有答案的条目，待配对';

-- +goose Down
DROP TABLE import_answers;
