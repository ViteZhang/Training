-- 整卷（T21）：练习模式的暂停开始时间、预占整卷批改次数时的额度周期（交卷结算、放弃退回都要用）。

-- +goose Up
ALTER TABLE paper_sessions
  ADD COLUMN paused_at DATETIME(3) NULL AFTER paused_seconds,
  ADD COLUMN quota_period VARCHAR(16) NULL AFTER counts_for_estimate;

-- +goose Down
ALTER TABLE paper_sessions
  DROP COLUMN quota_period,
  DROP COLUMN paused_at;
