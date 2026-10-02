-- +goose Up
-- 作文真题的要求字数（T11 识别，5.2 显示「字数 / 要求字数」）。
ALTER TABLE questions ADD COLUMN required_words SMALLINT UNSIGNED NULL COMMENT '作文要求字数' AFTER score;

-- +goose Down
ALTER TABLE questions DROP COLUMN required_words;
