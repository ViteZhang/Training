package textbook

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"sync"
	"testing"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

func setup(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	env := testenv.New(t)
	db, err := store.OpenMySQL(context.Background(), env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.MigrateUp(context.Background(), db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}
func metadata() rules.TextbookMetadata {
	return rules.TextbookMetadata{Title: "文学理论教程", Author: "童庆炳", Kind: "textbook", Discipline: "文学", Aliases: []string{"文论教程"}}
}
func edition() rules.TextbookEditionInput {
	return rules.TextbookEditionInput{Edition: "第五版", Authorized: true, LicenseNote: "测试授权"}
}
func mustBook(t *testing.T, s *Service, key string) Book {
	t.Helper()
	b, err := s.Create(context.Background(), 1, key, metadata(), edition())
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestCatalogIdempotencyAndEditions(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	b := mustBook(t, s, "book-key-1")
	again := mustBook(t, s, "book-key-1")
	if b.ID != again.ID || len(again.Editions) != 1 {
		t.Fatal("重放产生重复记录")
	}
	m := metadata()
	m.Title = "不同书名"
	_, err := s.Create(ctx, 1, "book-key-1", m, edition())
	if !apperr.IsKind(err, apperr.Conflict) {
		t.Fatalf("异参重放: %v", err)
	}
	e := edition()
	e.Edition = "第六版"
	updated, err := s.AddEdition(ctx, b.ID, 1, "edition-key-2", e)
	if err != nil || len(updated.Editions) != 2 {
		t.Fatalf("新增版本: %+v %v", updated, err)
	}
	_, err = s.AddEdition(ctx, b.ID, 1, "edition-key-2", e)
	if err != nil {
		t.Fatal(err)
	}
	e.Publisher = "异参"
	_, err = s.AddEdition(ctx, b.ID, 1, "edition-key-2", e)
	if !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal(err)
	}
	items, total, err := s.List(ctx, Filter{Search: "文论教程", Kind: "textbook", Discipline: "文学", Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("检索: %v %d %d", err, total, len(items))
	}
	m = metadata()
	m.Title = "更新后的书名"
	updated, err = s.Update(ctx, b.ID, b.Version, m)
	if err != nil || updated.Version != b.Version+1 {
		t.Fatal(err)
	}
	_, err = s.Update(ctx, b.ID, b.Version, metadata())
	if !apperr.IsKind(err, apperr.Conflict) {
		t.Fatalf("乐观锁: %v", err)
	}
}
func TestConcurrentCreateReplays(t *testing.T) {
	s, _ := setup(t)
	var wg sync.WaitGroup
	ids := make(chan uint64, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := s.Create(context.Background(), 1, "concurrent-key", metadata(), edition())
			if err != nil {
				errs <- err
				return
			}
			ids <- b.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var first uint64
	for id := range ids {
		if first == 0 {
			first = id
		}
		if id != first {
			t.Fatal("并发重放创建了不同教材")
		}
	}
}
func TestRevisionsIsolationAndImmutability(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	b := mustBook(t, s, "book-key-one")
	other := mustBook(t, s, "book-key-two")
	ed := b.Editions[0].ID
	r, err := s.NewRevision(ctx, b.ID, ed, 1, "revision-key-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.NewRevision(ctx, b.ID, ed, 1, "revision-key-1")
	if err != nil || r.ID != again.ID {
		t.Fatal(err)
	}
	if _, err = s.NewRevision(ctx, other.ID, ed, 1, "wrong-parent"); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("跨教材创建: %v", err)
	}
	if _, err = s.GetRevision(ctx, other.ID, ed, r.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("跨教材读取: %v", err)
	}
	sections := []rules.TextbookSection{{Heading: "第一章", Text: "文学活动的四个要素。"}}
	saved, err := s.SaveRevision(ctx, b.ID, ed, r.ID, 1, r.Version, sections, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "reviewed" || saved.IndexStatus != "stale" || !saved.ReviewedBy.Valid {
		t.Fatalf("校对状态: %+v", saved)
	}
	replay, err := s.SaveRevision(ctx, b.ID, ed, r.ID, 1, r.Version, sections, true)
	if err != nil || replay.Version != saved.Version {
		t.Fatal("保存重试不幂等", err)
	}
	sections[0].Text = "另一段原文"
	if _, err = s.SaveRevision(ctx, b.ID, ed, r.ID, 1, r.Version, sections, false); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatalf("冲突保存: %v", err)
	}
	saved, err = s.SaveRevision(ctx, b.ID, ed, r.ID, 1, saved.Version, sections, false)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "draft" || saved.ReviewedBy.Valid || saved.ReviewedAt.Valid {
		t.Fatal("修改原文未清除校对")
	}
	if _, err = s.SaveRevision(ctx, other.ID, ed, r.ID, 1, saved.Version, sections, false); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("跨教材保存: %v", err)
	}
	// 构造未来发布态，验证基础层也拒绝覆盖，不能只依赖页面隐藏按钮。
	if _, err = db.ExecContext(ctx, "UPDATE textbook_revisions SET status='published', index_status='ready' WHERE id=?", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveRevision(ctx, b.ID, ed, r.ID, 1, saved.Version, sections, false); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatalf("覆盖发布态: %v", err)
	}
}
func TestConcurrentRevisionNumbersAndSaves(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	b := mustBook(t, s, "book-key-con")
	ed := b.Editions[0].ID
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	revs := make(chan Revision, 2)
	for _, key := range []string{"revision-key-a", "revision-key-b"} {
		wg.Add(1)
		go func(k string) { defer wg.Done(); r, err := s.NewRevision(ctx, b.ID, ed, 1, k); errs <- err; revs <- r }(key)
	}
	wg.Wait()
	close(errs)
	close(revs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[uint64]bool{}
	var r Revision
	for rev := range revs {
		r = rev
		if seen[r.RevisionNo] {
			t.Fatal("修订序号重复")
		}
		seen[r.RevisionNo] = true
	}
	errs = make(chan error, 2)
	for _, text := range []string{"编辑甲正文", "编辑乙正文"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			_, err := s.SaveRevision(ctx, b.ID, ed, r.ID, 1, r.Version, []rules.TextbookSection{{Text: text}}, false)
			errs <- err
		}(text)
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if apperr.IsKind(err, apperr.Conflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("并发覆盖: %d/%d", success, conflict)
	}
}
