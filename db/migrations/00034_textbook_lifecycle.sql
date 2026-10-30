-- +goose Up
-- 删除用户/题目时清理私有候选，用户快照随注销清理；历史批改仍引用原快照。
ALTER TABLE answer_evidence_snapshots ADD CONSTRAINT fk_answer_snapshot_owner FOREIGN KEY(owner_user_id) REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE answer_update_candidates
 ADD CONSTRAINT fk_answer_candidate_owner FOREIGN KEY(owner_user_id) REFERENCES users(id) ON DELETE CASCADE,
 ADD CONSTRAINT fk_answer_candidate_question FOREIGN KEY(question_id) REFERENCES questions(id) ON DELETE CASCADE;
ALTER TABLE official_project_textbooks
 ADD CONSTRAINT fk_project_textbook_project FOREIGN KEY(project_id) REFERENCES official_projects(id) ON DELETE CASCADE,
 ADD CONSTRAINT fk_project_textbook_edition FOREIGN KEY(edition_id) REFERENCES textbook_editions(id) ON DELETE RESTRICT;
ALTER TABLE official_project_textbook_writes ADD CONSTRAINT fk_project_textbook_write FOREIGN KEY(project_id) REFERENCES official_projects(id) ON DELETE CASCADE;
ALTER TABLE official_answer_candidate_evidence
 ADD CONSTRAINT fk_official_evidence_project FOREIGN KEY(project_id) REFERENCES official_projects(id) ON DELETE CASCADE,
 ADD CONSTRAINT fk_official_evidence_snapshot FOREIGN KEY(snapshot_id) REFERENCES answer_evidence_snapshots(id) ON DELETE CASCADE;
ALTER TABLE legacy_textbook_migrations
 ADD COLUMN source_hash CHAR(64) NOT NULL,
 ADD COLUMN manifest JSON NOT NULL,
 ADD COLUMN source_project_id BIGINT UNSIGNED NOT NULL,
 ADD COLUMN source_title VARCHAR(128) NOT NULL;
ALTER TABLE official_projects ADD COLUMN textbook_link_version BIGINT UNSIGNED NOT NULL DEFAULT 1;
-- +goose Down
ALTER TABLE official_projects DROP COLUMN textbook_link_version;
ALTER TABLE legacy_textbook_migrations DROP COLUMN source_title, DROP COLUMN source_project_id, DROP COLUMN manifest, DROP COLUMN source_hash;
ALTER TABLE official_answer_candidate_evidence DROP FOREIGN KEY fk_official_evidence_snapshot, DROP FOREIGN KEY fk_official_evidence_project;
ALTER TABLE official_project_textbook_writes DROP FOREIGN KEY fk_project_textbook_write;
ALTER TABLE official_project_textbooks DROP FOREIGN KEY fk_project_textbook_edition, DROP FOREIGN KEY fk_project_textbook_project;
ALTER TABLE answer_update_candidates DROP FOREIGN KEY fk_answer_candidate_question, DROP FOREIGN KEY fk_answer_candidate_owner;
ALTER TABLE answer_evidence_snapshots DROP FOREIGN KEY fk_answer_snapshot_owner;
