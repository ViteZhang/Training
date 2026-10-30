-- +goose Up
CREATE TABLE paper_handwriting (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  paper_session_id BIGINT UNSIGNED NOT NULL,
  seq SMALLINT UNSIGNED NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  capture_order BIGINT UNSIGNED NOT NULL DEFAULT 0,
  object_keys JSON NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  recognized_text MEDIUMTEXT NOT NULL,
  low_confidence JSON NOT NULL,
  error_message VARCHAR(255) NOT NULL DEFAULT '',
  UNIQUE KEY uk_paper_hw_key (owner_user_id, paper_session_id, seq, idempotency_key),
  KEY idx_paper_hw_session (paper_session_id, seq),
  CONSTRAINT fk_paper_hw_owner FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_paper_hw_item FOREIGN KEY (paper_session_id, seq) REFERENCES paper_session_items(paper_session_id, seq) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='整卷手写稿后台识别；交卷后保留原始照片与结果';

-- +goose Down
DROP TABLE paper_handwriting;
