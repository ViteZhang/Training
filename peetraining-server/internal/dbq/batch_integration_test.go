package dbq_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

// TestInsertRows：超过一批的行数分几条 INSERT 写完；每批回调拿到的第一个 id 能按插入顺序回查出这一批的 id。
func TestInsertRows(t *testing.T) {
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec("INSERT INTO users (phone, invite_code, nickname) VALUES ('13900001111', 'INVT1111', 'b')")
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	owner := sql.NullInt64{Int64: uid, Valid: true}
	q := dbq.New(db)
	at := time.Date(2026, 11, 11, 1, 2, 3, 456_000_000, time.UTC)
	const n = 450
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{owner, strings.Repeat("a", 63) + string(rune('0'+i%10)), []byte(`{"n":` + itoa(i) + `}`), at}
	}
	var ids []uint64
	batches := 0
	err = q.InsertRows(ctx, "INSERT INTO answer_evidence_snapshots (owner_user_id, answer_hash, payload, created_at) VALUES", rows, func(chunk [][]any, first uint64) error {
		batches++
		got, err := q.ListUserAnswerSnapshotsSince(ctx, dbq.ListUserAnswerSnapshotsSinceParams{OwnerUserID: owner, CreatedAt: at, FirstID: first})
		if err != nil {
			return err
		}
		if len(got) != len(chunk) {
			t.Fatalf("批 %d：写 %d 行，查到 %d", batches, len(chunk), len(got))
		}
		ids = append(ids, got...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if batches != 3 || len(ids) != n {
		t.Fatalf("批数 %d，id %d 个", batches, len(ids))
	}
	for i, id := range ids {
		var p string
		if err := db.QueryRow("SELECT payload FROM answer_evidence_snapshots WHERE id = ?", id).Scan(&p); err != nil {
			t.Fatal(err)
		}
		if p != `{"n": `+itoa(i)+`}` && p != `{"n":`+itoa(i)+`}` {
			t.Fatalf("第 %d 行对应的 id %d 内容是 %s", i, id, p)
		}
	}
	if err := q.InsertRows(ctx, "INSERT INTO answer_evidence_snapshots (owner_user_id, answer_hash, payload, created_at) VALUES", nil, nil); err != nil {
		t.Fatal("空行应直接返回", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}
