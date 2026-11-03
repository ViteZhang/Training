-- T47 后台 AI 解析导入（PRD v3 增补 7.17）。
-- 解析的是团队上传的真题卷、参考书目录、有授权的讲义，与用户端资料（materials、import_jobs）完全分表存放，不使用任何用户上传的内容。
-- 核对通过的条目生成 7.12 草稿并提交 7.13 审核。

-- +goose Up
CREATE TABLE official_import_files (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id     BIGINT UNSIGNED NOT NULL,
  kind           ENUM('exam','catalog','handout','table') NOT NULL COMMENT '真题卷 / 参考书目录 / 有授权的讲义 / 表格导入',
  file_name      VARCHAR(255) NOT NULL,
  format         ENUM('pdf','docx','image','text','csv') NOT NULL,
  object_key     VARCHAR(255) NULL COMMENT 'OSS 私有桶对象键（official-imports/ 前缀，与用户资料分开）',
  size_bytes     BIGINT UNSIGNED NOT NULL DEFAULT 0,
  content        MEDIUMTEXT   NULL COMMENT '粘贴的文字或表格；文件解析后的全文（核对区中间栏的原文）',
  exam_year      SMALLINT UNSIGNED NULL COMMENT '真题卷年份（识别不出时用它）',
  is_recollection TINYINT(1)  NOT NULL DEFAULT 0 COMMENT '回忆版真题',
  status         ENUM('uploading','parsing','parsed','failed') NOT NULL DEFAULT 'uploading',
  fail_reason    VARCHAR(255) NULL,
  question_count INT UNSIGNED NOT NULL DEFAULT 0,
  kp_count       INT UNSIGNED NOT NULL DEFAULT 0,
  created_by     BIGINT UNSIGNED NULL,
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_official_import_files_project (project_id, id),
  CONSTRAINT fk_official_import_files_project FOREIGN KEY (project_id) REFERENCES official_projects (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='7.17 团队上传的资料';

CREATE TABLE official_import_items (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  file_id      BIGINT UNSIGNED NOT NULL,
  project_id   BIGINT UNSIGNED NOT NULL,
  item_type    ENUM('question','knowledge_point') NOT NULL,
  seq          INT UNSIGNED NOT NULL,
  payload      JSON         NOT NULL COMMENT '与 7.12 草稿同结构（QuestionPayload / KPPayload）',
  citations    JSON         NULL COMMENT '参考答案引用的官方参考素材',
  source_text  TEXT         NULL COMMENT '原文片段，核对区高亮',
  page         INT UNSIGNED NOT NULL DEFAULT 0,
  confidence   ENUM('high','medium','low') NOT NULL,
  status       ENUM('pending','accepted','rejected') NOT NULL DEFAULT 'pending',
  draft_id     BIGINT UNSIGNED NULL,
  reviewed_by  BIGINT UNSIGNED NULL,
  reviewed_at  DATETIME(3)  NULL,
  PRIMARY KEY (id),
  KEY idx_official_import_items_file (file_id, seq),
  CONSTRAINT fk_official_import_items_file FOREIGN KEY (file_id) REFERENCES official_import_files (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='7.17 解析结果，逐条接受 / 修改 / 驳回';

-- +goose Down
DROP TABLE official_import_items;
DROP TABLE official_import_files;
