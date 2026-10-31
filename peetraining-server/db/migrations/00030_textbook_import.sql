-- +goose Up
CREATE TABLE textbook_imports (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 revision_id BIGINT UNSIGNED NOT NULL,
 file_name VARCHAR(255) NOT NULL,
 format VARCHAR(16) NOT NULL,
 size_bytes BIGINT NOT NULL,
 sha256 CHAR(64) NOT NULL,
 status VARCHAR(16) NOT NULL DEFAULT 'uploading',
 page_count INT UNSIGNED NOT NULL DEFAULT 0,
 attempt INT UNSIGNED NOT NULL DEFAULT 0,
 failure_message VARCHAR(255) NOT NULL DEFAULT '',
 sealed BOOLEAN NOT NULL DEFAULT FALSE,
 retry_key VARCHAR(128) NOT NULL DEFAULT '',
 applied_mode VARCHAR(16) NOT NULL DEFAULT '',
 created_by BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_textbook_import_request (revision_id, created_by, request_key),
 UNIQUE KEY uk_textbook_import_file (revision_id, sha256),
 KEY idx_textbook_import_recover (status, updated_at),
 CONSTRAINT fk_textbook_import_revision FOREIGN KEY (revision_id) REFERENCES textbook_revisions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE textbook_import_pages (
 import_id BIGINT UNSIGNED NOT NULL,
 page_no INT UNSIGNED NOT NULL,
 payload JSON NOT NULL,
 uncertain BOOLEAN NOT NULL DEFAULT FALSE,
 model VARCHAR(128) NOT NULL,
 prompt_version VARCHAR(64) NOT NULL,
 PRIMARY KEY (import_id, page_no),
 CONSTRAINT fk_textbook_page_import FOREIGN KEY (import_id) REFERENCES textbook_imports(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE textbook_daily_budget (
 budget_day DATE NOT NULL PRIMARY KEY,
 pdf_pages INT UNSIGNED NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
INSERT INTO rule_params (param_key,value,version,updated_at) VALUES ('textbook_import_limits',JSON_OBJECT('max_concurrent_imports',2,'max_file_mb',50,'max_pages',1000,'max_imports_per_revision',50,'max_daily_pdf_pages',2000),1,UTC_TIMESTAMP(3));
-- +goose Down
DELETE FROM rule_params WHERE param_key='textbook_import_limits';
DROP TABLE textbook_daily_budget;
DROP TABLE textbook_import_pages;
DROP TABLE textbook_imports;
