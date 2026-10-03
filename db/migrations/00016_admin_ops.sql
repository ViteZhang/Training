-- +goose Up
-- T28 管理后台：运营与质量。后台「加解析额度」记为本周期的赠送额度（上限 + bonus）；统计与列表需要的索引。
ALTER TABLE quota_counters
  ADD COLUMN bonus INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '后台赠送的额度（7.2、7.5 补偿），上限 = 规则上限 + bonus' AFTER reserved;

ALTER TABLE users ADD KEY idx_users_created (created_at), ADD KEY idx_users_active (last_active_at);
ALTER TABLE orders ADD KEY idx_orders_paid (paid_at);
ALTER TABLE attempts ADD KEY idx_attempts_answered (answered_at);
ALTER TABLE import_jobs ADD KEY idx_import_jobs_status (status, created_at);
ALTER TABLE admin_users ADD COLUMN must_change_password TINYINT(1) NOT NULL DEFAULT 0 AFTER status;

-- +goose Down
ALTER TABLE admin_users DROP COLUMN must_change_password;
ALTER TABLE import_jobs DROP INDEX idx_import_jobs_status;
ALTER TABLE attempts DROP INDEX idx_attempts_answered;
ALTER TABLE orders DROP INDEX idx_orders_paid;
ALTER TABLE users DROP INDEX idx_users_active, DROP INDEX idx_users_created;
ALTER TABLE quota_counters DROP COLUMN bonus;
