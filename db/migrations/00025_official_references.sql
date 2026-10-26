-- +goose Up
-- PRD v3 7.11（10 月 26 日更新）：官方参考素材。后台录入有授权的教材、讲义原文，切分成段；
-- 7.12 编辑录题时 AI 生成官方题库的参考答案与采分点，优先引用这些段落并标出处（answer_generate 的 Context）。
-- 这是内容团队录入的授权资料，不是用户内容，不带 owner_user_id。
CREATE TABLE official_references (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  project_id   BIGINT UNSIGNED NOT NULL,
  title        VARCHAR(128) NOT NULL COMMENT '如：童庆炳《文学理论教程》第五版',
  kind         ENUM('textbook','handout') NOT NULL COMMENT '教材 / 讲义',
  license_note VARCHAR(255) NOT NULL COMMENT '授权说明（必须有授权才能录入）',
  segments     INT UNSIGNED NOT NULL DEFAULT 0,
  chars        INT UNSIGNED NOT NULL DEFAULT 0,
  created_by   BIGINT UNSIGNED NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_official_references_project (project_id),
  CONSTRAINT fk_official_references_project FOREIGN KEY (project_id) REFERENCES official_projects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='7.11 官方参考素材';

CREATE TABLE official_reference_segments (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  reference_id BIGINT UNSIGNED NOT NULL,
  seq          INT UNSIGNED NOT NULL,
  heading      VARCHAR(128) NOT NULL DEFAULT '' COMMENT '段落所在的章节标题，用作出处',
  text         TEXT NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_official_reference_segments (reference_id, seq),
  CONSTRAINT fk_official_reference_segments_ref FOREIGN KEY (reference_id) REFERENCES official_references (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='官方参考素材切分后的段落';

-- +goose Down
DROP TABLE official_reference_segments;
DROP TABLE official_references;
