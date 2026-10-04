-- 题库、资料、导入、知识点、题目、试卷（dev-spec 第五节）。
-- 一套题库模型、两种来源：自建题库 source=user、owner_user_id=用户；官方题库 source=official、owner_user_id 为空。
-- AI 产出的内容都有 origin 字段：user_confirmed 用户确认 / ai_extracted AI 提取待确认 / ai_generated AI 生成 / imported 导入原文 / official 官方。

-- +goose Up
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

-- +goose Down
DROP TABLE paper_questions;
DROP TABLE papers;
DROP TABLE question_reports;
DROP TABLE question_kps;
DROP TABLE rubric_points;
DROP TABLE questions;
DROP TABLE kp_relations;
DROP TABLE kp_sources;
DROP TABLE knowledge_points;
DROP TABLE import_items;
DROP TABLE import_job_materials;
DROP TABLE import_jobs;
DROP TABLE material_pages;
DROP TABLE materials;
DROP TABLE banks;
