-- +goose Up
-- 考情分析的出题风格标签（AI 统计，3.8）按真题内容缓存；知识关联（3.9）第一次打开图谱时生成一次，之后由用户调整。
ALTER TABLE banks
  ADD COLUMN exam_style      JSON        NULL COMMENT '出题风格标签（AI 统计）' AFTER subject_code,
  ADD COLUMN exam_style_key  CHAR(64)    NULL COMMENT '生成标签时真题内容的哈希，真题变了重新生成' AFTER exam_style,
  ADD COLUMN relations_generated_at DATETIME(3) NULL COMMENT '知识关联已生成；用户删光后不再自动生成' AFTER exam_style_key;

-- +goose Down
ALTER TABLE banks DROP COLUMN relations_generated_at, DROP COLUMN exam_style_key, DROP COLUMN exam_style;
