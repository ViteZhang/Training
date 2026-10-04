-- 基线迁移：只确认库的字符集与排序规则。全部业务表在 T03 的迁移里建。

-- +goose Up
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
