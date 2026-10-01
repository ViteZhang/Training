// Package db 内嵌 goose 迁移文件，随二进制一起发布，生产上用 `api migrate up` 执行。
package db

import "embed"

// Migrations 是 db/migrations 下全部 SQL 迁移。已合并的迁移不再修改，改库只加新文件。
//
//go:embed migrations/*.sql
var Migrations embed.FS
