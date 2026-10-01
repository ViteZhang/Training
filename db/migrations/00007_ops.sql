-- 配置、消息、导出、后台、AI 账本、官方题库（dev-spec 第五节「配置」「后台」「AI 账本」「消息与导出」「官方题库」）。

-- +goose Up
CREATE TABLE rule_params (
  param_key   VARCHAR(64)  NOT NULL,
  value       JSON         NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  version     INT UNSIGNED NOT NULL DEFAULT 1,
  updated_by  BIGINT UNSIGNED NULL,
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (param_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='规则参数与额度、价格（PRD 第 11、13 节初始值），后台 7.8 可改';

CREATE TABLE rule_param_history (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  param_key   VARCHAR(64)  NOT NULL,
  version     INT UNSIGNED NOT NULL,
  value       JSON         NOT NULL,
  updated_by  BIGINT UNSIGNED NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_rule_param_history_key (param_key, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE feature_flags (
  flag_key        VARCHAR(64)  NOT NULL,
  description     VARCHAR(255) NOT NULL DEFAULT '',
  enabled_for_all TINYINT(1)   NOT NULL DEFAULT 0,
  updated_by      BIGINT UNSIGNED NULL,
  updated_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (flag_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='功能开关：全部 / 指定用户（ADR 0009）';

CREATE TABLE feature_flag_users (
  flag_key   VARCHAR(64)  NOT NULL,
  user_id    BIGINT UNSIGNED NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (flag_key, user_id),
  CONSTRAINT fk_ffu_flag FOREIGN KEY (flag_key) REFERENCES feature_flags (flag_key) ON DELETE CASCADE,
  CONSTRAINT fk_ffu_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE app_versions (
  platform       ENUM('ios','android') NOT NULL,
  latest_version VARCHAR(16)  NOT NULL,
  min_version    VARCHAR(16)  NOT NULL COMMENT '低于它强制更新（0.6b）',
  download_url   VARCHAR(255) NOT NULL,
  release_notes  TEXT         NULL,
  updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (platform)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE messages (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  mtype         ENUM('import_done','review_needed','review_due','grading_done','paper_graded','essay_graded','export_ready','agreement_update','announcement','support_reply','content_accessed','membership','stage_change','official_bank') NOT NULL,
  title         VARCHAR(128) NOT NULL,
  body          TEXT         NOT NULL,
  link          JSON         NULL COMMENT '跳转目标 {page, params}',
  dedupe_key    VARCHAR(96)  NULL COMMENT '同一事件只发一条',
  read_at       DATETIME(3)  NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_messages_dedupe (owner_user_id, dedupe_key),
  KEY idx_messages_owner (owner_user_id, created_at),
  CONSTRAINT fk_messages_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='消息中心（2.3），保留 30 天';

CREATE TABLE announcements (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  title         VARCHAR(128) NOT NULL,
  body          TEXT         NOT NULL,
  audience      JSON         NOT NULL COMMENT '发送对象条件',
  with_popup    TINYINT(1)   NOT NULL DEFAULT 0,
  scheduled_at  DATETIME(3)  NULL,
  sent_at       DATETIME(3)  NULL,
  sent_count    INT UNSIGNED NOT NULL DEFAULT 0,
  created_by    BIGINT UNSIGNED NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='后台手动公告（7.9）';

CREATE TABLE export_jobs (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  subject_id    BIGINT UNSIGNED NOT NULL,
  options       JSON         NOT NULL,
  format        ENUM('pdf','docx') NOT NULL,
  status        ENUM('queued','running','done','failed') NOT NULL DEFAULT 'queued',
  object_key    VARCHAR(255) NULL,
  page_estimate INT UNSIGNED NULL,
  expires_at    DATETIME(3)  NULL COMMENT '24 小时后删除 OSS 对象',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  finished_at   DATETIME(3)  NULL,
  PRIMARY KEY (id),
  KEY idx_export_jobs_owner (owner_user_id, created_at),
  CONSTRAINT fk_export_jobs_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='导出题库（6.4）';

CREATE TABLE feedbacks (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  ftype         ENUM('suggestion','recognition','grading','bug','infringement') NOT NULL COMMENT '功能建议 / 识别不准 / 批改不准 / 出现问题 / 侵权投诉',
  content       TEXT         NOT NULL,
  screenshot_keys JSON       NULL,
  allow_access  TINYINT(1)   NOT NULL DEFAULT 0,
  related_material_id BIGINT UNSIGNED NULL,
  status        ENUM('open','replied','closed') NOT NULL DEFAULT 'open',
  reply         TEXT         NULL,
  replied_by    BIGINT UNSIGNED NULL,
  replied_at    DATETIME(3)  NULL,
  satisfaction  TINYINT UNSIGNED NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_feedbacks_owner (owner_user_id),
  KEY idx_feedbacks_status (status, created_at),
  CONSTRAINT fk_feedbacks_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='意见反馈（6.13、7.7）';

CREATE TABLE admin_users (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username      VARCHAR(32)  NOT NULL,
  display_name  VARCHAR(32)  NOT NULL,
  password_hash VARCHAR(255) NOT NULL COMMENT 'bcrypt',
  phone         VARCHAR(20)  NOT NULL COMMENT '两步验证短信',
  roles         JSON         NOT NULL COMMENT 'admin / support / content_lead / content_editor / analyst',
  status        ENUM('active','disabled') NOT NULL DEFAULT 'active',
  last_login_at DATETIME(3)  NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_admin_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='后台账号（7.15）';

CREATE TABLE admin_sessions (
  token_hash  CHAR(64)     NOT NULL,
  admin_id    BIGINT UNSIGNED NOT NULL,
  expires_at  DATETIME(3)  NOT NULL COMMENT '8 小时过期',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (token_hash),
  KEY idx_admin_sessions_admin (admin_id),
  CONSTRAINT fk_admin_sessions_admin FOREIGN KEY (admin_id) REFERENCES admin_users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE admin_audit_logs (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  admin_id    BIGINT UNSIGNED NOT NULL,
  action      VARCHAR(64)  NOT NULL,
  target_type VARCHAR(32)  NULL,
  target_id   VARCHAR(64)  NULL,
  detail      JSON         NULL COMMENT '不含用户内容原文',
  ip          VARCHAR(45)  NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_admin_audit_logs_time (created_at),
  KEY idx_admin_audit_logs_admin (admin_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='后台操作日志，保留 180 天，不提供删除接口';

CREATE TABLE content_access_grants (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id       BIGINT UNSIGNED NOT NULL,
  source        ENUM('dispute','feedback') NOT NULL,
  source_id     BIGINT UNSIGNED NOT NULL,
  scope         JSON         NOT NULL COMMENT '可查看的对象 [{type,id}]',
  granted_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  expires_at    DATETIME(3)  NOT NULL COMMENT '授权后 72 小时',
  revoked_at    DATETIME(3)  NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_cag_source (source, source_id),
  KEY idx_cag_user (user_id),
  CONSTRAINT fk_cag_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户授权后台查看内容（PRD 10.1）';

CREATE TABLE content_access_logs (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  grant_id    BIGINT UNSIGNED NOT NULL,
  admin_id    BIGINT UNSIGNED NOT NULL,
  user_id     BIGINT UNSIGNED NOT NULL,
  target_type VARCHAR(32)  NOT NULL,
  target_id   BIGINT UNSIGNED NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_cal_grant (grant_id),
  KEY idx_cal_time (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='每次查看都记一条并通知用户，保留 180 天';

CREATE TABLE ai_calls (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  capability     VARCHAR(32)  NOT NULL,
  model          VARCHAR(64)  NOT NULL,
  prompt_version VARCHAR(32)  NOT NULL,
  user_hash      CHAR(64)     NULL COMMENT '用户 ID 的哈希，不存输入输出原文',
  input_tokens   INT UNSIGNED NOT NULL DEFAULT 0,
  output_tokens  INT UNSIGNED NOT NULL DEFAULT 0,
  cost_micro_yuan BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '费用，单位百万分之一元',
  latency_ms     INT UNSIGNED NOT NULL DEFAULT 0,
  success        TINYINT(1)   NOT NULL,
  error_kind     VARCHAR(32)  NULL COMMENT 'timeout / schema_invalid / quote_not_found 等',
  retried        TINYINT(1)   NOT NULL DEFAULT 0,
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_ai_calls_cap_time (capability, created_at),
  KEY idx_ai_calls_time (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='AI 调用账本（7.8）';

CREATE TABLE ai_rollouts (
  capability        VARCHAR(32)  NOT NULL,
  stable_model      VARCHAR(64)  NOT NULL,
  stable_prompt     VARCHAR(32)  NOT NULL,
  candidate_model   VARCHAR(64)  NULL,
  candidate_prompt  VARCHAR(32)  NULL,
  candidate_percent TINYINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '按用户比例放量 0–100',
  updated_by        BIGINT UNSIGNED NULL,
  updated_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (capability)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='AI 能力的模型、提示词版本与灰度（7.8）';

CREATE TABLE stats_hourly (
  metric     VARCHAR(48)  NOT NULL,
  dimension  VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '如专业课代码、导入方式',
  bucket     DATETIME     NOT NULL COMMENT '小时或日的起点（北京时间）',
  value      DECIMAL(18,4) NOT NULL,
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (metric, dimension, bucket)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Worker 每小时汇总的统计（7.1、7.10 只读它，不扫用户内容表）';

CREATE TABLE official_projects (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  school        VARCHAR(64)  NOT NULL,
  major         VARCHAR(64)  NOT NULL,
  subject_code  VARCHAR(16)  NOT NULL,
  subject_name  VARCHAR(64)  NOT NULL,
  bank_id       BIGINT UNSIGNED NULL,
  stage         ENUM('initiated','materials','framework','producing','reviewing','published') NOT NULL DEFAULT 'initiated',
  editor_ids    JSON         NULL,
  authorized_materials JSON  NULL COMMENT '授权资料清单（须有授权或为公开真题）',
  created_by    BIGINT UNSIGNED NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_official_projects_code (subject_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='官方题库立项（7.11）';

CREATE TABLE official_drafts (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id   BIGINT UNSIGNED NOT NULL,
  editor_id    BIGINT UNSIGNED NOT NULL,
  entity_type  ENUM('knowledge_point','question','paper','profile') NOT NULL,
  entity_id    BIGINT UNSIGNED NULL COMMENT '修订已有条目时指向它',
  change_type  ENUM('add','supplement','revise','merge','split','offline') NOT NULL,
  payload      JSON         NOT NULL,
  status       ENUM('draft','submitted','approved','rejected','published') NOT NULL DEFAULT 'draft',
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_official_drafts_project (project_id, status),
  CONSTRAINT fk_official_drafts_project FOREIGN KEY (project_id) REFERENCES official_projects (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='官方内容草稿（7.12）';

CREATE TABLE review_tasks (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  draft_id    BIGINT UNSIGNED NOT NULL,
  reviewer_id BIGINT UNSIGNED NULL,
  mode        ENUM('full','sampled','skipped') NOT NULL COMMENT '新编辑前 3 次全量，之后抽检 30%',
  decision    ENUM('pending','approved','rejected','edited') NOT NULL DEFAULT 'pending',
  reason      VARCHAR(255) NULL COMMENT '退回必须填原因',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  decided_at  DATETIME(3)  NULL,
  PRIMARY KEY (id),
  KEY idx_review_tasks_decision (decision, created_at),
  CONSTRAINT fk_review_tasks_draft FOREIGN KEY (draft_id) REFERENCES official_drafts (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='审核队列（7.13）';

CREATE TABLE official_versions (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id    BIGINT UNSIGNED NOT NULL,
  version       VARCHAR(16)  NOT NULL,
  changelog     JSON         NOT NULL COMMENT '新增、修订、合并、下线清单，标出采分点变化',
  published_by  BIGINT UNSIGNED NOT NULL,
  published_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  rolled_back_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_official_versions (project_id, version),
  CONSTRAINT fk_official_versions_project FOREIGN KEY (project_id) REFERENCES official_projects (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='官方题库版本（7.14）';

CREATE TABLE bank_subscriptions (
  owner_user_id BIGINT UNSIGNED NOT NULL,
  bank_id       BIGINT UNSIGNED NOT NULL COMMENT '官方题库',
  subject_id    BIGINT UNSIGNED NOT NULL COMMENT '挂到用户的哪门专业课',
  use_official_profile TINYINT(1) NOT NULL DEFAULT 1 COMMENT '考情与组卷优先用官方命题画像，可切回自己的真题统计',
  added_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (owner_user_id, bank_id),
  CONSTRAINT fk_bank_subscriptions_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_bank_subscriptions_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE,
  CONSTRAINT fk_bank_subscriptions_subject FOREIGN KEY (subject_id) REFERENCES subjects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户添加的官方题库（PRD 11.15）';

-- +goose Down
DROP TABLE bank_subscriptions;
DROP TABLE official_versions;
DROP TABLE review_tasks;
DROP TABLE official_drafts;
DROP TABLE official_projects;
DROP TABLE stats_hourly;
DROP TABLE ai_rollouts;
DROP TABLE ai_calls;
DROP TABLE content_access_logs;
DROP TABLE content_access_grants;
DROP TABLE admin_audit_logs;
DROP TABLE admin_sessions;
DROP TABLE admin_users;
DROP TABLE feedbacks;
DROP TABLE export_jobs;
DROP TABLE announcements;
DROP TABLE messages;
DROP TABLE app_versions;
DROP TABLE feature_flag_users;
DROP TABLE feature_flags;
DROP TABLE rule_param_history;
DROP TABLE rule_params;
