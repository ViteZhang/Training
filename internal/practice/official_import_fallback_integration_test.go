package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
)

// 模型对某批题的输出一直不合格时，拆半重试到单块，再按规则照录原文（低置信度），不让整份资料解析失败
// （北师大 708 真题合集：模型输出时好时坏，一批不合格整份就显示「解析失败」）。
func TestOfficialImportFallsBackWhenModelOutputInvalid(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	m := cloudai.NewMock()
	m.Set(ai.Structure.Name, "not json")
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Client: m, Queries: dbq.New(f.db)}), Now: func() time.Time { return f.clock }})
	editor, lead := adminUser(t, f, "editor-fallback"), adminUser(t, f, "lead-fallback")
	pid, err := s.CreateProject(ctx, lead, "北京师范大学", "中国语言文学", "808", "中国语言文学", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	file, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "text", FileName: "808", Text: exam808, ExamYear: 2025})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.ImportItems(ctx, pid, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.File.Status != "parsed" || d.File.QuestionCount != 4 {
		t.Fatalf("应按规则照录 4 道题：%+v", d.File)
	}
	for _, it := range d.Items {
		if it.Confidence != "low" || it.SourceText == "" {
			t.Errorf("规则兜底的题应为低置信度、带原文：%+v", it)
		}
	}
	// 拆半重试：一整批、两个半批、四个单块各调用两次（每次不合格重试一次）。
	if n := len(m.Calls()); n < 4*2 {
		t.Errorf("应拆到单块重试，调用了 %d 次", n)
	}
}
