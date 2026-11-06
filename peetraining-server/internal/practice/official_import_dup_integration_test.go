package practice_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
)

// exam808MD 是同一份 808 真题整理成的 Markdown：标题、加粗题号、引用、分隔线都要去掉，限选分组照样识别。
const exam808MD = "# 北京师范大学 2025 年 808 中国语言文学\n\n" +
	"## 一、名词解释（共 30 分，每题 6 分）\n\n" +
	"**1.** 复句\n\n" +
	"**2.** 儒家十三经\n\n---\n\n" +
	"## 三、论述题（本题共分七组，请按专业分组答题，每组共 70 分，组内 2 小题，每小题 35 分，回答非报考专业不得分）\n\n" +
	"> 第一组：请选择文艺学、比较文学与世界文学专业的考生作答\n\n" +
	"1. 谈一谈异国形象中\"他者\"与\"自我\"的关系（35 分）\n\n" +
	"> 第二组：请选择古代文学、中国古典文献学专业的考生作答\n\n" +
	"1. 类书是什么？试列举三种类书，并说明类书对[古典文献](https://example.com)和古典文学研究的意义（35 分）\n"

// TestOfficialImportMarkdownDuplicates 是 T52 的验收：后台导入支持 .md（上传文件与粘贴都行），解析前去掉排版标记；
// 同一年份的真题卷传两次时，后传的那份逐题标「重复」、可一键驳回；别的年份考过同一题只提示、不算重复。
func TestOfficialImportMarkdownDuplicates(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	s.ConfigureImports(official.ImportDeps{OSS: f.oss})
	editor, lead := adminUser(t, f, "editor-md"), adminUser(t, f, "lead-md")
	course, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "北京师范大学", SubjectName: "中国语言文学", SubjectCode: "808", Discipline: "文学"})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := s.CreateProjectFromCourse(ctx, lead, course, []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}

	// 上传 .md 文件：拿直传地址 → 传到 OSS → 确认后解析。
	md, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "md", FileName: "2025 真题.md", Size: int64(len(exam808MD)), ExamYear: 2025})
	if err != nil || md.UploadURL == "" || md.Status != "uploading" {
		t.Fatalf("md 文件：%+v %v", md, err)
	}
	f.oss.Seed(fmt.Sprintf("official-imports/%d/%d", pid, md.ID), []byte("\ufeff"+exam808MD))
	if err := s.ConfirmImportFile(ctx, pid, md.ID); err != nil {
		t.Fatal(err)
	}
	d1, err := s.ImportItems(ctx, pid, md.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d1.File.Status != "parsed" || len(d1.Items) != 4 || d1.File.Format != "md" {
		t.Fatalf("md 解析：%+v %d", d1.File, len(d1.Items))
	}
	for _, mark := range []string{"#", "**", "> ", "---", "https://"} {
		if strings.Contains(d1.Text, mark) {
			t.Errorf("原文里还有排版标记 %q：%s", mark, d1.Text)
		}
	}
	limited := 0
	for _, it := range d1.Items {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		if p.Group != nil {
			limited++
		}
		if strings.Contains(p.Stem, "*") || strings.Contains(p.Stem, "](") {
			t.Errorf("题干带排版标记：%q", p.Stem)
		}
		if it.Duplicate != "" {
			t.Errorf("第一次上传不算重复：%+v", it)
		}
	}
	if limited != 2 {
		t.Errorf("md 里的限选分组：%d", limited)
	}

	// 同一年份再粘贴一次（纯文字）：逐题标重复，指向先传的那份，置信度为低。
	again, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "text", FileName: "2025 真题（重传）", Text: exam808, ExamYear: 2025})
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := s.ImportItems(ctx, pid, again.ID)
	if len(d2.Items) != 4 {
		t.Fatalf("重传解析：%d", len(d2.Items))
	}
	for _, it := range d2.Items {
		if !strings.Contains(it.Duplicate, "《2025 真题.md》第") || it.Confidence != "low" {
			t.Errorf("重传的题标重复：%q %s", it.Duplicate, it.Confidence)
		}
	}
	// 先传的那份不因为后传的而标重复。
	if d1, _ = s.ImportItems(ctx, pid, md.ID); d1.Items[0].Duplicate != "" {
		t.Errorf("先传的不标：%+v", d1.Items[0])
	}

	// 一键驳回重复题；驳回后先传的那份仍然干净，核对过的题不再标。
	n, err := s.RejectDuplicates(ctx, editor, pid, again.ID)
	if err != nil || n != 4 {
		t.Fatalf("一键驳回：%d %v", n, err)
	}
	if n, _ := s.RejectDuplicates(ctx, editor, pid, again.ID); n != 0 {
		t.Errorf("再点一次没有可驳回的：%d", n)
	}
	if _, err := s.RejectDuplicates(ctx, editor, other0(t, s, lead), again.ID); kind(err) != apperr.NotFound {
		t.Errorf("跨项目 404：%v", err)
	}

	// 别的年份考了同一题：不算重复，提示 2025 年也考过。粘贴的 md 也去掉排版标记。
	y24 := strings.ReplaceAll(exam808MD, "2025", "2024")
	old, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "md", FileName: "2024 真题", Text: y24, ExamYear: 2024})
	if err != nil {
		t.Fatal(err)
	}
	d3, _ := s.ImportItems(ctx, pid, old.ID)
	if len(d3.Items) != 4 || strings.Contains(d3.Text, "**") {
		t.Fatalf("粘贴的 md：%d %q", len(d3.Items), d3.Text)
	}
	for _, it := range d3.Items {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		if p.ExamYear == nil || *p.ExamYear != 2024 {
			t.Fatalf("年份：%+v", p)
		}
		if it.Duplicate != "" || len(it.SeenYears) != 1 || it.SeenYears[0] != 2025 {
			t.Errorf("别的年份考过：%q %v", it.Duplicate, it.SeenYears)
		}
	}

	// 先传的接受后进了草稿：同年再传仍能认出来（同一份资料里的同一题也算）。
	sc := 6.0
	var first official.QuestionPayload
	_ = json.Unmarshal(d1.Items[0].Payload, &first)
	first.Score, first.Answer, first.Rubric = &sc, "略", []official.Point{{Content: "要点", Score: 6}}
	b, _ := json.Marshal(first)
	if _, err := s.ReviewImportItem(ctx, editor, pid, d1.Items[0].ID, "accept", b); err != nil {
		t.Fatal(err)
	}
	twice, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "text", FileName: "两道一样的", ExamYear: 2025,
		Text: "一、名词解释（共 12 分，每题 6 分）\n1. " + first.Stem + "\n2. " + first.Stem + "\n"})
	if err != nil {
		t.Fatal(err)
	}
	d4, _ := s.ImportItems(ctx, pid, twice.ID)
	if len(d4.Items) != 2 || !strings.Contains(d4.Items[0].Duplicate, "2025 真题.md") || d4.Items[1].Duplicate == "" {
		t.Errorf("已接受的题与同一份里的重复：%+v", d4.Items)
	}
}
