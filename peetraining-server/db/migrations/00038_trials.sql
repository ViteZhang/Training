-- T45 体验一道题、学科通用体验题与 AI 现场出题（PRD v3 增补「体验一道题」「学科通用体验题」「体验与额度」「AI 出题」）。
-- 体验的题目和作答属于用户内容，带 owner_user_id；用户选择不保存时整行删除，体验次数另记在 trial_usage，删了也不回退。

-- +goose Up
CREATE TABLE sample_directions (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  discipline  VARCHAR(16)  NOT NULL COMMENT '学科，如 哲学、文学',
  name        VARCHAR(32)  NOT NULL COMMENT '方向，如 西方哲学、古代文学',
  sort_order  SMALLINT UNSIGNED NOT NULL DEFAULT 0,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_sample_directions (discipline, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='通用体验题的学科与方向（7.18）';

CREATE TABLE sample_questions (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  direction_id  BIGINT UNSIGNED NULL COMMENT '已覆盖学科的方向；AI 现场出题为空',
  origin        ENUM('manual','ai_batch','ai_live') NOT NULL COMMENT '手工录入 / AI 批量初稿 / 未覆盖学科的 AI 现场出题',
  cache_key     VARCHAR(200) NULL COMMENT 'AI 现场出题按专业课缓存复用的键',
  subject_label VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'AI 现场出题对应的专业课名',
  textbook      VARCHAR(128) NOT NULL DEFAULT '' COMMENT '参考教材',
  qtype         ENUM('term','short_answer','discussion') NOT NULL,
  score         DECIMAL(6,2) NOT NULL,
  stem          TEXT         NOT NULL,
  answer        TEXT         NOT NULL,
  rubric        JSON         NOT NULL COMMENT '采分点 [{content, score, keywords}]，合计等于分值',
  status        ENUM('pending','online','offline') NOT NULL DEFAULT 'pending' COMMENT '审核通过后才在 1.10 显示；AI 现场出题直接可用，可抽检下线',
  try_count     INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '被体验次数（提交批改成功才计）',
  created_by    BIGINT UNSIGNED NULL,
  reviewed_by   BIGINT UNSIGNED NULL,
  reviewed_at   DATETIME(3)  NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_sample_questions_direction (direction_id, status),
  KEY idx_sample_questions_cache (cache_key, status),
  CONSTRAINT fk_sample_questions_direction FOREIGN KEY (direction_id) REFERENCES sample_directions (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='通用体验题与 AI 出题缓存（团队内容，不含用户资料）';

CREATE TABLE trials (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id      BIGINT UNSIGNED NOT NULL,
  kind               ENUM('own','sample','ai_live') NOT NULL COMMENT '自己的题 / 通用体验题 / AI 现场出题',
  sample_question_id BIGINT UNSIGNED NULL,
  input_mode         ENUM('paste','photo','sample') NOT NULL,
  qtype              ENUM('term','short_answer','discussion') NOT NULL,
  score              DECIMAL(6,2) NOT NULL,
  stem               TEXT         NOT NULL,
  reference_answer   TEXT         NOT NULL,
  answer_source      ENUM('user','ai') NOT NULL COMMENT 'user 用户填了参考答案；ai AI 参考答案（标「仅供参考」）',
  rubric             JSON         NOT NULL,
  answer_mode        ENUM('typed','photo') NULL,
  answer_text        MEDIUMTEXT   NULL COMMENT '作答原文；不得写入日志',
  photo_keys         JSON         NULL,
  result             JSON         NULL COMMENT '逐采分点结果、建议',
  total              DECIMAL(6,2) NULL,
  status             ENUM('ready','graded','saved') NOT NULL DEFAULT 'ready',
  saved_question_id  BIGINT UNSIGNED NULL,
  idempotency_key    VARCHAR(64)  NULL,
  graded_at          DATETIME(3)  NULL,
  created_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_trials_owner (owner_user_id, created_at),
  UNIQUE KEY uk_trials_key (owner_user_id, idempotency_key),
  CONSTRAINT fk_trials_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='体验一道题（1.9–1.10），不消耗任何额度';

CREATE TABLE trial_usage (
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  used            INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '批改成功的体验次数',
  first_graded_at DATETIME(3)  NULL,
  PRIMARY KEY (owner_user_id),
  CONSTRAINT fk_trial_usage_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='体验次数（体验一道题与通用体验题共用）';

INSERT INTO feature_flags (flag_key, enabled_for_all, description, updated_at) VALUES
  ('trial', FALSE, '体验一道题（1.9）', UTC_TIMESTAMP(3)),
  ('sample_trial', FALSE, '学科通用体验题（1.10）', UTC_TIMESTAMP(3)),
  ('ai_live_question', FALSE, '未覆盖学科的 AI 现场出题（1.10）', UTC_TIMESTAMP(3));

INSERT INTO rule_params (param_key, value, description) VALUES
  ('official_first', JSON_OBJECT('trial_limit', 1, 'apply_suggest_threshold', 3, 'apply_overdue_hours', 24,
    'supplement', JSON_OBJECT('window_days', 14, 'min_attempts', 5, 'max_score_rate', 0.6, 'max_official', 5, 'wrong_book_count', 30, 'snooze_days', 7)),
   'PRD v3 增补：体验次数、申请收录阈值、补充资料提示');

-- 第一批覆盖学科：哲学、文学（增补「AI 出题」）。
INSERT INTO sample_directions (discipline, name, sort_order) VALUES
  ('哲学', '马克思主义哲学', 1), ('哲学', '中国哲学', 2), ('哲学', '西方哲学', 3), ('哲学', '伦理学', 4),
  ('文学', '古代文学', 1), ('文学', '现当代文学', 2), ('文学', '外国文学', 3), ('文学', '文学理论', 4);

-- +goose Down
DELETE FROM rule_params WHERE param_key = 'official_first';
DELETE FROM feature_flags WHERE flag_key IN ('trial', 'sample_trial', 'ai_live_question');
DROP TABLE trial_usage;
DROP TABLE trials;
DROP TABLE sample_questions;
DROP TABLE sample_directions;
