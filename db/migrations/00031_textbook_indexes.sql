-- +goose Up
-- T40：每个索引代次独立构建；发布只原子切换指针，不改旧正文或向量。
ALTER TABLE textbooks ADD COLUMN default_edition_id BIGINT UNSIGNED NULL;
ALTER TABLE textbook_editions
 ADD COLUMN version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 ADD COLUMN current_generation_id BIGINT UNSIGNED NULL,
 ADD COLUMN license_expires_at DATETIME(3) NULL,
 ADD COLUMN excerpts_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE textbook_revisions ADD COLUMN ready_generation_id BIGINT UNSIGNED NULL;
CREATE TABLE textbook_index_generations (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 book_id BIGINT UNSIGNED NOT NULL,
 edition_id BIGINT UNSIGNED NOT NULL,
 revision_id BIGINT UNSIGNED NOT NULL,
 source_version BIGINT UNSIGNED NOT NULL,
 content_hash CHAR(64) NOT NULL,
 profile JSON NOT NULL,
 profile_hash CHAR(64) NOT NULL,
 split_config JSON NOT NULL,
 collection_name VARCHAR(96) NOT NULL DEFAULT '',
 status VARCHAR(16) NOT NULL DEFAULT 'queued',
 chunk_count INT UNSIGNED NOT NULL DEFAULT 0,
 attempt INT UNSIGNED NOT NULL DEFAULT 0,
 retry_key VARCHAR(128) NOT NULL DEFAULT '',
 failure_message VARCHAR(255) NOT NULL DEFAULT '',
 created_by BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_textbook_index_request (revision_id,created_by,request_key),
 KEY idx_textbook_index_recover (status,updated_at),
 CONSTRAINT fk_textbook_index_revision FOREIGN KEY (revision_id) REFERENCES textbook_revisions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE textbook_chunks (
 generation_id BIGINT UNSIGNED NOT NULL,
 ordinal INT UNSIGNED NOT NULL,
 section_no INT UNSIGNED NOT NULL,
 start_rune INT UNSIGNED NOT NULL,
 end_rune INT UNSIGNED NOT NULL,
 heading VARCHAR(512) NOT NULL,
 body MEDIUMTEXT NOT NULL,
 text_hash CHAR(64) NOT NULL,
 vector_ready BOOLEAN NOT NULL DEFAULT FALSE,
 PRIMARY KEY (generation_id,ordinal),
 FULLTEXT KEY ft_textbook_chunk (body,heading) WITH PARSER ngram,
 CONSTRAINT fk_textbook_chunk_generation FOREIGN KEY (generation_id) REFERENCES textbook_index_generations(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE textbook_publication_events (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 book_id BIGINT UNSIGNED NOT NULL,
 edition_id BIGINT UNSIGNED NOT NULL,
 revision_id BIGINT UNSIGNED NULL,
 generation_id BIGINT UNSIGNED NULL,
 action VARCHAR(24) NOT NULL,
 summary VARCHAR(500) NOT NULL,
 created_by BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_textbook_event_request (book_id,created_by,request_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
INSERT INTO rule_params (param_key,value,version,updated_at) VALUES ('textbook_retrieval',JSON_OBJECT('version','v1','chunk_runes',700,'overlap_runes',100,'recall_k',20,'result_k',8,'context_runes',6000),1,UTC_TIMESTAMP(3));
INSERT INTO feature_flags (flag_key,enabled_for_all,description,updated_at) VALUES ('textbook_retrieval',FALSE,'官方教材检索（独立于官方题库）',UTC_TIMESTAMP(3));
-- +goose Down
DELETE FROM feature_flags WHERE flag_key='textbook_retrieval';
DELETE FROM rule_params WHERE param_key='textbook_retrieval';
DROP TABLE textbook_publication_events;
DROP TABLE textbook_chunks;
DROP TABLE textbook_index_generations;
ALTER TABLE textbook_revisions DROP COLUMN ready_generation_id;
ALTER TABLE textbook_editions DROP COLUMN excerpts_enabled, DROP COLUMN license_expires_at, DROP COLUMN current_generation_id, DROP COLUMN version;
ALTER TABLE textbooks DROP COLUMN default_edition_id;
