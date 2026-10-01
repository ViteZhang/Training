-- 作答、批改、学习状态、整卷与估分（dev-spec 第五节）。
-- 学习数据只按「用户 + 知识点 / 题目」记，不关心题库来源。
-- 删除资料时，从它识别出的题目连同作答记录与错题一起删除（外键级联）；整卷成绩保存在 paper_sessions，不随之删除。

-- +goose Up
CREATE TABLE practice_sessions (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  subject_id    BIGINT UNSIGNED NULL,
  kind          ENUM('daily','type_drill','custom','wrong_redo','placement','high_freq','recite','single') NOT NULL,
  title         VARCHAR(64)  NOT NULL DEFAULT '',
  config        JSON         NULL COMMENT '自定义练习条件等',
  question_ids  JSON         NOT NULL COMMENT '本组题目顺序',
  cursor_index  INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '断点，下次从这里继续（4.10）',
  status        ENUM('in_progress','finished','abandoned') NOT NULL DEFAULT 'in_progress',
  summary       JSON         NULL COMMENT '本组总结（4.11）',
  started_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  finished_at   DATETIME(3)  NULL,
  PRIMARY KEY (id),
  KEY idx_practice_sessions_owner (owner_user_id, status, started_at),
  CONSTRAINT fk_practice_sessions_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='练习会话';

CREATE TABLE paper_sessions (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  paper_id        BIGINT UNSIGNED NULL COMMENT '试卷被删除时置空，成绩保留',
  subject_id      BIGINT UNSIGNED NOT NULL,
  paper_kind      ENUM('real_exam','ai_standard','ai_targeted','official_mock') NOT NULL,
  paper_title     VARCHAR(128) NOT NULL,
  mode            ENUM('practice','mock') NOT NULL COMMENT '练习模式 / 模拟考试模式',
  status          ENUM('in_progress','paused','grading','graded','abandoned') NOT NULL DEFAULT 'in_progress',
  started_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deadline_at     DATETIME(3)  NULL COMMENT '模拟考试的服务端截止时间，倒计时以它为准',
  interrupted_at  DATETIME(3)  NULL,
  resume_used     TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '系统中断后 10 分钟内恢复只能用一次（PRD 11.9）',
  paused_seconds  INT UNSIGNED NOT NULL DEFAULT 0,
  submitted_at    DATETIME(3)  NULL,
  graded_at       DATETIME(3)  NULL,
  full_score      DECIMAL(6,2) NOT NULL,
  score           DECIMAL(6,2) NULL COMMENT '换算到整卷满分后的得分',
  counts_for_estimate TINYINT(1) NOT NULL DEFAULT 0 COMMENT '只有做完的导入真题卷计入预估分',
  report          JSON         NULL COMMENT '整卷报告与时间分析快照（4.24、4.25）',
  idempotency_key VARCHAR(64)  NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_paper_sessions_idem (owner_user_id, idempotency_key),
  KEY idx_paper_sessions_owner (owner_user_id, subject_id, status),
  CONSTRAINT fk_paper_sessions_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_paper_sessions_paper FOREIGN KEY (paper_id) REFERENCES papers (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='整卷作答；同一用户同一时间只能有一套进行中（应用层保证）';

CREATE TABLE attempts (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NOT NULL,
  question_id      BIGINT UNSIGNED NOT NULL,
  practice_session_id BIGINT UNSIGNED NULL,
  paper_session_id BIGINT UNSIGNED NULL,
  answer_mode      ENUM('choice','typed','voice','photo','self_assess') NOT NULL,
  selected         JSON         NULL COMMENT '客观题选项',
  answer_text      MEDIUMTEXT   NULL COMMENT '作答原文；不得写入日志',
  photo_keys       JSON         NULL COMMENT '手写稿照片 OSS 对象键',
  is_correct       TINYINT(1)   NULL,
  score            DECIMAL(6,2) NULL,
  full_score       DECIMAL(6,2) NULL,
  revealed_answer  TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '点了「看答案」',
  self_assess      ENUM('unknown','vague','mastered') NULL,
  duration_seconds INT UNSIGNED NOT NULL DEFAULT 0,
  timed            TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '限时作答或模拟考试，失分诊断「时间不够」只在这里判定',
  offline          TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '离线作答，联网后服务端复核',
  verified_at      DATETIME(3)  NULL,
  idempotency_key  VARCHAR(64)  NULL,
  answered_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '作答时间（离线作答取客户端时间）',
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_attempts_idem (owner_user_id, idempotency_key),
  KEY idx_attempts_owner_question (owner_user_id, question_id, answered_at),
  KEY idx_attempts_owner_time (owner_user_id, answered_at),
  KEY idx_attempts_paper_session (paper_session_id),
  CONSTRAINT fk_attempts_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_attempts_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE,
  CONSTRAINT fk_attempts_practice_session FOREIGN KEY (practice_session_id) REFERENCES practice_sessions (id) ON DELETE SET NULL,
  CONSTRAINT fk_attempts_paper_session FOREIGN KEY (paper_session_id) REFERENCES paper_sessions (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='每次作答';

CREATE TABLE paper_session_items (
  paper_session_id BIGINT UNSIGNED NOT NULL,
  seq              SMALLINT UNSIGNED NOT NULL,
  question_id      BIGINT UNSIGNED NULL,
  qtype            VARCHAR(16)  NOT NULL,
  section          VARCHAR(32)  NOT NULL,
  score_max        DECIMAL(6,2) NOT NULL,
  attempt_id       BIGINT UNSIGNED NULL,
  draft_text       MEDIUMTEXT   NULL COMMENT '服务端草稿，客户端每 5 秒同步',
  marked           TINYINT(1)   NOT NULL DEFAULT 0,
  time_spent_seconds INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '停留在该题的累计时间（PRD 11.9）',
  PRIMARY KEY (paper_session_id, seq),
  CONSTRAINT fk_psi_session FOREIGN KEY (paper_session_id) REFERENCES paper_sessions (id) ON DELETE CASCADE,
  CONSTRAINT fk_psi_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE SET NULL,
  CONSTRAINT fk_psi_attempt FOREIGN KEY (attempt_id) REFERENCES attempts (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='整卷里每道题的作答状态与用时（4.22 答题卡）';

CREATE TABLE gradings (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NOT NULL,
  attempt_id       BIGINT UNSIGNED NOT NULL,
  kind             ENUM('subjective','norm') NOT NULL COMMENT '主观题批改 / 答题规范批改',
  trigger_reason   ENUM('submit','pending_resubmit','dispute_recheck','rubric_changed','manual_review') NOT NULL DEFAULT 'submit',
  parent_grading_id BIGINT UNSIGNED NULL COMMENT '重批时指向原批改',
  status           ENUM('queued_quota','pending','processing','done','failed') NOT NULL DEFAULT 'pending' COMMENT 'queued_quota = 额度用完存为待批改',
  rubric_version   INT UNSIGNED NULL,
  rubric_snapshot  JSON         NULL COMMENT '批改时的采分点快照与来源（PRD 11.14）',
  score            DECIMAL(6,2) NULL,
  full_score       DECIMAL(6,2) NULL,
  point_results    JSON         NULL COMMENT '逐采分点：hit / partial / miss、得分、引用原话、原因',
  loss             JSON         NULL COMMENT '失分归因：knowledge / norm / time 的扣分与原因',
  suggestions      JSON         NULL,
  model            VARCHAR(64)  NULL,
  prompt_version   VARCHAR(32)  NULL,
  quota_charged    TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '是否扣了批改次数；失败、复核、改采分点重批不扣',
  fail_reason      VARCHAR(255) NULL,
  idempotency_key  VARCHAR(64)  NULL,
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  finished_at      DATETIME(3)  NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_gradings_idem (owner_user_id, idempotency_key),
  KEY idx_gradings_attempt (attempt_id, created_at),
  KEY idx_gradings_owner_status (owner_user_id, status),
  CONSTRAINT fk_gradings_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_gradings_attempt FOREIGN KEY (attempt_id) REFERENCES attempts (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='AI 批改（4.6–4.8）';

CREATE TABLE disputes (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  grading_id      BIGINT UNSIGNED NOT NULL,
  reason          ENUM('hit_missed','rubric_wrong','score_unfair','other') NOT NULL COMMENT '4.8 四选一',
  note            TEXT         NULL,
  allow_access    TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '允许后台查看这道题和答案',
  regrade_id      BIGINT UNSIGNED NULL,
  score_before    DECIMAL(6,2) NULL,
  score_after     DECIMAL(6,2) NULL,
  status          ENUM('rechecking','rechecked','sampled','manual_changed','closed') NOT NULL DEFAULT 'rechecking',
  attribution     ENUM('rubric_incomplete','model_error','answer_insufficient') NULL COMMENT '7.6 人工归因',
  handled_by      BIGINT UNSIGNED NULL,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  resolved_at     DATETIME(3)  NULL,
  PRIMARY KEY (id),
  KEY idx_disputes_owner (owner_user_id),
  KEY idx_disputes_status (status, created_at),
  CONSTRAINT fk_disputes_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_disputes_grading FOREIGN KEY (grading_id) REFERENCES gradings (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='批改异议';

CREATE TABLE kp_mastery (
  owner_user_id      BIGINT UNSIGNED NOT NULL,
  kp_id              BIGINT UNSIGNED NOT NULL,
  m                  DECIMAL(5,2) NOT NULL DEFAULT 0 COMMENT '掌握分 0–100（PRD 11.1）',
  state              ENUM('unlearned','learning','consolidating','mastered') NOT NULL DEFAULT 'unlearned',
  viewed             TINYINT(1)   NOT NULL DEFAULT 0,
  answered           TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '有作答验证记录后，自评不再改 M（D16）',
  last_self_assess   ENUM('unknown','vague','mastered') NULL,
  last_self_assess_at DATETIME(3) NULL,
  correct_dates      JSON         NULL COMMENT '近 30 天答对的日期（北京时间），判定已掌握用',
  next_review_on     DATE         NULL COMMENT '下次复习日（北京时间）',
  interval_step      TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '复习间隔档位：3 → 7 → 15 → 30',
  recite_next_review_on DATE      NULL,
  recite_interval_step TINYINT UNSIGNED NOT NULL DEFAULT 0,
  decay_applied_on   DATE         NULL COMMENT '逾期衰减最后计算到哪天',
  updated_at         DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (owner_user_id, kp_id),
  KEY idx_kp_mastery_review (owner_user_id, next_review_on),
  CONSTRAINT fk_kp_mastery_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_kp_mastery_kp FOREIGN KEY (kp_id) REFERENCES knowledge_points (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='知识点掌握度与复习排期';

CREATE TABLE wrong_book (
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  question_id     BIGINT UNSIGNED NOT NULL,
  status          ENUM('active','eliminated','removed') NOT NULL DEFAULT 'active' COMMENT 'eliminated=自动移出计入已消灭；removed=手动移出',
  added_reason    ENUM('wrong','partial','revealed') NOT NULL,
  last_loss_type  ENUM('knowledge','norm','time') NULL,
  wrong_count     INT UNSIGNED NOT NULL DEFAULT 1,
  last_score_rate DECIMAL(5,4) NULL,
  correct_dates   JSON         NULL COMMENT '收录后答对的日期，2 个不同日期连续答对自动移出（PRD 11.8）',
  next_review_on  DATE         NULL,
  added_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  removed_at      DATETIME(3)  NULL,
  PRIMARY KEY (owner_user_id, question_id),
  KEY idx_wrong_book_status (owner_user_id, status, next_review_on),
  CONSTRAINT fk_wrong_book_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_wrong_book_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='错题本';

CREATE TABLE daily_plans (
  owner_user_id  BIGINT UNSIGNED NOT NULL,
  plan_date      DATE         NOT NULL COMMENT '北京时间日期',
  stage          ENUM('foundation','strengthen','sprint','final') NOT NULL,
  budget_minutes SMALLINT UNSIGNED NOT NULL,
  plan_groups    JSON         NOT NULL COMMENT '各组（新知识点 / 到期复习 / 薄弱查漏 / 背诵）的题目与预计用时快照',
  practice_session_id BIGINT UNSIGNED NULL,
  completed_at   DATETIME(3)  NULL,
  generated_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (owner_user_id, plan_date),
  CONSTRAINT fk_daily_plans_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='每日计划快照（PRD 11.5）';

CREATE TABLE recite_records (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  kp_id         BIGINT UNSIGNED NOT NULL,
  practice_session_id BIGINT UNSIGNED NULL,
  mode          ENUM('cloze','dictation','oral') NOT NULL COMMENT '挖空 / 默写 / 口述',
  result        ENUM('forgot','vague','remembered') NULL,
  coverage      JSON         NULL COMMENT '默写与口述的关键词覆盖',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_recite_records_owner (owner_user_id, created_at),
  CONSTRAINT fk_recite_records_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_recite_records_kp FOREIGN KEY (kp_id) REFERENCES knowledge_points (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='背诵记录（4.14–4.17）';

CREATE TABLE score_estimates (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  subject_id      BIGINT UNSIGNED NOT NULL,
  low             SMALLINT UNSIGNED NOT NULL,
  high            SMALLINT UNSIGNED NOT NULL,
  mid             DECIMAL(6,2) NOT NULL,
  basis_papers    TINYINT UNSIGNED NOT NULL COMMENT '依据了几套真题卷',
  basis_questions SMALLINT UNSIGNED NOT NULL COMMENT '依据了近多少道主观题',
  main_gap_qtype  VARCHAR(16)  NULL COMMENT '主要差在哪种题型',
  details         JSON         NULL,
  trigger_reason  VARCHAR(32)  NOT NULL COMMENT 'paper_graded / material_deleted / rubric_changed 等',
  computed_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_score_estimates_subject (owner_user_id, subject_id, computed_at),
  CONSTRAINT fk_score_estimates_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_score_estimates_subject FOREIGN KEY (subject_id) REFERENCES subjects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='预估分历史（PRD 11.6）；最新一条为当前值，按周取趋势';

-- +goose Down
DROP TABLE score_estimates;
DROP TABLE recite_records;
DROP TABLE daily_plans;
DROP TABLE wrong_book;
DROP TABLE kp_mastery;
DROP TABLE disputes;
DROP TABLE gradings;
DROP TABLE paper_session_items;
DROP TABLE attempts;
DROP TABLE paper_sessions;
DROP TABLE practice_sessions;
