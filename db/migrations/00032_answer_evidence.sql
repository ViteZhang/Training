-- +goose Up
-- T41：答案依据不可变快照；主动候选不覆盖当前题目。
CREATE TABLE answer_evidence_snapshots (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 owner_user_id BIGINT UNSIGNED NULL,
 answer_hash CHAR(64) NOT NULL,
 payload JSON NOT NULL,
 created_at DATETIME(3) NOT NULL,
 KEY idx_answer_snapshot_owner (owner_user_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
ALTER TABLE questions ADD COLUMN answer_snapshot_id BIGINT UNSIGNED NULL;
CREATE TABLE answer_update_candidates (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 owner_user_id BIGINT UNSIGNED NOT NULL,
 question_id BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 base_hash CHAR(64) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'queued',
 answer MEDIUMTEXT NULL,
 points JSON NULL,
 snapshot_id BIGINT UNSIGNED NULL,
 failure_message VARCHAR(255) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_answer_candidate_request (owner_user_id,question_id,request_key),
 KEY idx_answer_candidate_recover (status,updated_at),
 KEY idx_answer_candidate_question (owner_user_id,question_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +goose Down
DROP TABLE answer_update_candidates;
ALTER TABLE questions DROP COLUMN answer_snapshot_id;
DROP TABLE answer_evidence_snapshots;
