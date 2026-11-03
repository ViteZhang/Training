package practice_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
)

// TestOfficialImport 是 T47 的验收：粘贴真题 → AI 解析出题目与置信度 → 逐条接受 / 修改 / 驳回 → 接受的进 7.13 审核；
// 表格导入按字段校验，采分点合计不等于分值时标红、不能接受；解析数据与用户资料分表。
func TestOfficialImport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	editor, lead := adminUser(t, f, "editor-import"), adminUser(t, f, "lead-import")
	pid, err := s.CreateProject(ctx, lead, "某某大学", "中国古代文学", "654", "中国语言文学基础", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}

	// 粘贴真题卷：没有队列时同步解析。
	file, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "text", FileName: "2024 真题", Text: exam, ExamYear: 2024})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.ImportItems(ctx, pid, file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.File.Status != "parsed" || d.File.QuestionCount != 4 || d.Text == "" {
		t.Fatalf("解析：%+v（%d 条）", d.File, len(d.Items))
	}
	var term official.ImportItem
	for _, it := range d.Items {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		if p.QType == "term" && term.ID == 0 {
			term = it
			if len(p.Rubric) == 0 || p.ExamYear == nil || *p.ExamYear != 2024 || it.SourceText == "" {
				t.Errorf("名词解释应补上采分点与年份：%+v %s", p, it.SourceText)
			}
		}
	}
	if term.ID == 0 {
		t.Fatal("没有解析出名词解释")
	}
	// 别的项目拿不到这份资料。
	other, _ := s.CreateProject(ctx, lead, "别的大学", "哲学", "812", "西方哲学史", nil)
	if _, err := s.ImportItems(ctx, other, file.ID); kind(err) != apperr.NotFound {
		t.Errorf("跨项目 404：%v", err)
	}

	// 修改后接受：采分点合计不等于分值时不能接受。
	var p official.QuestionPayload
	_ = json.Unmarshal(term.Payload, &p)
	sc := 10.0
	p.Score = &sc
	p.KPNames = nil
	p.Rubric = []official.Point{{Content: "情景交融", Score: 5}, {Content: "虚实相生", Score: 4}}
	bad, _ := json.Marshal(p)
	if _, err := s.ReviewImportItem(ctx, editor, pid, term.ID, "accept", bad); kind(err) != apperr.BadRequest {
		t.Errorf("采分点合计不等于分值：%v", err)
	}
	p.Rubric[1].Score = 5
	good, _ := json.Marshal(p)
	draftID, err := s.ReviewImportItem(ctx, editor, pid, term.ID, "accept", good)
	if err != nil || draftID == 0 {
		t.Fatalf("接受：%d %v", draftID, err)
	}
	if _, err := s.ReviewImportItem(ctx, editor, pid, term.ID, "reject", nil); kind(err) != apperr.Conflict {
		t.Errorf("重复核对：%v", err)
	}
	rs, _ := s.Reviews(ctx, pid)
	if len(rs) != 1 || rs[0].DraftID != draftID {
		t.Errorf("接受的进审核队列：%+v", rs)
	}
	// 驳回一条。
	for _, it := range d.Items {
		if it.ID != term.ID {
			if _, err := s.ReviewImportItem(ctx, editor, pid, it.ID, "reject", nil); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if d2, _ := s.ImportItems(ctx, pid, file.ID); d2.File.ReviewedCount != 2 {
		t.Errorf("已核对数：%d", d2.File.ReviewedCount)
	}

	// 表格导入：必填列与真题年份校验；采分点合计不等于分值标红；缺答案由 AI 补全。
	if _, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: "题型,分值\n名词解释,5"}); err != nil {
		t.Fatal(err) // 缺列在解析时报错，文件标失败
	}
	files, _ := s.ImportFiles(ctx, pid)
	if files[0].Status != "failed" || files[0].FailReason == "" {
		t.Errorf("缺必填列应解析失败：%+v", files[0])
	}
	csv := "来源类型,年份,题型,分值,题干,参考答案,采分点,知识点\n" +
		"真题,2023,名词解释,5,意境,意境是情景交融、虚实相生的艺术境界。,\"情景交融（3分）\n虚实相生 2分\",意境\n" +
		"真题,2023,简答,10,简述典型的含义,,\"共性与个性的统一（4分）\n反映本质（5分）\",典型\n" +
		"模拟卷,,论述,30,论述意境理论的发展,,,\n"
	tf, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", FileName: "题目.csv", Text: csv})
	if err != nil {
		t.Fatal(err)
	}
	td, err := s.ImportItems(ctx, pid, tf.ID)
	if err != nil || td.File.Status != "parsed" || len(td.Items) != 3 {
		t.Fatalf("表格导入：%+v %v", td.File, err)
	}
	if td.Items[0].Confidence != "high" || td.Items[0].RubricMismatch {
		t.Errorf("完整的一行置信度高：%+v", td.Items[0])
	}
	if !td.Items[1].RubricMismatch {
		t.Errorf("采分点合计 9 ≠ 10 应标红：%+v", td.Items[1])
	}
	var gen official.QuestionPayload
	_ = json.Unmarshal(td.Items[2].Payload, &gen)
	if gen.Answer == "" || len(gen.Rubric) == 0 || td.Items[2].Confidence != "medium" {
		t.Errorf("缺答案由 AI 补全：%+v %s", gen, td.Items[2].Confidence)
	}
	bad2 := "题型,分值,题干,来源类型\n名词解释,5,意境,真题\n"
	bf, _ := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: bad2})
	if bd, _ := s.ImportItems(ctx, pid, bf.ID); bd.File.Status != "failed" {
		t.Errorf("真题没填年份应失败：%+v", bd.File)
	}

	// 参考书目录：生成知识点框架。
	cf, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "catalog", Format: "text", Text: notes})
	if err != nil {
		t.Fatal(err)
	}
	if cd, _ := s.ImportItems(ctx, pid, cf.ID); cd.File.KPCount == 0 {
		t.Errorf("目录生成知识点：%+v", cd.File)
	}

	// 分表存放：用户资料表里没有这些内容。
	var n int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM materials").Scan(&n)
	if n != 0 {
		t.Errorf("7.17 不写用户资料表：%d", n)
	}
}

func TestParseRubricCell(t *testing.T) {
	pts, err := official.ParseRubricCell("对象符合认识（4 分）\n先天直观形式：4\n调和经验论与唯理论 4分\n认识限于现象（2）")
	if err != nil || len(pts) != 4 || pts[0].Score != 4 || pts[3].Score != 2 || pts[1].Content != "先天直观形式" {
		t.Errorf("%+v %v", pts, err)
	}
	if _, err := official.ParseRubricCell("没有分值的要点"); err == nil {
		t.Error("缺分值应报错")
	}
}
