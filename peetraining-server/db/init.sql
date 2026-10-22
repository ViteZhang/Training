-- peetraining-server 本地开发数据库初始化脚本
-- 用途：手动初始化独立的 training_dev 数据库；不要在生产库直接执行。
-- 执行方式：mysql -u root -p < db/init.sql
-- 脚本包含 00001~00018 的 Goose Up 内容及版本记录，执行后 api migrate up 不会重复建表。
SET NAMES utf8mb4;
SET time_zone = '+00:00';
CREATE DATABASE IF NOT EXISTS `training_dev` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
USE `training_dev`;

-- Goose 迁移版本表（与 github.com/pressly/goose/v3 MySQL 方言一致）。
CREATE TABLE IF NOT EXISTS `goose_db_version` (
  id bigint(20) unsigned NOT NULL AUTO_INCREMENT,
  version_id bigint NOT NULL,
  is_applied boolean NOT NULL,
  tstamp datetime NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id)
) ENGINE=InnoDB;
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (0, TRUE);

-- ===== 迁移 00001：00001_baseline.sql =====
SELECT 1;
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (1, TRUE);

-- ===== 迁移 00002：00002_account_profile.sql =====
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
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (2, TRUE);

-- ===== 迁移 00003：00003_bank_content.sql =====
CREATE TABLE banks (
  id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  source            ENUM('user','official') NOT NULL,
  owner_user_id     BIGINT UNSIGNED NULL COMMENT '自建题库的所有者；官方题库为空',
  subject_id        BIGINT UNSIGNED NULL COMMENT '自建题库对应的专业课，一门课一个题库',
  title             VARCHAR(128) NOT NULL DEFAULT '',
  school_major_tag  VARCHAR(64)  NULL COMMENT '院校专业标签，官方题库上线提醒用',
  subject_code      VARCHAR(16)  NULL,
  created_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at        DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_banks_subject (subject_id),
  KEY idx_banks_owner (owner_user_id),
  KEY idx_banks_source_code (source, subject_code),
  CONSTRAINT fk_banks_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_banks_subject FOREIGN KEY (subject_id) REFERENCES subjects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='题库：知识点、题目、试卷、资料的容器';

CREATE TABLE materials (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id  BIGINT UNSIGNED NOT NULL,
  bank_id        BIGINT UNSIGNED NOT NULL,
  category       ENUM('question','reference','essay') NULL COMMENT '题目类 / 参考类 / 作文类；上传时按用户选择，解析后由 AI 判断修正',
  sub_type       VARCHAR(32)  NULL COMMENT '真题汇编、习题集、参考书、讲义、笔记、作文真题、范文、写作笔记、评分细则',
  file_name      VARCHAR(255) NOT NULL,
  format         ENUM('docx','xlsx','pdf','image','text') NOT NULL,
  object_key     VARCHAR(255) NULL COMMENT 'OSS 私有桶对象键；粘贴文字为空',
  size_bytes     BIGINT UNSIGNED NOT NULL DEFAULT 0,
  page_count     INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '实际页数（图片为张数）',
  billed_pages   INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '计费页数（PRD 11.12）',
  sha256         CHAR(64)     NOT NULL COMMENT '内容哈希，同一用户重复上传直接提示',
  status         ENUM('uploading','uploaded','parsing','parsed','partial','failed','rejected') NOT NULL DEFAULT 'uploading',
  fail_reason    VARCHAR(255) NULL COMMENT '面向用户的失败原因',
  question_count INT UNSIGNED NOT NULL DEFAULT 0,
  kp_count       INT UNSIGNED NOT NULL DEFAULT 0,
  right_confirmed_at DATETIME(3) NOT NULL COMMENT '用户勾选「对资料有合法使用权」的时间（1.5）',
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_materials_owner_sha (owner_user_id, sha256),
  KEY idx_materials_bank (bank_id, created_at),
  CONSTRAINT fk_materials_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_materials_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='用户上传的资料文件';

CREATE TABLE material_pages (
  material_id    BIGINT UNSIGNED NOT NULL,
  page_no        INT UNSIGNED NOT NULL COMMENT '从 1 开始；出处页码都靠它',
  owner_user_id  BIGINT UNSIGNED NOT NULL,
  text           MEDIUMTEXT   NOT NULL,
  tables         JSON         NULL,
  low_confidence JSON         NULL COMMENT '低置信度位置 [{start,end}]',
  PRIMARY KEY (material_id, page_no),
  KEY idx_material_pages_owner (owner_user_id),
  FULLTEXT KEY ft_material_pages_text (text) WITH PARSER ngram,
  CONSTRAINT fk_material_pages_material FOREIGN KEY (material_id) REFERENCES materials (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='资料逐页识别文字（3.7 原文查看、3.2 搜索）';

CREATE TABLE import_jobs (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  bank_id         BIGINT UNSIGNED NOT NULL,
  mode            ENUM('question','reference','essay') NOT NULL COMMENT '1.4 选的导入方式',
  status          ENUM('queued','running','reviewing','confirmed','failed','canceled') NOT NULL DEFAULT 'queued' COMMENT 'reviewing=已有可确认的条目',
  reserved_pages  INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '预占的解析额度',
  billed_pages    INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '最终扣除的页数（失败页已退回）',
  prompt_versions JSON         NULL COMMENT '各步用到的提示词版本',
  fail_reason     VARCHAR(255) NULL,
  started_at      DATETIME(3)  NULL,
  finished_at     DATETIME(3)  NULL,
  confirmed_at    DATETIME(3)  NULL,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_import_jobs_owner (owner_user_id, created_at),
  CONSTRAINT fk_import_jobs_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_import_jobs_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='导入任务：一次选择的一批文件';

CREATE TABLE import_job_materials (
  job_id        BIGINT UNSIGNED NOT NULL,
  material_id   BIGINT UNSIGNED NOT NULL,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  step          ENUM('queued','extract','moderate','split','structure','match','rubric','tag','dedupe','done') NOT NULL DEFAULT 'queued' COMMENT 'dev-spec 第六节流水线步骤，失败只重跑这一步',
  status        ENUM('pending','running','done','partial','failed') NOT NULL DEFAULT 'pending',
  attempts      TINYINT UNSIGNED NOT NULL DEFAULT 0,
  fail_reason   VARCHAR(255) NULL,
  failed_pages  JSON         NULL COMMENT '识别失败的页，额度退回',
  eta_seconds   INT UNSIGNED NULL,
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (job_id, material_id),
  KEY idx_import_job_materials_material (material_id),
  CONSTRAINT fk_ijm_job FOREIGN KEY (job_id) REFERENCES import_jobs (id) ON DELETE CASCADE,
  CONSTRAINT fk_ijm_material FOREIGN KEY (material_id) REFERENCES materials (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='导入任务里每个文件的进度（1.6）';

CREATE TABLE import_items (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NOT NULL,
  job_id           BIGINT UNSIGNED NOT NULL,
  material_id      BIGINT UNSIGNED NULL,
  item_type        ENUM('question','knowledge_point','essay_topic','essay_rubric','writing_method','essay_material','model_essay') NOT NULL,
  seq              INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '在任务内的顺序',
  payload          JSON         NOT NULL COMMENT 'AI 产出的待确认内容；用户在 1.7 / 1.7b 修改后覆盖',
  confidence       DECIMAL(4,3) NULL,
  status           ENUM('pending','confirmed','edited','deleted') NOT NULL DEFAULT 'pending',
  needs_review     TINYINT(1)   NOT NULL DEFAULT 0,
  review_reasons   JSON         NULL COMMENT 'low_confidence / missing_answer / rubric_unconfirmed / rubric_sum_mismatch / duplicate',
  duplicate_of_question_id BIGINT UNSIGNED NULL COMMENT '疑似重复的已有题目',
  dedupe_key       CHAR(64)     NULL COMMENT '同一任务重跑时防止重复写入',
  created_entity_id BIGINT UNSIGNED NULL COMMENT '确认入库后生成的题目 / 知识点等 ID',
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_import_items_dedupe (job_id, dedupe_key),
  KEY idx_import_items_job (job_id, status, seq),
  KEY idx_import_items_owner (owner_user_id),
  CONSTRAINT fk_import_items_job FOREIGN KEY (job_id) REFERENCES import_jobs (id) ON DELETE CASCADE,
  CONSTRAINT fk_import_items_material FOREIGN KEY (material_id) REFERENCES materials (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='AI 产出的待确认条目（1.7）';

CREATE TABLE knowledge_points (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NULL COMMENT '官方题库为空',
  bank_id          BIGINT UNSIGNED NOT NULL,
  parent_id        BIGINT UNSIGNED NULL,
  level            ENUM('section','chapter','point') NOT NULL COMMENT '板块 → 章节 → 知识点',
  name             VARCHAR(128) NOT NULL,
  original_text    TEXT         NULL COMMENT '原文表述，必须能在资料里逐字找到',
  source_material_id BIGINT UNSIGNED NULL,
  source_page      INT UNSIGNED NULL,
  origin           ENUM('user_confirmed','ai_extracted','ai_generated','imported','official') NOT NULL,
  needs_review     TINYINT(1)   NOT NULL DEFAULT 0,
  ai_explanation   TEXT         NULL COMMENT 'AI 解读，首次生成后缓存（3.4）',
  ai_explanation_at DATETIME(3) NULL,
  exam_count       INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '在用户真题中出现次数（缓存，导入与删除时重算）',
  official_kp_id   BIGINT UNSIGNED NULL COMMENT '与官方知识点合并后的对应关系（PRD 11.15）',
  sort_order       INT UNSIGNED NOT NULL DEFAULT 0,
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_kps_bank_parent (bank_id, parent_id, sort_order),
  KEY idx_kps_owner (owner_user_id),
  FULLTEXT KEY ft_kps (name, original_text) WITH PARSER ngram,
  CONSTRAINT fk_kps_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_kps_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE,
  CONSTRAINT fk_kps_parent FOREIGN KEY (parent_id) REFERENCES knowledge_points (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='知识点树（3.1、3.4）';

CREATE TABLE kp_sources (
  kp_id         BIGINT UNSIGNED NOT NULL,
  material_id   BIGINT UNSIGNED NOT NULL,
  page_no       INT UNSIGNED NOT NULL DEFAULT 0,
  owner_user_id BIGINT UNSIGNED NULL,
  PRIMARY KEY (kp_id, material_id, page_no),
  KEY idx_kp_sources_material (material_id),
  CONSTRAINT fk_kp_sources_kp FOREIGN KEY (kp_id) REFERENCES knowledge_points (id) ON DELETE CASCADE,
  CONSTRAINT fk_kp_sources_material FOREIGN KEY (material_id) REFERENCES materials (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='知识点来自哪些资料；删除资料时只删除只来自它的知识点（PRD 11.12）';

CREATE TABLE kp_relations (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NULL,
  bank_id       BIGINT UNSIGNED NOT NULL,
  kp_a_id       BIGINT UNSIGNED NOT NULL,
  kp_b_id       BIGINT UNSIGNED NOT NULL,
  relation_type ENUM('contrast','component','sibling','related') NOT NULL COMMENT '易混对比 / 组成要素 / 同章并列 / 相关',
  origin        ENUM('user_confirmed','ai_generated','official') NOT NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_kp_relations_pair (kp_a_id, kp_b_id),
  KEY idx_kp_relations_bank (bank_id),
  CONSTRAINT fk_kp_relations_a FOREIGN KEY (kp_a_id) REFERENCES knowledge_points (id) ON DELETE CASCADE,
  CONSTRAINT fk_kp_relations_b FOREIGN KEY (kp_b_id) REFERENCES knowledge_points (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='知识关联（3.9 图谱）；kp_a_id < kp_b_id';

CREATE TABLE questions (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NULL,
  bank_id          BIGINT UNSIGNED NOT NULL,
  qtype            ENUM('single_choice','multi_choice','true_false','fill_blank','term','short_answer','discussion','essay','calculation','other') NOT NULL COMMENT 'term 名词解释 / short_answer 简答 / discussion 论述 / essay 作文',
  stem             TEXT         NOT NULL,
  options          JSON         NULL COMMENT '客观题选项 [{key,text}]',
  answer           MEDIUMTEXT   NULL COMMENT '客观题正确答案或主观题参考答案',
  answer_origin    ENUM('imported','ai_generated','user_confirmed','official') NULL,
  analysis         TEXT         NULL COMMENT '解析',
  score            DECIMAL(6,2) NULL COMMENT '分值',
  difficulty       ENUM('easy','medium','hard') NULL COMMENT '首个版本不识别，按中（D16）',
  source           ENUM('exam','exercise','ai_generated','official') NOT NULL,
  exam_year        SMALLINT UNSIGNED NULL,
  question_no      VARCHAR(16)  NULL COMMENT '原卷题号，用于答案配对',
  is_recollection  TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '回忆版，不计入考情统计',
  source_material_id BIGINT UNSIGNED NULL,
  source_page      INT UNSIGNED NULL,
  generated_from_kp_id BIGINT UNSIGNED NULL COMMENT 'AI 出题依据的知识点',
  content_hash     CHAR(64)     NOT NULL COMMENT '题干归一化后的哈希，用于去重',
  rubric_version   INT UNSIGNED NOT NULL DEFAULT 1 COMMENT '采分点每次修改加 1，批改记录版本',
  status           ENUM('active','offline','deleted') NOT NULL DEFAULT 'active' COMMENT 'AI 题被报错 3 次自动下线',
  needs_review     TINYINT(1)   NOT NULL DEFAULT 0,
  review_reasons   JSON         NULL,
  report_count     INT UNSIGNED NOT NULL DEFAULT 0,
  official_question_id BIGINT UNSIGNED NULL,
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_questions_bank_type (bank_id, status, qtype),
  KEY idx_questions_owner (owner_user_id),
  KEY idx_questions_hash (bank_id, content_hash),
  KEY idx_questions_material (source_material_id),
  FULLTEXT KEY ft_questions (stem) WITH PARSER ngram,
  CONSTRAINT fk_questions_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_questions_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='题目';

CREATE TABLE rubric_points (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NULL,
  question_id   BIGINT UNSIGNED NULL COMMENT '属于题目的采分点',
  kp_id         BIGINT UNSIGNED NULL COMMENT '属于知识点的采分点（背诵挖空、出题依据）',
  seq           SMALLINT UNSIGNED NOT NULL,
  content       TEXT         NOT NULL,
  keywords      JSON         NULL COMMENT '采分关键词，挖空与默写比对用',
  score         DECIMAL(6,2) NULL,
  origin        ENUM('user_confirmed','ai_extracted','ai_generated','official') NOT NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_rubric_points_question (question_id, seq),
  KEY idx_rubric_points_kp (kp_id, seq),
  CONSTRAINT fk_rubric_points_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE,
  CONSTRAINT fk_rubric_points_kp FOREIGN KEY (kp_id) REFERENCES knowledge_points (id) ON DELETE CASCADE,
  CONSTRAINT ck_rubric_points_owner CHECK (question_id IS NOT NULL OR kp_id IS NOT NULL)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='采分点：当前版本；历史版本以快照存在批改记录里';

CREATE TABLE question_kps (
  question_id   BIGINT UNSIGNED NOT NULL,
  kp_id         BIGINT UNSIGNED NOT NULL,
  is_primary    TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '主知识点全额更新掌握分，其余减半（PRD 11.1）',
  owner_user_id BIGINT UNSIGNED NULL,
  PRIMARY KEY (question_id, kp_id),
  KEY idx_question_kps_kp (kp_id),
  CONSTRAINT fk_question_kps_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE,
  CONSTRAINT fk_question_kps_kp FOREIGN KEY (kp_id) REFERENCES knowledge_points (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='题目与知识点多对多';

CREATE TABLE question_reports (
  question_id   BIGINT UNSIGNED NOT NULL,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  reason        VARCHAR(255) NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (question_id, owner_user_id),
  CONSTRAINT fk_question_reports_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE,
  CONSTRAINT fk_question_reports_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='「题目有问题」报错（4.3）';

CREATE TABLE papers (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id    BIGINT UNSIGNED NULL,
  bank_id          BIGINT UNSIGNED NOT NULL,
  kind             ENUM('real_exam','ai_standard','ai_targeted','official_mock') NOT NULL,
  title            VARCHAR(128) NOT NULL,
  exam_year        SMALLINT UNSIGNED NULL,
  full_score       DECIMAL(6,2) NOT NULL COMMENT '整卷满分（取自真题）',
  actual_score     DECIMAL(6,2) NOT NULL COMMENT '实际收录题目的满分；缺题时小于 full_score，成绩换算到整卷满分（PRD 11.6）',
  duration_minutes SMALLINT UNSIGNED NOT NULL DEFAULT 180,
  structure        JSON         NOT NULL COMMENT '题型结构 [{qtype,count,score_each,total}]',
  missing_note     VARCHAR(255) NULL COMMENT '缺题说明',
  source_material_id BIGINT UNSIGNED NULL,
  created_at       DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_papers_bank (bank_id, kind, exam_year),
  KEY idx_papers_owner (owner_user_id),
  CONSTRAINT fk_papers_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_papers_bank FOREIGN KEY (bank_id) REFERENCES banks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='真题卷与 AI 组卷';

CREATE TABLE paper_questions (
  paper_id    BIGINT UNSIGNED NOT NULL,
  seq         SMALLINT UNSIGNED NOT NULL,
  question_id BIGINT UNSIGNED NOT NULL,
  section     VARCHAR(32)  NOT NULL COMMENT '题型分组，如「一、名词解释」',
  score       DECIMAL(6,2) NOT NULL,
  PRIMARY KEY (paper_id, seq),
  KEY idx_paper_questions_question (question_id),
  CONSTRAINT fk_paper_questions_paper FOREIGN KEY (paper_id) REFERENCES papers (id) ON DELETE CASCADE,
  CONSTRAINT fk_paper_questions_question FOREIGN KEY (question_id) REFERENCES questions (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (3, TRUE);

-- ===== 迁移 00004：00004_learning.sql =====
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
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (4, TRUE);

-- ===== 迁移 00005：00005_essay.sql =====
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
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (5, TRUE);

-- ===== 迁移 00006：00006_commerce.sql =====
CREATE TABLE quota_counters (
  owner_user_id BIGINT UNSIGNED NOT NULL,
  quota_type    ENUM('parse_pages','import_questions','grading','ai_questions','paper_grading','essay_grading') NOT NULL,
  period_key    VARCHAR(16)  NOT NULL COMMENT 'total / 2026-10（月）/ 2026-10-01（日）/ 2026-W40（周），按北京时间',
  used          INT UNSIGNED NOT NULL DEFAULT 0,
  reserved      INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '预占未结算（解析）',
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (owner_user_id, quota_type, period_key),
  CONSTRAINT fk_quota_counters_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='额度计数，扣减时 SELECT ... FOR UPDATE 防并发超额';

CREATE TABLE quota_ledger (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  quota_type      ENUM('parse_pages','import_questions','grading','ai_questions','paper_grading','essay_grading') NOT NULL,
  period_key      VARCHAR(16)  NOT NULL,
  action          ENUM('reserve','settle','release','consume','refund','grant') NOT NULL,
  amount          INT NOT NULL,
  ref_type        VARCHAR(32)  NULL COMMENT 'import_job / grading / paper_session / essay / admin',
  ref_id          BIGINT UNSIGNED NULL,
  idempotency_key VARCHAR(96)  NOT NULL,
  note            VARCHAR(255) NULL,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_quota_ledger_idem (owner_user_id, idempotency_key),
  KEY idx_quota_ledger_owner (owner_user_id, quota_type, created_at),
  CONSTRAINT fk_quota_ledger_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='额度流水；扣额度与写结果在同一事务';

CREATE TABLE memberships (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  tier          ENUM('sprint','season','monthly','gift') NOT NULL COMMENT '冲刺卡 / 考季卡 / 月卡 / 赠送天数',
  source        ENUM('redeem','order','invite','survey','admin') NOT NULL,
  source_ref    BIGINT UNSIGNED NULL,
  starts_at     DATETIME(3)  NOT NULL COMMENT '叠加到当前会员之后',
  ends_at       DATETIME(3)  NOT NULL,
  revoked_at    DATETIME(3)  NULL COMMENT '作废兑换码或退款后收回',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_memberships_owner (owner_user_id, ends_at),
  CONSTRAINT fk_memberships_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='会员时段，当前会员 = 未收回时段里覆盖当前时间的那段';

CREATE TABLE redeem_batches (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name            VARCHAR(64)  NOT NULL,
  tier            ENUM('sprint','season','monthly','gift') NOT NULL,
  days            SMALLINT UNSIGNED NULL COMMENT 'gift 或自定义天数',
  quantity        INT UNSIGNED NOT NULL,
  code_expires_at DATETIME(3)  NOT NULL,
  channel         VARCHAR(32)  NOT NULL DEFAULT '',
  status          ENUM('active','disabled') NOT NULL DEFAULT 'active',
  created_by      BIGINT UNSIGNED NULL,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='兑换码批次（7.4）';

CREATE TABLE redeem_codes (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  batch_id      BIGINT UNSIGNED NOT NULL,
  code_hash     CHAR(64)     NOT NULL COMMENT '兑换码只存哈希',
  code_tail     CHAR(3)      NOT NULL COMMENT '末 3 位，后台单码查询时辅助核对',
  status        ENUM('unused','used','void') NOT NULL DEFAULT 'unused',
  used_by       BIGINT UNSIGNED NULL,
  used_at       DATETIME(3)  NULL,
  membership_id BIGINT UNSIGNED NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_redeem_codes_hash (code_hash),
  KEY idx_redeem_codes_batch (batch_id, status),
  CONSTRAINT fk_redeem_codes_batch FOREIGN KEY (batch_id) REFERENCES redeem_batches (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE orders (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  order_no       VARCHAR(32)  NOT NULL,
  owner_user_id  BIGINT UNSIGNED NULL COMMENT '注销后置空，订单保留用于财务',
  tier           ENUM('sprint','season','monthly') NOT NULL,
  channel        ENUM('wechat','alipay','apple_iap') NOT NULL,
  amount_cents   INT UNSIGNED NOT NULL,
  status         ENUM('created','paid','closed','refunding','refunded') NOT NULL DEFAULT 'created',
  transaction_id VARCHAR(64)  NULL,
  notify_hash    CHAR(64)     NULL COMMENT '回调原文哈希',
  membership_id  BIGINT UNSIGNED NULL,
  paid_at        DATETIME(3)  NULL,
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_orders_no (order_no),
  UNIQUE KEY uk_orders_txn (channel, transaction_id),
  KEY idx_orders_owner (owner_user_id),
  KEY idx_orders_status (status, created_at),
  CONSTRAINT fk_orders_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='订单（支付开关打开后使用）';

CREATE TABLE refunds (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  order_id    BIGINT UNSIGNED NOT NULL,
  reason      VARCHAR(255) NOT NULL,
  status      ENUM('pending','approved','rejected','done') NOT NULL DEFAULT 'pending',
  handled_by  BIGINT UNSIGNED NULL,
  handled_at  DATETIME(3)  NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_refunds_order (order_id),
  CONSTRAINT fk_refunds_order FOREIGN KEY (order_id) REFERENCES orders (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE invites (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  inviter_id   BIGINT UNSIGNED NOT NULL,
  invitee_id   BIGINT UNSIGNED NOT NULL,
  registered_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  activated_at DATETIME(3)  NULL COMMENT '被邀请人导入第一份资料的时间，此时发奖励',
  inviter_days SMALLINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '邀请人实际得到的天数（受 70 天上限）',
  PRIMARY KEY (id),
  UNIQUE KEY uk_invites_invitee (invitee_id),
  KEY idx_invites_inviter (inviter_id),
  CONSTRAINT fk_invites_inviter FOREIGN KEY (inviter_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_invites_invitee FOREIGN KEY (invitee_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='邀请研友（6.8）';

CREATE TABLE survey_responses (
  owner_user_id  BIGINT UNSIGNED NOT NULL,
  exam_year      SMALLINT UNSIGNED NOT NULL,
  scores         JSON         NOT NULL COMMENT '每门课实际成绩与考前预估',
  retest_result  ENUM('in','out','unknown') NOT NULL,
  admission      ENUM('admitted','adjusted','rejected','pending') NULL,
  share_consent  TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '同意作为匿名上岸案例',
  membership_id  BIGINT UNSIGNED NULL COMMENT '赠送的 30 天会员',
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (owner_user_id),
  CONSTRAINT fk_survey_responses_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='考后回访（6.14），每人一次';
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (6, TRUE);

-- ===== 迁移 00007：00007_ops.sql =====
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
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (7, TRUE);

-- ===== 迁移 00008：00008_seed_config.sql =====
INSERT INTO rule_params (param_key, value, description) VALUES
('mastery', JSON_OBJECT(
  'self_assess', JSON_OBJECT('unknown', 0, 'vague', 30, 'mastered', 50),
  'objective_correct', JSON_OBJECT('easy', 10, 'medium', 15, 'hard', 20),
  'objective_wrong', -15,
  'subjective_slope', 25, 'subjective_offset', -10,
  'reveal_answer', -10,
  'recite', JSON_OBJECT('remembered', 8, 'vague', 2, 'forgot', -8),
  'overdue_daily', -3,
  'secondary_kp_factor', 0.5
), 'PRD 11.1 掌握分 M 的变化'),
('mastery_state', JSON_OBJECT(
  'mastered_min_m', 80, 'mastered_correct_days', 2, 'mastered_window_days', 30,
  'subjective_correct_rate', 0.8, 'weak_section_avg', 40,
  'false_mastery_window_days', 7, 'false_mastery_min_attempts', 2, 'false_mastery_max_rate', 0.5
), 'PRD 11.2 掌握状态、以为会了、短板'),
('review_interval', JSON_OBJECT(
  'reset_days', 1, 'vague_days', 2, 'steps', JSON_ARRAY(3, 7, 15, 30),
  'subjective_low_rate', 0.5, 'subjective_high_rate', 0.8
), 'PRD 11.3 复习间隔'),
('stage', JSON_OBJECT(
  'foundation_min_days', 150, 'strengthen_min_days', 60, 'sprint_min_days', 14,
  'early_sprint_max_days', 90, 'early_sprint_coverage', 0.7,
  'sprint_low_coverage', 0.6, 'sprint_new_kp_share', 0.1
), 'PRD 11.4 备考阶段（D16：≥150 基础、60–149 强化、14–59 冲刺、<14 考前）'),
('plan_mix', JSON_OBJECT(
  'foundation', JSON_OBJECT('new', 0.30, 'review', 0.30, 'weak', 0.20, 'recite', 0.20),
  'strengthen', JSON_OBJECT('new', 0.10, 'review', 0.25, 'weak', 0.45, 'recite', 0.20),
  'sprint',     JSON_OBJECT('new', 0.00, 'review', 0.30, 'weak', 0.50, 'recite', 0.20),
  'final',      JSON_OBJECT('new', 0.00, 'review', 0.35, 'weak', 0.20, 'recite', 0.45)
), 'PRD 11.5 今日计划构成'),
('plan', JSON_OBJECT(
  'minutes', JSON_OBJECT('objective', 1, 'term', 3, 'short_answer', 6, 'discussion', 12, 'recite', 1),
  'kp_exam_bonus', 0.5,
  'type_drill_per_week', 3, 'sprint_papers_per_week', 1, 'final_mock_every_days', 3, 'final_no_new_paper_days', 3
), 'PRD 11.5 单题用时与提分收益'),
('score_estimate', JSON_OBJECT(
  'measured_weight', 0.6, 'model_weight', 0.4,
  'measured_recent_papers', 2, 'qtype_recent_questions', 20, 'qtype_min_questions', 5,
  'width', JSON_OBJECT('1', 0.10, '2', 0.06, '3', 0.04)
), 'PRD 11.6 预估分'),
('loss_diagnosis', JSON_OBJECT('knowledge_max_m', 60, 'time_min_ratio', 0.3, 'tail_start_ratio', 0.67), 'PRD 11.7 失分诊断；tail_start_ratio 为「试卷后段」起点（按题序）'),
('wrong_book', JSON_OBJECT('remove_after_correct_days', 2, 'subjective_correct_rate', 0.8), 'PRD 11.8 错题本'),
('paper_time', JSON_OBJECT(
  'default_total_minutes', 180, 'check_minutes', 15,
  'per_question_minutes', JSON_OBJECT('term', 4, 'short_answer', 15, 'discussion', 40, 'objective', 2, 'essay', 60),
  'round_to_minutes', 5, 'overtime_ratio', 0.2, 'remind_left_minutes', 15, 'resume_window_minutes', 10
), 'PRD 11.9 整卷与模拟考试的时间'),
('ai_paper', JSON_OBJECT('exam_kp_share_min', 0.5, 'exam_kp_share_max', 0.7, 'targeted_weak_share', 0.5, 'weak_max_m', 60, 'targeted_avoid_days', 30), 'PRD 11.10 AI 组卷'),
('exam_profile', JSON_OBJECT('min_papers', 2, 'structure_recent_years', 3, 'high_freq_min_count', 2, 'missing_min_share', 0.1, 'missing_kp_ratio', 0.5, 'missing_max_mastery', 20), 'PRD 11.11 考情分析统计'),
('import_limits', JSON_OBJECT(
  'max_files', 10, 'max_pages_per_file', 200, 'max_mb_per_file', 50, 'max_images', 30, 'max_paste_chars', 20000,
  'chars_per_page', 1500, 'rows_per_page', 50
), 'PRD 11.12 导入规则'),
('answer_words', JSON_OBJECT('term', JSON_ARRAY(80, 150), 'short_answer', JSON_ARRAY(300, 500), 'discussion', JSON_ARRAY(800, NULL)), 'PRD 4.4 主观题建议字数'),
('essay', JSON_OBJECT('weekly_goal_default', 2, 'estimate_recent_essays', 3, 'repeat_tolerance', 6), 'PRD 11.13 作文评分'),
('grading', JSON_OBJECT('repeat_tolerance', 1, 'ai_question_offline_reports', 3), 'PRD 11.14、4.3'),
('quota', JSON_OBJECT(
  'free', JSON_OBJECT('subjects', 3, 'parse_pages_total', 100, 'import_questions_total', 500, 'grading_daily', 3, 'ai_questions_daily', 20, 'paper_grading_weekly', 1, 'essay_grading_weekly', 1),
  'member', JSON_OBJECT('subjects', 4, 'parse_pages_monthly', 1000, 'import_questions_total', NULL, 'grading_daily', NULL, 'ai_questions_daily', NULL, 'paper_grading_weekly', NULL, 'essay_grading_weekly', NULL)
), 'PRD 13.1 免费版与会员额度；NULL 表示不限（D12：免费版最多 3 门专业课）'),
('pricing', JSON_OBJECT(
  'sprint', JSON_OBJECT('name', '冲刺卡', 'cents', 5900, 'until', 'current_exam_end'),
  'season', JSON_OBJECT('name', '考季卡', 'cents', 16900, 'until', 'next_exam_end', 'recommended', TRUE),
  'monthly', JSON_OBJECT('name', '月卡', 'cents', 2990, 'days', 30)
), 'PRD 13.2 会员档位（测试价）'),
('growth', JSON_OBJECT('invite_days', 7, 'invite_max_days', 70, 'survey_days', 30), 'PRD 13.4 邀请与回访奖励'),
('account', JSON_OBJECT(
  'sms_code_ttl_seconds', 300, 'sms_resend_seconds', 60, 'sms_daily_limit', 10, 'sms_max_attempts', 5,
  'deletion_cooling_days', 7, 'access_token_minutes', 30, 'refresh_token_days', 60
), 'PRD 6 模块 0 验证码与账号'),
('retention', JSON_OBJECT('messages_days', 30, 'content_access_hours', 72, 'audit_log_days', 180, 'export_hours', 24), 'PRD 7.2.3、10.1、6.4 保留期限');

INSERT INTO feature_flags (flag_key, description, enabled_for_all) VALUES
('online_payment', '在线支付（6.5 购买、6.6、7.3 订单）', 0),
('official_bank',  '官方题库（3.1 添加入口、7.10–7.14）', 0),
('oral_recite',    '口述背诵（4.16）', 0),
('voice_answer',   '语音作答（4.4、4.20 语音转文字入口，D15）', 0),
('scanned_pdf',    '扫描版 PDF 导入（1.5）', 0),
('invite',         '邀请研友（6.8）', 0);

INSERT INTO exam_dates (exam_year, label, first_exam_start, first_exam_end, subject_exam_date) VALUES
(2027, '2027 研考', '2026-12-19', '2026-12-20', '2026-12-20');

INSERT INTO essay_rubrics (owner_user_id, subject_id, source, name, full_score, dimensions, is_active, origin) VALUES
(NULL, NULL, 'generic', '通用五维度标准', 150, JSON_ARRAY(
  JSON_OBJECT('name', '立意', 'description', '切题、观点明确、立意深刻', 'score', 30),
  JSON_OBJECT('name', '结构', 'description', '层次清晰、首尾呼应、过渡自然', 'score', 30),
  JSON_OBJECT('name', '内容与论证', 'description', '论据充实、论证有力', 'score', 30),
  JSON_OBJECT('name', '语言表达', 'description', '语言通顺、准确、得体', 'score', 30),
  JSON_OBJECT('name', '文采与亮点', 'description', '修辞、引用与独到见解', 'score', 30)
), 0, 'official');
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (8, TRUE);

-- ===== 迁移 00009：00009_import_answers.sql =====
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
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (9, TRUE);

-- ===== 迁移 00010：00010_essay_import.sql =====
-- 作文真题的要求字数（T11 识别，5.2 显示「字数 / 要求字数」）。
ALTER TABLE questions ADD COLUMN required_words SMALLINT UNSIGNED NULL COMMENT '作文要求字数' AFTER score;
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (10, TRUE);

-- ===== 迁移 00011：00011_exam_profile.sql =====
-- 考情分析的出题风格标签（AI 统计，3.8）按真题内容缓存；知识关联（3.9）第一次打开图谱时生成一次，之后由用户调整。
ALTER TABLE banks
  ADD COLUMN exam_style      JSON        NULL COMMENT '出题风格标签（AI 统计）' AFTER subject_code,
  ADD COLUMN exam_style_key  CHAR(64)    NULL COMMENT '生成标签时真题内容的哈希，真题变了重新生成' AFTER exam_style,
  ADD COLUMN relations_generated_at DATETIME(3) NULL COMMENT '知识关联已生成；用户删光后不再自动生成' AFTER exam_style_key;
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (11, TRUE);

-- ===== 迁移 00012：00012_recite.sql =====
ALTER TABLE recite_records
  ADD COLUMN idempotency_key VARCHAR(64) NULL AFTER coverage,
  ADD UNIQUE KEY uk_recite_records_idem (owner_user_id, idempotency_key);
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (12, TRUE);

-- ===== 迁移 00013：00013_paper_session.sql =====
ALTER TABLE paper_sessions
  ADD COLUMN paused_at DATETIME(3) NULL AFTER paused_seconds,
  ADD COLUMN quota_period VARCHAR(16) NULL AFTER counts_for_estimate;
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (13, TRUE);

-- ===== 迁移 00014：00014_essay_grading.sql =====
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
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (14, TRUE);

-- ===== 迁移 00015：00015_payment.sql =====
-- T25 会员与支付：App Store 内购用 appAccountToken 把交易绑到订单；退款单号；会员档位补内购商品（PRD 13.2，后台可改）。
ALTER TABLE orders
  ADD COLUMN app_account_token CHAR(36) NULL COMMENT 'App Store 内购：购买时传给 StoreKit 的 appAccountToken，校验交易属于这个订单' AFTER transaction_id,
  ADD COLUMN idempotency_key VARCHAR(64) NULL AFTER app_account_token,
  ADD UNIQUE KEY uk_orders_idem (owner_user_id, idempotency_key);

ALTER TABLE refunds
  ADD COLUMN refund_no VARCHAR(32) NULL AFTER order_id,
  ADD COLUMN amount_cents INT UNSIGNED NULL AFTER refund_no,
  ADD UNIQUE KEY uk_refunds_no (refund_no);

-- 会员档位沿用 00008 的 pricing（后台 7.8 改价格），补上 App Store 内购商品 ID。
UPDATE rule_params SET value = JSON_SET(value,
  '$.sprint.apple_product_id', 'cn.dreamelab.training.sprint',
  '$.season.apple_product_id', 'cn.dreamelab.training.season',
  '$.monthly.apple_product_id', 'cn.dreamelab.training.monthly'),
  description = 'PRD 13.2 会员档位（测试价，单位分）与 App Store 内购商品 ID'
WHERE param_key = 'pricing';
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (15, TRUE);

-- ===== 迁移 00016：00016_admin_ops.sql =====
-- T28 管理后台：运营与质量。后台「加解析额度」记为本周期的赠送额度（上限 + bonus）；统计与列表需要的索引。
ALTER TABLE quota_counters
  ADD COLUMN bonus INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '后台赠送的额度（7.2、7.5 补偿），上限 = 规则上限 + bonus' AFTER reserved;

ALTER TABLE users ADD KEY idx_users_created (created_at), ADD KEY idx_users_active (last_active_at);
ALTER TABLE orders ADD KEY idx_orders_paid (paid_at);
ALTER TABLE attempts ADD KEY idx_attempts_answered (answered_at);
ALTER TABLE import_jobs ADD KEY idx_import_jobs_status (status, created_at);
ALTER TABLE admin_users ADD COLUMN must_change_password TINYINT(1) NOT NULL DEFAULT 0 AFTER status;
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (16, TRUE);

-- ===== 迁移 00017：00017_admin_config.sql =====
-- T29 管理后台：配置与系统。AI 成本预算（7.8 每活跃用户每天）、系统消息开关与时间（7.9）。
INSERT INTO rule_params (param_key, value, description) VALUES
('ai_budget', JSON_OBJECT('daily_cost_per_active_user_yuan', 0.8, 'revenue_ratio_max', 0.3), '7.8 AI 成本预算：每活跃用户每天、占收入比例上限'),
('system_messages', JSON_OBJECT('review_due_enabled', TRUE, 'review_due_hour', 7, 'task_done_enabled', TRUE), '7.9 系统自动消息：复习到期（开关与北京时间整点）、解析和批改完成');
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (17, TRUE);

-- ===== 迁移 00018：00018_official.sql =====
-- T30 官方题库（PRD 10.2 7.10–7.14、11.15）。
-- 用户添加官方题库时，官方内容复制到用户自己的题库（origin / source = official，official_kp_id / official_question_id 指回官方条目），
-- 刷题、批改、复习与数据隔离都不用改。official_hash 是上次同步时官方内容的哈希：用户的副本内容和它一致说明用户没改过，
-- 新版本才覆盖；改过的不覆盖，提示「官方内容已更新」。
ALTER TABLE knowledge_points
  ADD COLUMN official_hash CHAR(64) NULL COMMENT '用户副本：上次同步时官方内容的哈希' AFTER official_kp_id,
  ADD COLUMN official_new_until DATETIME(3) NULL COMMENT '新版本新增的知识点在此之前标「新」' AFTER official_hash,
  ADD KEY idx_kps_official (owner_user_id, official_kp_id);

ALTER TABLE questions
  ADD COLUMN official_hash CHAR(64) NULL COMMENT '用户副本：上次同步时官方内容的哈希' AFTER official_question_id,
  ADD KEY idx_questions_official (owner_user_id, official_question_id);

ALTER TABLE official_versions
  ADD COLUMN snapshot JSON NULL COMMENT '回滚用：发布前被改动条目的原内容' AFTER changelog;

ALTER TABLE official_drafts
  ADD COLUMN submitted_at DATETIME(3) NULL AFTER status,
  ADD COLUMN reject_reason VARCHAR(255) NULL AFTER submitted_at;

ALTER TABLE review_tasks ADD COLUMN edited_payload JSON NULL AFTER reason;

CREATE TABLE official_demand_marks (
  subject_code VARCHAR(16)  NOT NULL,
  status       ENUM('unplanned') NOT NULL,
  marked_by    BIGINT UNSIGNED NULL,
  marked_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (subject_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='7.10 标为未规划的专业课';
INSERT INTO `goose_db_version` (version_id, is_applied) VALUES (18, TRUE);

