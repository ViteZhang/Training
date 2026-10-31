-- +goose Up
-- 审计 action 保存 HTTP 方法与完整路由模板；教材导入等嵌套路由超过原来的 64 字符。
ALTER TABLE admin_audit_logs MODIFY COLUMN action VARCHAR(512) NOT NULL;

-- +goose Down
ALTER TABLE admin_audit_logs MODIFY COLUMN action VARCHAR(64) NOT NULL;
