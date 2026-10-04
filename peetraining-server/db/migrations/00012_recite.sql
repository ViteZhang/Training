-- 背诵（T20）：背诵记录的幂等键，重复提交不重复改掌握分与复习日。

-- +goose Up
ALTER TABLE recite_records
  ADD COLUMN idempotency_key VARCHAR(64) NULL AFTER coverage,
  ADD UNIQUE KEY uk_recite_records_idem (owner_user_id, idempotency_key);

-- +goose Down
ALTER TABLE recite_records
  DROP INDEX uk_recite_records_idem,
  DROP COLUMN idempotency_key;
