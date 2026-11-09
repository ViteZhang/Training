package practice_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
)

// TestOfficialImportDelete：删除一份导入的资料，连同解析出的题、接受后生成的草稿与审核任务；
// 别的资料、编辑手工建的草稿不受影响；有题已发布时不能删；跨项目 404。
func TestOfficialImportDelete(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	s.ConfigureImports(official.ImportDeps{OSS: f.oss})
	editor, lead := adminUser(t, f, "editor-del"), adminUser(t, f, "lead-del")
	course, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "北京师范大学", SubjectName: "中国语言文学", SubjectCode: "808", Discipline: "文学"})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := s.CreateProjectFromCourse(ctx, lead, course, []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := f.db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// 上传的文件（OSS 里有原文）与粘贴的文字各一份。
	up, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "md", FileName: "2025 真题.md", Size: int64(len(exam808MD)), ExamYear: 2025})
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("official-imports/%d/%d", pid, up.ID)
	f.oss.Seed(key, []byte(exam808MD))
	if err := s.ConfirmImportFile(ctx, pid, up.ID); err != nil {
		t.Fatal(err)
	}
	keep, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "text", FileName: "2024 真题", Text: exam808, ExamYear: 2024})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.ImportItems(ctx, pid, up.ID)
	if err != nil || len(d.Items) == 0 {
		t.Fatalf("解析：%d %v", len(d.Items), err)
	}
	filled := func(it official.ImportItem) json.RawMessage {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		sc := 6.0
		p.Score, p.Answer, p.Rubric = &sc, "略", []official.Point{{Content: "要点", Score: 6}}
		b, _ := json.Marshal(p)
		return b
	}
	accept := func(it official.ImportItem) uint64 {
		t.Helper()
		id, err := s.ReviewImportItem(ctx, editor, pid, it.ID, "accept", filled(it))
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	draft := accept(d.Items[0])
	manual, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, filled(d.Items[0]))
	if err != nil {
		t.Fatal(err)
	}
	if count("SELECT COUNT(*) FROM review_tasks WHERE draft_id = ?", draft) == 0 {
		t.Fatal("接受后应进审核队列")
	}

	// 跨项目、不存在的都是 404。
	if err := s.DeleteImportFile(ctx, other0(t, s, lead), up.ID); kind(err) != apperr.NotFound {
		t.Errorf("跨项目 404：%v", err)
	}
	if err := s.DeleteImportFile(ctx, pid, 999999); kind(err) != apperr.NotFound {
		t.Errorf("不存在 404：%v", err)
	}

	if err := s.DeleteImportFile(ctx, pid, up.ID); err != nil {
		t.Fatal(err)
	}
	if n := count("SELECT COUNT(*) FROM official_import_items WHERE file_id = ?", up.ID); n != 0 {
		t.Errorf("题没删干净：%d", n)
	}
	if n := count("SELECT COUNT(*) FROM official_import_files WHERE id = ?", up.ID); n != 0 {
		t.Errorf("文件没删：%d", n)
	}
	if n := count("SELECT COUNT(*) FROM official_drafts WHERE id = ?", draft) + count("SELECT COUNT(*) FROM review_tasks WHERE draft_id = ?", draft); n != 0 {
		t.Errorf("接受生成的草稿与审核任务没删：%d", n)
	}
	if n := count("SELECT COUNT(*) FROM official_drafts WHERE id = ?", manual.ID); n != 1 {
		t.Errorf("手工草稿不该删：%d", n)
	}
	if info, _ := f.oss.Head(ctx, key); info.Exists {
		t.Error("OSS 原文件应删除")
	}
	if dk, err := s.ImportItems(ctx, pid, keep.ID); err != nil || len(dk.Items) == 0 {
		t.Errorf("另一份资料不受影响：%v", err)
	}
	if err := s.DeleteImportFile(ctx, pid, up.ID); kind(err) != apperr.NotFound {
		t.Errorf("删第二次 404：%v", err)
	}

	// 有题已发布：不能删，什么都不动。
	dk, _ := s.ImportItems(ctx, pid, keep.ID)
	published := accept(dk.Items[0])
	if _, err := f.db.Exec("UPDATE official_drafts SET status = 'published' WHERE id = ?", published); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteImportFile(ctx, pid, keep.ID); kind(err) != apperr.Conflict {
		t.Errorf("已发布不能删：%v", err)
	}
	if n := count("SELECT COUNT(*) FROM official_import_items WHERE file_id = ?", keep.ID); n != len(dk.Items) {
		t.Errorf("拒绝删除时不该动数据：%d", n)
	}
}
