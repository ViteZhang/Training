-- T48 题库「官方 / 我的」与补充资料提示（PRD v3 增补 3.1e、「补充资料提示」）。
-- 批改异议增加原因「和我们老师讲的不一样」（teacher_differs），作为补充资料提示的触发之一。

-- +goose Up
ALTER TABLE disputes
  MODIFY COLUMN reason ENUM('hit_missed','rubric_wrong','score_unfair','other','teacher_differs') NOT NULL COMMENT '4.8；teacher_differs 和我们老师讲的不一样（PRD v3 增补）';

CREATE TABLE supplement_prompt_dismissals (
  owner_user_id BIGINT UNSIGNED NOT NULL,
  subject_id    BIGINT UNSIGNED NOT NULL,
  prompt_key    VARCHAR(48)  NOT NULL COMMENT 'section:<板块 id> / dispute / wrong_book',
  snooze_until  DATETIME(3)  NOT NULL COMMENT '这之前不再提示',
  PRIMARY KEY (owner_user_id, subject_id, prompt_key),
  CONSTRAINT fk_supplement_dismissals_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_supplement_dismissals_subject FOREIGN KEY (subject_id) REFERENCES subjects (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='3.1e 补充资料提示「以后再说」';

-- +goose Down
DROP TABLE supplement_prompt_dismissals;
UPDATE disputes SET reason = 'other' WHERE reason = 'teacher_differs';
ALTER TABLE disputes MODIFY COLUMN reason ENUM('hit_missed','rubric_wrong','score_unfair','other') NOT NULL COMMENT '4.8 四选一';
