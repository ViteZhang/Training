-- T49 后台 7.17 解析后由编辑点「生成答案 / 提取采分点」（PRD v3 增补 7.17，11 月 4 日更新）。
-- 解析时默认不再自动补答案；上传时可以勾选「缺答案时由 AI 生成」。整份资料「一键补全」在 Worker 里排队执行，记进度。
-- 单题补全失败的原因记在条目上，核对区直接显示。

-- +goose Up
ALTER TABLE official_import_files
  ADD COLUMN auto_answer TINYINT(1) NOT NULL DEFAULT 0 COMMENT '解析时缺答案、采分点的题由 AI 补全' AFTER is_recollection,
  ADD COLUMN fill_status ENUM('idle','running') NOT NULL DEFAULT 'idle' COMMENT '一键补全答案的状态' AFTER kp_count,
  ADD COLUMN fill_total INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '这次一键补全要处理的题数' AFTER fill_status,
  ADD COLUMN fill_done INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '已处理的题数（含失败）' AFTER fill_total,
  ADD COLUMN fill_failed INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '补全失败的题数' AFTER fill_done;

ALTER TABLE official_import_items
  ADD COLUMN fill_error VARCHAR(255) NULL COMMENT '最近一次补答案、采分点失败的原因' AFTER confidence;

-- +goose Down
ALTER TABLE official_import_items DROP COLUMN fill_error;
ALTER TABLE official_import_files DROP COLUMN fill_failed, DROP COLUMN fill_done, DROP COLUMN fill_total, DROP COLUMN fill_status, DROP COLUMN auto_answer;
