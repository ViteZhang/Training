-- +goose Up
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

-- +goose Down
DROP TABLE official_demand_marks;
ALTER TABLE review_tasks DROP COLUMN edited_payload;
ALTER TABLE official_drafts DROP COLUMN reject_reason, DROP COLUMN submitted_at;
ALTER TABLE official_versions DROP COLUMN snapshot;
ALTER TABLE questions DROP INDEX idx_questions_official, DROP COLUMN official_hash;
ALTER TABLE knowledge_points DROP INDEX idx_kps_official, DROP COLUMN official_new_until, DROP COLUMN official_hash;
