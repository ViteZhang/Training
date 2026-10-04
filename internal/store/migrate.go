package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"

	"peetraining-server/db"
)

func newProvider(sqlDB *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectMySQL, sqlDB, migrations)
}

// MigrateUp 执行全部未执行的迁移。重复执行没有副作用。
func MigrateUp(ctx context.Context, sqlDB *sql.DB, log *slog.Logger) error {
	p, err := newProvider(sqlDB)
	if err != nil {
		return fmt.Errorf("加载迁移：%w", err)
	}
	results, err := p.Up(ctx)
	if err != nil {
		return fmt.Errorf("执行迁移：%w", err)
	}
	for _, r := range results {
		log.Info("migration applied", "migration", r.Source.Version, "file", r.Source.Path, "duration_ms", r.Duration.Milliseconds())
	}
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return err
	}
	log.Info("migrations up to date", "db_version", v, "applied_now", len(results))
	return nil
}

// MigrateStatus 输出每个迁移的执行状态。
func MigrateStatus(ctx context.Context, sqlDB *sql.DB, log *slog.Logger) error {
	p, err := newProvider(sqlDB)
	if err != nil {
		return fmt.Errorf("加载迁移：%w", err)
	}
	statuses, err := p.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range statuses {
		log.Info("migration status", "migration", s.Source.Version, "file", s.Source.Path, "state", string(s.State))
	}
	return nil
}

// MigrateDownTo 回滚到指定版本。只用于测试与本地排查；生产不回滚迁移，回滚靠新迁移。
func MigrateDownTo(ctx context.Context, sqlDB *sql.DB, version int64) error {
	p, err := newProvider(sqlDB)
	if err != nil {
		return err
	}
	_, err = p.DownTo(ctx, version)
	return err
}
