// Package store 管理 MySQL、Redis 连接与数据库迁移。
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// OpenMySQL 打开连接池并确认可连通。
//
// 强制 parseTime、UTC 与 utf8mb4：数据库时间一律存 UTC（CLAUDE.md 必须遵守第 13 条），
// 业务日期在应用层按 Asia/Shanghai 换算。
func OpenMySQL(ctx context.Context, dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 MYSQL_DSN：%w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Collation = "utf8mb4_0900_ai_ci"
	// goose 的迁移文件里可能有多条语句。
	cfg.MultiStatements = true

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 MySQL 连接器：%w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接 MySQL：%w", err)
	}
	return db, nil
}

// SQLPinger 让 *sql.DB 满足健康检查的 Ping(ctx) 接口。
type SQLPinger struct{ DB *sql.DB }

func (p SQLPinger) Ping(ctx context.Context) error { return p.DB.PingContext(ctx) }
