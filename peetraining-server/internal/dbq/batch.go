package dbq

// 本文件是手写的（不是 sqlc 生成的）：sqlc 的 MySQL 引擎不支持一条 INSERT 写多行。
// 添加官方题库要给用户复制成百上千行，生产上应用机跨机房访问数据库，逐条写每行一次往返要几十秒；
// 一条 INSERT 写几百行只要一次往返。

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"peetraining-server/internal/dbtypes"
)

const (
	batchMaxRows  = 200
	batchMaxBytes = 4 << 20 // 远低于 MySQL 默认的 max_allowed_packet（64 MB）
)

// InsertRows 执行 head（如 "INSERT INTO t (a, b) VALUES"）加多组 (?, ?)；每行的参数个数必须相同。
// 按行数和大致字节数分批。after 不为空时每批执行完调用一次，传入这一批的行和第一行的自增 id
// （同一条 INSERT 的自增 id 按行的顺序递增，但不保证连续，要按 id 顺序回查）。
// 调用方负责每行都写 owner_user_id（用户数据隔离）。
func (q *Queries) InsertRows(ctx context.Context, head string, rows [][]any, after func(chunk [][]any, firstID uint64) error) error {
	for len(rows) > 0 {
		n, size := 0, 0
		for n < len(rows) && n < batchMaxRows {
			sz := rowSize(rows[n])
			if n > 0 && size+sz > batchMaxBytes {
				break
			}
			size += sz
			n++
		}
		chunk := rows[:n]
		rows = rows[n:]
		ph := "(" + strings.TrimSuffix(strings.Repeat("?,", len(chunk[0])), ",") + ")"
		var b strings.Builder
		b.WriteString(head)
		args := make([]any, 0, n*len(chunk[0]))
		for i, r := range chunk {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(ph)
			args = append(args, r...)
		}
		res, err := q.db.ExecContext(ctx, b.String(), args...)
		if err != nil {
			return err
		}
		if after != nil {
			id, err := res.LastInsertId()
			if err != nil {
				return err
			}
			if err := after(chunk, uint64(id)); err != nil {
				return err
			}
		}
	}
	return nil
}

func rowSize(r []any) int {
	n := 0
	for _, v := range r {
		switch x := v.(type) {
		case string:
			n += len(x)
		case []byte:
			n += len(x)
		case json.RawMessage:
			n += len(x)
		case dbtypes.NullJSON:
			n += len(x)
		case sql.NullString:
			n += len(x.String)
		default:
			n += 16
		}
	}
	return n + 8*len(r)
}
