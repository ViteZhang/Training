-- +goose Up
-- T23 作文：批改额度预占、复核、AI 命题记录（PRD 模块 5、11.13、13.1）。
ALTER TABLE essays
  ADD COLUMN ai_topic_id    BIGINT UNSIGNED NULL COMMENT 'AI 命题（essay_ai_topics）' AFTER topic_question_id,
  ADD COLUMN quota_period   VARCHAR(16)  NULL COMMENT '提交时预占作文批改次数的周期，批改完成结算、失败退回' AFTER idempotency_key,
  ADD COLUMN grading_round  TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '第几次批改（复核重批加 1）' AFTER quota_period,
  ADD COLUMN submitted_at   DATETIME(3)  NULL AFTER graded_at,
  ADD COLUMN fail_reason    VARCHAR(255) NULL AFTER submitted_at,
  ADD COLUMN disputed       TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '每篇只能复核一次（与主观题相同）' AFTER fail_reason,
  ADD COLUMN dispute_reason VARCHAR(16)  NULL AFTER disputed,
  ADD COLUMN dispute_note   TEXT         NULL AFTER dispute_reason,
  ADD COLUMN score_before   DECIMAL(6,2) NULL COMMENT '复核前的总分' AFTER dispute_note;

CREATE TABLE essay_ai_topics (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id  BIGINT UNSIGNED NOT NULL,
  subject_id     BIGINT UNSIGNED NOT NULL,
  topic          TEXT         NOT NULL,
  required_words SMALLINT UNSIGNED NULL,
  note           VARCHAR(255) NULL,
  model          VARCHAR(64)  NULL,
  prompt_version VARCHAR(32)  NULL,
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_essay_ai_topics_owner (owner_user_id, subject_id, created_at),
  CONSTRAINT fk_essay_ai_topics_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_essay_ai_topics_subject FOREIGN KEY (subject_id) REFERENCES subjects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='作文 AI 命题（5.1，标「AI 出题」）';

-- 作文课预估分取最近 3 篇（PRD 11.13）。
UPDATE rule_params SET value = JSON_SET(value, '$.essay_recent', 3) WHERE param_key = 'score_estimate';

-- +goose Down
UPDATE rule_params SET value = JSON_REMOVE(value, '$.essay_recent') WHERE param_key = 'score_estimate';
DROP TABLE essay_ai_topics;
ALTER TABLE essays DROP COLUMN score_before, DROP COLUMN dispute_note, DROP COLUMN dispute_reason, DROP COLUMN disputed,
  DROP COLUMN fail_reason, DROP COLUMN submitted_at, DROP COLUMN grading_round, DROP COLUMN quota_period, DROP COLUMN ai_topic_id;
