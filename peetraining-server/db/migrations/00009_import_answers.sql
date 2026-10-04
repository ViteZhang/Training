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

-- 每个文件的解析额度预占：结算或退回时要用预占时的周期与页数（quota.Ticket）。
ALTER TABLE import_job_materials
  ADD COLUMN reserved_pages INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '本文件预占的解析页数' AFTER failed_pages,
  ADD COLUMN quota_period   VARCHAR(16)  NULL COMMENT '预占时的额度周期键' AFTER reserved_pages,
  ADD COLUMN settled        TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '额度已结算（成功按计费页数扣，失败全部退回）' AFTER quota_period;

-- +goose Down
ALTER TABLE import_job_materials DROP COLUMN settled, DROP COLUMN quota_period, DROP COLUMN reserved_pages;
DROP TABLE import_answers;
