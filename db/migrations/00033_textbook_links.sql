-- +goose Up
ALTER TABLE subjects ADD COLUMN textbook_selections JSON NULL;
CREATE TABLE official_project_textbooks (
 project_id BIGINT UNSIGNED NOT NULL,
 edition_id BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(3) NOT NULL,
 PRIMARY KEY(project_id,edition_id),
 KEY idx_project_textbook_edition(edition_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE legacy_textbook_migrations (
 reference_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 book_id BIGINT UNSIGNED NOT NULL,
 edition_id BIGINT UNSIGNED NOT NULL,
 revision_id BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE official_answer_candidate_evidence (
 project_id BIGINT UNSIGNED NOT NULL,
 snapshot_id BIGINT UNSIGNED NOT NULL,
 content_hash CHAR(64) NOT NULL,
 PRIMARY KEY(project_id,snapshot_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE official_project_textbook_writes (
 project_id BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 PRIMARY KEY(project_id,request_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +goose Down
DROP TABLE official_project_textbook_writes;
DROP TABLE official_answer_candidate_evidence;
DROP TABLE legacy_textbook_migrations;
DROP TABLE official_project_textbooks;
ALTER TABLE subjects DROP COLUMN textbook_selections;
