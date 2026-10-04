-- 账号与备考档案（dev-spec 第五节「账号」「备考档案」）。
-- 约定：时间一律 DATETIME(3) 存 UTC；用户内容表都带 owner_user_id，并随用户删除级联删除（注销 7 天冷静期后物理删除）。

-- +goose Up
CREATE TABLE users (
  id                    BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  phone                 VARCHAR(20)  NOT NULL COMMENT '手机号，唯一；日志里不得出现明文',
  nickname              VARCHAR(32)  NOT NULL DEFAULT '',
  avatar_key            VARCHAR(255) NULL COMMENT 'OSS 对象键',
  invite_code           VARCHAR(16)  NOT NULL COMMENT '我的邀请码',
  status                ENUM('active','banned','deleting') NOT NULL DEFAULT 'active',
  onboarding_step       VARCHAR(16)  NOT NULL DEFAULT '1.1' COMMENT '引导中断的步骤；done 表示已完成（PRD 4 主要入口）',
  deletion_requested_at DATETIME(3)  NULL,
  deletion_due_at       DATETIME(3)  NULL COMMENT '冷静期结束时间，到期由定时任务物理删除',
  last_active_at        DATETIME(3)  NULL,
  created_at            DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at            DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_phone (phone),
  UNIQUE KEY uk_users_invite_code (invite_code),
  KEY idx_users_deletion_due (status, deletion_due_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='App 用户';

CREATE TABLE refresh_tokens (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id      BIGINT UNSIGNED NOT NULL,
  device_id    VARCHAR(64)  NOT NULL COMMENT '客户端生成的设备标识',
  device_name  VARCHAR(64)  NOT NULL DEFAULT '',
  platform     ENUM('ios','android','web') NOT NULL,
  token_hash   CHAR(64)     NOT NULL COMMENT '刷新令牌的 SHA-256，原文不落库',
  expires_at   DATETIME(3)  NOT NULL,
  revoked_at   DATETIME(3)  NULL,
  last_used_at DATETIME(3)  NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_refresh_tokens_hash (token_hash),
  KEY idx_refresh_tokens_user_device (user_id, device_id),
  CONSTRAINT fk_refresh_tokens_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='按设备的刷新令牌（6.11 登录设备列表）';

CREATE TABLE agreements (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  kind           ENUM('user','privacy','membership') NOT NULL COMMENT '用户协议 / 隐私政策 / 会员服务协议',
  version        VARCHAR(16)  NOT NULL,
  title          VARCHAR(64)  NOT NULL,
  body           MEDIUMTEXT   NOT NULL,
  change_summary TEXT         NULL COMMENT '0.4b 弹窗里的变更摘要',
  effective_at   DATETIME(3)  NOT NULL,
  published_at   DATETIME(3)  NULL COMMENT '为空表示草稿',
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_agreements_kind_version (kind, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='协议正文与版本（0.4、7.8）';

CREATE TABLE agreement_acceptances (
  user_id      BIGINT UNSIGNED NOT NULL,
  agreement_id BIGINT UNSIGNED NOT NULL,
  accepted_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id, agreement_id),
  CONSTRAINT fk_agreement_acceptances_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_agreement_acceptances_agreement FOREIGN KEY (agreement_id) REFERENCES agreements (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户同意过的协议版本';

CREATE TABLE exam_dates (
  exam_year         SMALLINT UNSIGNED NOT NULL COMMENT '研考年份，如 2027',
  label             VARCHAR(32)  NOT NULL COMMENT '如「2027 研考」',
  first_exam_start  DATE         NOT NULL COMMENT '初试第一天',
  first_exam_end    DATE         NOT NULL,
  subject_exam_date DATE         NOT NULL COMMENT '专业课考试日，倒计时以它为准（PRD 11.4）',
  updated_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (exam_year)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='各年份初试日期，后台 7.8 配置';

CREATE TABLE study_profiles (
  user_id             BIGINT UNSIGNED NOT NULL,
  exam_year           SMALLINT UNSIGNED NOT NULL,
  stage               ENUM('foundation','strengthen','sprint','final') NOT NULL COMMENT '基础期 / 强化期 / 冲刺期 / 考前期',
  stage_manual        TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '用户手动选择后系统只提示、不自动覆盖（PRD 11.4）',
  pending_stage       ENUM('foundation','strengthen','sprint','final') NULL COMMENT '改阶段次日生效时暂存',
  daily_minutes       SMALLINT UNSIGNED NOT NULL DEFAULT 45,
  pending_daily_minutes SMALLINT UNSIGNED NULL COMMENT '改时长次日生效时暂存',
  pending_effective_on DATE        NULL,
  reminder_times      JSON         NULL COMMENT '学习提醒时间，如 ["07:30","20:00"]',
  target_school_major VARCHAR(64)  NULL COMMENT '目标院校专业（选填），官方题库上线提醒用',
  essay_weekly_goal   TINYINT UNSIGNED NOT NULL DEFAULT 2,
  mock_time_reminders TINYINT(1)   NOT NULL DEFAULT 1 COMMENT '模拟考试题型用时提醒（4.21 可在设置里关闭）',
  notify_daily        TINYINT(1)   NOT NULL DEFAULT 1,
  notify_review_due   TINYINT(1)   NOT NULL DEFAULT 1,
  notify_task_done    TINYINT(1)   NOT NULL DEFAULT 1,
  stage_prompted      VARCHAR(16)  NULL COMMENT '已弹过 2.1e 的目标阶段，避免重复弹出',
  created_at          DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at          DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id),
  CONSTRAINT fk_study_profiles_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='备考档案（1.3、6.9）';

CREATE TABLE subjects (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  name            VARCHAR(64)  NOT NULL,
  code            VARCHAR(16)  NULL COMMENT '专业课代码，选填，如 654；7.1、7.10 按它聚合',
  full_score      SMALLINT UNSIGNED NOT NULL DEFAULT 150 COMMENT '100 / 150 / 300',
  target_score    SMALLINT UNSIGNED NULL COMMENT '为空表示没设目标',
  is_essay        TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '作文课：按导入资料自动判断（PRD 11.12）',
  essay_set_by    ENUM('auto','user') NULL COMMENT '作文课判断来源；user 表示用户在 1.7 改过，之后不再自动改',
  sort_order      TINYINT UNSIGNED NOT NULL DEFAULT 0,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_subjects_owner (owner_user_id, sort_order),
  KEY idx_subjects_code (code),
  CONSTRAINT fk_subjects_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户自填的专业课；免费版最多 3 门、会员最多 4 门（PRD 1.1、13.1）';

-- +goose Down
DROP TABLE subjects;
DROP TABLE study_profiles;
DROP TABLE exam_dates;
DROP TABLE agreement_acceptances;
DROP TABLE agreements;
DROP TABLE refresh_tokens;
DROP TABLE users;
