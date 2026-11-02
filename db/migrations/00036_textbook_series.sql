-- +goose Up
-- 套书名：分上下卷等多册出版的教材填同一个套书名，用户输入套书名时整套一起匹配。空表示单册。
ALTER TABLE textbooks ADD COLUMN series VARCHAR(128) NOT NULL DEFAULT '' AFTER aliases;

-- +goose Down
ALTER TABLE textbooks DROP COLUMN series;
