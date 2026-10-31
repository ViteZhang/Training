package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/rules"
)

func TestIntegrationTextbookEdit(t *testing.T) {
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
	actor, err := b.Admin.CreateAdmin(ctx, "textbook-editor", "教材编辑测试", "13700007702", "Init-pass-2026", []admin.Role{admin.RoleAdmin}, false)
	if err != nil {
		t.Fatal(err)
	}
	book, err := b.Textbooks.Create(ctx, actor, "edit-book-key", rules.TextbookMetadata{Title: "教材编辑测试", Author: "测试作者", Kind: "textbook", Discipline: "文学", Aliases: []string{}}, rules.TextbookEditionInput{Edition: "第一版", Authorized: true, LicenseNote: "测试授权", ScopeNote: "原范围"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := b.Handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	f := &adminFx{t: t, b: b, srv: srv}
	token := f.login("textbook-editor", "13700007702", "Init-pass-2026")
	path := fmt.Sprintf("/api/v1/admin/textbooks/%d", book.ID)
	for _, scope := range []string{"新使用范围", ""} {
		payload := fmt.Sprintf(`{"title":"修改后的教材","author":"测试作者","kind":"textbook","discipline":"文学","aliases":[],"expected_version":%d,"edition_scopes":[{"edition_id":%d,"expected_version":%d,"scope_note":%q}]}`, book.Version, book.Editions[0].ID, book.Editions[0].Version, scope)
		code, response := f.do("PUT", path, token, payload)
		if code != 200 {
			t.Fatalf("保存失败: %d %s", code, response)
		}
		code, response = f.do("GET", path, token, "")
		if code != 200 {
			t.Fatalf("回读失败: %d %s", code, response)
		}
		var got gen.Textbook
		if err := json.Unmarshal([]byte(response), &got); err != nil {
			t.Fatal(err)
		}
		if got.Title != "修改后的教材" || len(got.Editions) != 1 || got.Editions[0].ScopeNote != scope {
			t.Fatalf("回读数据不匹配: %s", response)
		}
		book, err = b.Textbooks.Get(ctx, book.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
}
