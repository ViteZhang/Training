-- T37：题目来源由用户确认；年份不再用于推断真题来源。

-- +goose Up
ALTER TABLE questions
  MODIFY COLUMN source ENUM('exam','exercise','unconfirmed','ai_generated','official') NOT NULL;

ALTER TABLE import_job_materials
  ADD COLUMN question_source ENUM('exam','exercise','unconfirmed') NULL AFTER owner_user_id;

-- 旧版导入只凭年份自动标成真题，无法证明用户确认过来源，保守迁为待确认。
UPDATE questions
SET source = 'unconfirmed'
WHERE owner_user_id IS NOT NULL AND source = 'exam' AND source_material_id IS NOT NULL;

-- 尚未确认入库的旧导入条目同样不能保留按年份自动推断的真题来源。
UPDATE import_items
SET payload = JSON_SET(payload, '$.source', 'unconfirmed')
WHERE item_type = 'question' AND created_entity_id IS NULL
  AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.source')) = 'exam';

-- +goose Down
-- 回滚时将待确认项转为习题，避免回滚后再次被当作真题。
UPDATE import_items
SET payload = JSON_SET(payload, '$.source', 'exercise')
WHERE item_type = 'question' AND created_entity_id IS NULL
  AND JSON_UNQUOTE(JSON_EXTRACT(payload, '$.source')) = 'unconfirmed';

UPDATE questions SET source = 'exercise' WHERE source = 'unconfirmed';

ALTER TABLE import_job_materials DROP COLUMN question_source;

ALTER TABLE questions
  MODIFY COLUMN source ENUM('exam','exercise','ai_generated','official') NOT NULL;
