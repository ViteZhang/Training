package store_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"peetraining-server/internal/logx"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

// 迁移可以从空库执行到最新、整体回滚、再执行一遍，保证每个 Down 都写对了。
func TestMigrationsUpDownUp(t *testing.T) {
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := logx.New(io.Discard, slog.LevelInfo)

	if err := store.MigrateUp(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM feature_flags WHERE enabled_for_all = 0").Scan(&n); err != nil || n != 6 {
		t.Fatalf("应有 6 个默认关闭的功能开关，got %d %v", n, err)
	}
	if err := store.MigrateDownTo(ctx, db, 0); err != nil {
		t.Fatalf("整体回滚：%v", err)
	}
	if err := store.MigrateUp(ctx, db, log); err != nil {
		t.Fatalf("回滚后再执行：%v", err)
	}
}

// 删除用户时，其全部内容随外键级联删除（注销到期物理删除的基础，ADR 0006）；
// 删除题目时，作答与错题一并删除，整卷成绩保留（PRD 11.12）。
func TestSchemaCascades(t *testing.T) {
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.ExecContext(ctx, q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	uid := exec(`INSERT INTO users (phone, invite_code) VALUES ('13800000001', 'ABC12345')`)
	sid := exec(`INSERT INTO subjects (owner_user_id, name) VALUES (?, '中国语言文学基础')`, uid)
	bid := exec(`INSERT INTO banks (source, owner_user_id, subject_id) VALUES ('user', ?, ?)`, uid, sid)
	mid := exec(`INSERT INTO materials (owner_user_id, bank_id, file_name, format, sha256, right_confirmed_at) VALUES (?, ?, 'a.docx', 'docx', REPEAT('a', 64), NOW(3))`, uid, bid)
	kp := exec(`INSERT INTO knowledge_points (owner_user_id, bank_id, level, name, origin) VALUES (?, ?, 'point', '意境', 'ai_extracted')`, uid, bid)
	q := exec(`INSERT INTO questions (owner_user_id, bank_id, qtype, stem, source, source_material_id, content_hash) VALUES (?, ?, 'term', '意境', 'exam', ?, REPEAT('b', 64))`, uid, bid, mid)
	exec(`INSERT INTO question_kps (question_id, kp_id, is_primary, owner_user_id) VALUES (?, ?, 1, ?)`, q, kp, uid)
	pid := exec(`INSERT INTO papers (owner_user_id, bank_id, kind, title, full_score, actual_score, structure) VALUES (?, ?, 'real_exam', '2024 真题', 150, 150, JSON_ARRAY())`, uid, bid)
	ps := exec(`INSERT INTO paper_sessions (owner_user_id, paper_id, subject_id, paper_kind, paper_title, mode, status, full_score, score) VALUES (?, ?, ?, 'real_exam', '2024 真题', 'mock', 'graded', 150, 98)`, uid, pid, sid)
	a := exec(`INSERT INTO attempts (owner_user_id, question_id, paper_session_id, answer_mode, answer_text) VALUES (?, ?, ?, 'typed', '答案')`, uid, q, ps)
	exec(`INSERT INTO gradings (owner_user_id, attempt_id, kind) VALUES (?, ?, 'subjective')`, uid, a)
	exec(`INSERT INTO wrong_book (owner_user_id, question_id, added_reason) VALUES (?, ?, 'partial')`, uid, q)
	exec(`INSERT INTO kp_mastery (owner_user_id, kp_id, m) VALUES (?, ?, 40)`, uid, kp)

	// 删除题目：作答、批改、错题级联删除；整卷成绩保留。
	exec(`DELETE FROM questions WHERE id = ?`, q)
	for _, table := range []string{"attempts", "gradings", "wrong_book", "question_kps"} {
		if n := count(table); n != 0 {
			t.Errorf("删除题目后 %s 应为空，还有 %d 行", table, n)
		}
	}
	if count("paper_sessions") != 1 {
		t.Error("删除题目后整卷成绩应保留")
	}

	// 删除用户：全部内容级联删除。
	exec(`DELETE FROM users WHERE id = ?`, uid)
	for _, table := range []string{"subjects", "banks", "materials", "knowledge_points", "papers", "paper_sessions", "kp_mastery"} {
		if n := count(table); n != 0 {
			t.Errorf("删除用户后 %s 应为空，还有 %d 行", table, n)
		}
	}
}
