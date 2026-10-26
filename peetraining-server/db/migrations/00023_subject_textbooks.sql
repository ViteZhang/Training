-- +goose Up
-- PRD v3 1.3、6.9、11.16（10 月 26 日更新）：每门专业课的参考教材（选填，可填多本）。
-- 资料里缺答案时，AI 生成参考答案会尽量贴近教材的表述；只存书名，不需要用户上传教材。
ALTER TABLE subjects ADD COLUMN textbooks JSON NULL COMMENT '参考教材书名列表，如 ["童庆炳《文学理论教程》"]' AFTER my_majors;

-- +goose Down
ALTER TABLE subjects DROP COLUMN textbooks;
