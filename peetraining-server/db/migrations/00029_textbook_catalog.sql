-- +goose Up
-- T38：共享教材身份、出版版本和校对修订；不迁移/发布旧项目素材。
CREATE TABLE textbooks (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 title VARCHAR(128) NOT NULL,
 author VARCHAR(128) NOT NULL,
 kind VARCHAR(16) NOT NULL,
 discipline VARCHAR(128) NOT NULL,
 aliases JSON NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 created_by BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_textbook_request (created_by, request_key),
 KEY idx_textbook_title (title),
 KEY idx_textbook_discipline (discipline, kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE textbook_editions (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 book_id BIGINT UNSIGNED NOT NULL,
 edition VARCHAR(64) NOT NULL,
 publisher VARCHAR(128) NOT NULL DEFAULT '',
 publication_year SMALLINT NULL,
 isbn VARCHAR(32) NOT NULL DEFAULT '',
 license_note VARCHAR(500) NOT NULL,
 authorized TINYINT(1) NOT NULL DEFAULT 0,
 scope_note VARCHAR(255) NOT NULL DEFAULT '',
 status VARCHAR(16) NOT NULL DEFAULT 'unpublished',
 current_revision_id BIGINT UNSIGNED NULL,
 created_by BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 request_hash CHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_textbook_edition (book_id, edition),
 UNIQUE KEY uk_edition_request (book_id, created_by, request_key),
 CONSTRAINT fk_textbook_edition_book FOREIGN KEY (book_id) REFERENCES textbooks(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE textbook_revisions (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 edition_id BIGINT UNSIGNED NOT NULL,
 revision_no BIGINT UNSIGNED NOT NULL,
 version BIGINT UNSIGNED NOT NULL DEFAULT 1,
 status VARCHAR(16) NOT NULL DEFAULT 'draft',
 sections JSON NOT NULL,
 content_hash CHAR(64) NOT NULL,
 index_status VARCHAR(16) NOT NULL DEFAULT 'missing',
 reviewed_by BIGINT UNSIGNED NULL,
 reviewed_at DATETIME(3) NULL,
 created_by BIGINT UNSIGNED NOT NULL,
 request_key VARCHAR(128) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 UNIQUE KEY uk_textbook_revision_no (edition_id, revision_no),
 UNIQUE KEY uk_revision_request (edition_id, created_by, request_key),
 CONSTRAINT fk_textbook_revision_edition FOREIGN KEY (edition_id) REFERENCES textbook_editions(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE textbook_revisions;
DROP TABLE textbook_editions;
DROP TABLE textbooks;
