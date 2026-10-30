-- +goose Up
ALTER TABLE paper_handwriting ADD COLUMN input_kind VARCHAR(8) NOT NULL DEFAULT 'photo';

CREATE TABLE voice_transcriptions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  practice_session_id BIGINT UNSIGNED NOT NULL,
  question_id BIGINT UNSIGNED NOT NULL,
  idempotency_key VARCHAR(128) NOT NULL,
  object_key VARCHAR(512) NOT NULL,
  capture_order BIGINT NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  recognized_text MEDIUMTEXT NOT NULL,
  error_message VARCHAR(255) NOT NULL DEFAULT '',
  UNIQUE KEY uk_voice_key (owner_user_id, practice_session_id, question_id, idempotency_key),
  KEY idx_voice_session (practice_session_id, question_id),
  CONSTRAINT fk_voice_owner FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT fk_voice_session FOREIGN KEY (practice_session_id) REFERENCES practice_sessions(id) ON DELETE CASCADE,
  CONSTRAINT fk_voice_question FOREIGN KEY (question_id) REFERENCES questions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='练习按题持久保存语音转写任务';

-- +goose Down
DROP TABLE voice_transcriptions;
ALTER TABLE paper_handwriting DROP COLUMN input_kind;
