package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"peetraining-server/internal/dbq"
)

// WithTx 在一个事务里执行 fn；fn 返回错误或 panic 时回滚。
// 扣额度与写结果必须放在同一个事务里（CLAUDE.md 必须遵守第 7 条）。
func WithTx(ctx context.Context, db *sql.DB, fn func(q *dbq.Queries) error) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始事务：%w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, rbErr)
			}
		}
	}()
	if err = fn(dbq.New(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
