package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/go-sql-driver/mysql"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// 使用真实中间件、MySQL 与 Redis，复现完整路由无法写入旧审计字段，并验证导入提交。
func TestIntegrationTextbookImportLongAuditAction(t *testing.T) {
	cfg := testConfig(t)
	ctx := context.Background()
	log := logx.New(io.Discard, slog.LevelDebug)
	if err := Migrate(ctx, cfg, log, "up"); err != nil {
		t.Fatal(err)
	}
	b, err := Open(ctx, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	actor, err := b.Admin.CreateAdmin(ctx, "textbook-audit", "教材审计测试", "13700007701", "Init-pass-2026", []admin.Role{admin.RoleAdmin}, false)
	if err != nil {
		t.Fatal(err)
	}
	book, err := b.Textbooks.Create(ctx, actor, "audit-book-key", rules.TextbookMetadata{Title: "教材审计测试", Author: "测试作者", Kind: "textbook", Discipline: "文学"}, rules.TextbookEditionInput{Edition: "第一版", Authorized: true, LicenseNote: "测试授权"})
	if err != nil {
		t.Fatal(err)
	}
	edition := book.Editions[0].ID
	revision, err := b.Textbooks.NewRevision(ctx, book.ID, edition, actor, "audit-revision-key")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("# 第一章\n文学理论教材测试原文。")
	hash := sha256.Sum256(data)
	job, _, err := b.Textbooks.CreateImport(ctx, book.ID, edition, revision.ID, actor, "audit-import-key", rules.TextbookImportFile{Name: "教材.md", Format: "md", Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])})
	if err != nil {
		t.Fatal(err)
	}
	b.Cloud.OSS.(*oss.Mock).Seed(fmt.Sprintf("textbooks/imports/%d/upload", job.ID), data)
	handler, err := b.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	f := &adminFx{t: t, b: b, srv: srv}
	token := f.login("textbook-audit", "13700007701", "Init-pass-2026")
	root := fmt.Sprintf("/api/v1/admin/textbooks/%d/editions/%d/revisions/%d/imports/%d", book.ID, edition, revision.ID, job.ID)
	body := `{"idempotency_key":"audit-commit-key"}`
	action := "POST /admin/textbooks/:bookId/editions/:editionId/revisions/:revisionId/imports/:importId/commit"

	// 仅对本测试独占数据库还原旧字段，确认生产日志中的 MySQL 1406 错误。
	if err := store.MigrateDownTo(ctx, b.DB, 34); err != nil {
		t.Fatal(err)
	}
	var mysqlErr *mysql.MySQLError
	if err := b.Admin.Audit(ctx, actor, action, "book", fmt.Sprint(book.ID), nil, "127.0.0.1"); !errors.As(err, &mysqlErr) || mysqlErr.Number != 1406 {
		t.Fatalf("旧字段应复现审计长度错误: %v", err)
	}
	for range 2 {
		if err := store.MigrateUp(ctx, b.DB, log); err != nil {
			t.Fatal(err)
		}
	}
	if code, response := f.do("POST", root+"/upload", token, ""); code != 200 {
		t.Fatalf("原件重传地址应可续签: %d %s", code, response)
	}
	for range 2 {
		if code, response := f.do("POST", root+"/commit", token, body); code != 200 {
			t.Fatalf("扩容后提交及重放应成功: %d %s", code, response)
		}
	}
	if code, response := f.do("POST", root+"/upload", token, ""); code != 409 {
		t.Fatalf("已排队任务不应重新生成上传地址: %d %s", code, response)
	}
	var stored string
	if err := b.DB.QueryRowContext(ctx, "SELECT action FROM admin_audit_logs WHERE admin_id=? AND action=? LIMIT 1", actor, action).Scan(&stored); err != nil || stored != action {
		t.Fatalf("必须完整保留长审计路径: %q %v", stored, err)
	}
	uploadAction := "POST /admin/textbooks/:bookId/editions/:editionId/revisions/:revisionId/imports/:importId/upload"
	if err := b.DB.QueryRowContext(ctx, "SELECT action FROM admin_audit_logs WHERE admin_id=? AND action=? LIMIT 1", actor, uploadAction).Scan(&stored); err != nil || stored != uploadAction {
		t.Fatalf("必须完整保留续签审计路径: %q %v", stored, err)
	}
	var status string
	var attempt int
	if err := b.DB.QueryRowContext(ctx, "SELECT status,attempt FROM textbook_imports WHERE id=?", job.ID).Scan(&status, &attempt); err != nil || status != "queued" || attempt != 1 {
		t.Fatalf("重放不应重复排队: status=%s attempt=%d err=%v", status, attempt, err)
	}
}
