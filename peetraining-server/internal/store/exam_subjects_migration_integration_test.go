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

// T50 迁移 00043：同一院校同一代码按专业录的多行合并成一条考试科目，专业收进适用专业，
// 引用（用户专业课、待入库）改指向保留的那条；旧立项按院校 + 代码挂到考试科目上；统考科目已录入。
func TestExamSubjectsMigration(t *testing.T) {
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
	if err := store.MigrateDownTo(ctx, db, 42); err != nil {
		t.Fatal(err)
	}

	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := db.ExecContext(ctx, q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	// 四川大学 812 已有一行（专业：哲学），再按宗教学录一行同一套卷子；另录一门只有名称没有代码的课。
	var keep int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM catalog_courses WHERE school = '四川大学' AND subject_code = '812'").Scan(&keep); err != nil {
		t.Fatal(err)
	}
	dup := exec("INSERT INTO catalog_courses (school, major, discipline, subject_name, subject_code, status) VALUES ('四川大学', '宗教学', '', '西方哲学史', '812', 'hidden')")
	exec("INSERT INTO catalog_courses (school, major, discipline, subject_name) VALUES ('某某大学', '中国语言文学', '文学', '文学概论')")
	uid := exec("INSERT INTO users (phone, invite_code) VALUES ('13900005050', 'T50MIG')")
	sid := exec("INSERT INTO subjects (owner_user_id, name, code, catalog_course_id, school, major) VALUES (?, '西方哲学史', '812', ?, '四川大学', '宗教学')", uid, dup)
	pend := exec("INSERT INTO catalog_pending_courses (group_key, school, subject_name, status, catalog_course_id) VALUES ('k-t50', '四川大学', '西方哲学史', 'merged', ?)", dup)
	proj := exec("INSERT INTO official_projects (school, major, subject_code, subject_name) VALUES ('四川大学', '哲学', '812', '西方哲学史')")

	if err := store.MigrateUp(ctx, db, log); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM catalog_courses WHERE school = '四川大学' AND subject_code = '812'").Scan(&n)
	if n != 1 {
		t.Fatalf("同一院校同一代码合并成一条：%d", n)
	}
	var majors, status, key string
	if err := db.QueryRowContext(ctx, "SELECT CAST(majors AS CHAR), status, course_key FROM catalog_courses WHERE id = ?", keep).Scan(&majors, &status, &key); err != nil {
		t.Fatal(err)
	}
	if majors != `["哲学", "宗教学"]` && majors != `["宗教学", "哲学"]` {
		t.Errorf("适用专业：%s", majors)
	}
	if status != "active" || key != "school|四川大学|812|0" {
		t.Errorf("合并后状态与去重键：%s %s", status, key)
	}
	var got int64
	_ = db.QueryRowContext(ctx, "SELECT catalog_course_id FROM subjects WHERE id = ?", sid).Scan(&got)
	if got != keep {
		t.Errorf("用户专业课改指向保留的那条：%d want %d", got, keep)
	}
	_ = db.QueryRowContext(ctx, "SELECT catalog_course_id FROM catalog_pending_courses WHERE id = ?", pend).Scan(&got)
	if got != keep {
		t.Errorf("待入库改指向保留的那条：%d want %d", got, keep)
	}
	_ = db.QueryRowContext(ctx, "SELECT catalog_course_id FROM official_projects WHERE id = ?", proj).Scan(&got)
	if got != keep {
		t.Errorf("旧立项挂到考试科目：%d want %d", got, keep)
	}
	_ = db.QueryRowContext(ctx, "SELECT course_key FROM catalog_courses WHERE school = '某某大学'").Scan(&key)
	if key != "school|某某大学|n:文学概论|0" {
		t.Errorf("没有代码时按名称：%s", key)
	}
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM catalog_courses WHERE scope = 'national' AND school = '' AND subject_code IN ('311', '312', '313', '333')").Scan(&n)
	if n != 4 {
		t.Errorf("第一批全国统考科目：%d", n)
	}
}
