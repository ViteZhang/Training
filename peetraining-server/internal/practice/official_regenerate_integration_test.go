package practice_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/official"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/textbook"
)

// TestOfficialImportRegenerate 是 T53 的验收：AI 生成的答案没用上教材时逐题说明原因；
// 先导入、后关联教材时可以逐题或整份按关联教材重新生成（原文给的答案不动、原文给的采分点保留），说明不进草稿。
func TestOfficialImportRegenerate(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	engine := ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true})
	books := textbook.New(f.db)
	books.ConfigureEvidence(engine, flags.New(dbq.New(f.db)))
	engine.SetAnswerEvidenceProvider(books)
	s := official.New(official.Deps{DB: f.db, AI: engine, Now: func() time.Time { return f.clock }})
	editor, lead := adminUser(t, f, "editor-regen"), adminUser(t, f, "lead-regen")
	pid, err := s.CreateProject(ctx, lead, "北京师范大学", "中国语言文学", "808", "中国语言文学基础", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	items := func(fileID uint64) map[string]official.ImportItem {
		t.Helper()
		d, err := s.ImportItems(ctx, pid, fileID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]official.ImportItem{}
		for _, it := range d.Items {
			var p official.QuestionPayload
			_ = json.Unmarshal(it.Payload, &p)
			out[p.Stem] = it
		}
		return out
	}
	pl := func(it official.ImportItem) official.QuestionPayload {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		return p
	}

	// 一道 AI 生成答案、一道原文给了采分点没给答案、一道原文给了答案。
	csv := "题型,分值,题干,参考答案,采分点\n" +
		"简答,10,简述意境的内涵,,\n" +
		"简答,10,简述文学的审美属性,,\"审美意识形态 5\n情感性 5\"\n" +
		"名词解释,5,典型,典型是文学作品中显示出特征的人物形象。,\"显示特征的人物形象 5\"\n"
	file, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: csv, AutoAnswer: true})
	if err != nil {
		t.Fatal(err)
	}
	got := items(file.ID)
	gen, kept, orig := got["简述意境的内涵"], got["简述文学的审美属性"], got["典型"]
	// 项目没有关联教材：答案按通用知识生成，说明原因。
	for _, it := range []official.ImportItem{gen, kept} {
		if !it.AnswerAI || it.AnswerBasis == nil || it.AnswerBasis.Reason != ai.BasisNoTextbook {
			t.Fatalf("没关联教材的原因：%+v %+v", it.AnswerBasis, pl(it))
		}
	}
	if !pl(kept).RubricKept || len(pl(kept).Rubric) != 2 || pl(gen).RubricKept {
		t.Fatalf("原文给的采分点记下来：%+v / %+v", pl(kept), pl(gen))
	}
	if orig.AnswerAI || orig.AnswerBasis != nil {
		t.Fatalf("原文给的答案没有说明：%+v", orig)
	}
	// 原文给的答案不能重新生成；别的项目 404。
	if _, err := s.RegenerateImportItem(ctx, pid, orig.ID); kind(err) != apperr.BadRequest {
		t.Errorf("原文答案：%v", err)
	}
	if _, err := s.RegenerateImportItem(ctx, other0(t, s, lead), gen.ID); kind(err) != apperr.NotFound {
		t.Errorf("跨项目：%v", err)
	}
	if _, err := s.RegenerateImportFile(ctx, other0(t, s, lead), file.ID); kind(err) != apperr.NotFound {
		t.Errorf("跨项目整份：%v", err)
	}

	// 关联了一本还不能检索的教材：重新生成后逐本说明原因。
	book, err := books.Create(ctx, lead, "regen-book", rules.TextbookMetadata{Title: "文学理论教程", Author: "童庆炳", Kind: "textbook", Discipline: "文学"},
		rules.TextbookEditionInput{Edition: "第五版", LicenseNote: "测试授权", Authorized: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO official_project_textbooks (project_id, edition_id, created_at) VALUES (?, ?, NOW(3))", pid, book.Editions[0].ID); err != nil {
		t.Fatal(err)
	}
	re, err := s.RegenerateImportItem(ctx, pid, gen.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p := pl(re); p.Regenerate || p.Answer == "" || !p.AnswerAI || p.AnswerSnapshotID == pl(gen).AnswerSnapshotID || len(p.Rubric) == 0 {
		t.Fatalf("单题重新生成：%+v", p)
	}
	if b := re.AnswerBasis; b == nil || b.Reason != ai.BasisUnavailable || len(b.Details) != 1 || !strings.Contains(b.Details[0], "还没发布") {
		t.Fatalf("不能检索的原因：%+v", re.AnswerBasis)
	}

	// 整份重新生成（没有队列时同步执行）：AI 答案都换掉，原文采分点保留，原文答案不动。
	if _, err := s.RegenerateImportFile(ctx, pid, file.ID); err != nil {
		t.Fatal(err)
	}
	after := items(file.ID)
	if p := pl(after["简述文学的审美属性"]); p.Regenerate || p.AnswerSnapshotID == pl(kept).AnswerSnapshotID || !p.RubricKept ||
		len(p.Rubric) != 2 || p.Rubric[0].Content != "审美意识形态" {
		t.Fatalf("原文采分点保留：%+v", p)
	}
	if p := pl(after["典型"]); p.Answer != pl(orig).Answer || p.AnswerAI {
		t.Fatalf("原文答案不动：%+v", p)
	}
	d, err := s.ImportItems(ctx, pid, file.ID)
	if err != nil || d.File.Fill.Status != "idle" {
		t.Fatalf("补全状态：%+v %v", d.File.Fill, err)
	}

	// 接受：说明与内部标记不进草稿。
	id, err := s.ReviewImportItem(ctx, editor, pid, after["简述文学的审美属性"].ID, "accept", nil)
	if err != nil {
		t.Fatal(err)
	}
	var draft string
	if err := f.db.QueryRow("SELECT CAST(payload AS CHAR) FROM official_drafts WHERE id = ?", id).Scan(&draft); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"answer_basis", "rubric_kept", "regenerate"} {
		if strings.Contains(draft, k) {
			t.Errorf("草稿里不该有 %s：%s", k, draft)
		}
	}
	if !strings.Contains(draft, "answer_ai") {
		t.Errorf("草稿保留 AI 生成标记：%s", draft)
	}
	// 没有 AI 答案可以重新生成的资料：400。
	only, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: "题型,分值,题干,参考答案,采分点\n名词解释,5,意象,意象是寓意之象。,\"寓意之象 5\"\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegenerateImportFile(ctx, pid, only.ID); kind(err) != apperr.BadRequest {
		t.Errorf("没有可重新生成的：%v", err)
	}
}
