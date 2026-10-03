-- +goose Up
-- T29 管理后台：配置与系统。AI 成本预算（7.8 每活跃用户每天）、系统消息开关与时间（7.9）。
INSERT INTO rule_params (param_key, value, description) VALUES
('ai_budget', JSON_OBJECT('daily_cost_per_active_user_yuan', 0.8, 'revenue_ratio_max', 0.3), '7.8 AI 成本预算：每活跃用户每天、占收入比例上限'),
('system_messages', JSON_OBJECT('review_due_enabled', TRUE, 'review_due_hour', 7, 'task_done_enabled', TRUE), '7.9 系统自动消息：复习到期（开关与北京时间整点）、解析和批改完成');

-- +goose Down
DELETE FROM rule_params WHERE param_key IN ('ai_budget', 'system_messages');
