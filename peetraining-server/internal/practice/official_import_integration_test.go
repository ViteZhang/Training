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
	"peetraining-server/internal/rules"
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
			// T49：没勾「缺答案时由 AI 生成」，解析时不补采分点，等编辑点「生成答案 / 提取采分点」。
			if len(p.Rubric) != 0 || !it.NeedsFill || p.ExamYear == nil || *p.ExamYear != 2024 || it.SourceText == "" {
				t.Errorf("名词解释解析后应待补采分点、带年份：%+v %+v", p, it)
			}
		}
	}
	if term.ID == 0 {
		t.Fatal("没有解析出名词解释")
	}
	// 没有采分点时不能接受，提示先生成。
	if _, err := s.ReviewImportItem(ctx, editor, pid, term.ID, "accept", nil); kind(err) != apperr.BadRequest {
		t.Errorf("没有采分点不能接受：%v", err)
	}
	// 生成答案 / 提取采分点：原文有答案的只提采分点，答案仍算原文的。
	term, err = s.FillImportItem(ctx, pid, term.ID)
	if err != nil {
		t.Fatal(err)
	}
	var filled official.QuestionPayload
	_ = json.Unmarshal(term.Payload, &filled)
	if len(filled.Rubric) == 0 || filled.AnswerAI || term.NeedsFill || term.FillError != "" {
		t.Errorf("提取采分点：%+v %+v", filled, term)
	}
	if _, err := s.FillImportItem(ctx, pid, term.ID); kind(err) != apperr.BadRequest {
		t.Errorf("已经补齐的不再补：%v", err)
	}
	if _, err := s.FillImportItem(ctx, other0(t, s, lead), term.ID); kind(err) != apperr.NotFound {
		t.Errorf("跨项目补全 404：%v", err)
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

	// 表格导入：必填列与真题年份校验；采分点合计不等于分值标红；缺答案的待补，一键补全后由 AI 补上。
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
	if !td.Items[2].NeedsFill || td.Items[2].Confidence != "medium" || td.Items[0].NeedsFill {
		t.Errorf("缺答案的待补：%+v", td.Items)
	}
	// 一键补全：没有队列时同步执行；缺答案的 AI 生成（标 answer_ai），已经填了采分点的保留。
	tfile, err := s.FillImportFile(ctx, pid, tf.ID)
	if err != nil {
		t.Fatal(err)
	}
	td, _ = s.ImportItems(ctx, pid, tf.ID)
	if td.File.Fill.Status != "idle" || td.File.Fill.Total != 2 || td.File.Fill.Done != 2 || td.File.Fill.Failed != 0 {
		t.Errorf("一键补全进度：%+v %+v", tfile.Fill, td.File.Fill)
	}
	var gen, kept official.QuestionPayload
	_ = json.Unmarshal(td.Items[2].Payload, &gen)
	if gen.Answer == "" || len(gen.Rubric) == 0 || !gen.AnswerAI || td.Items[2].NeedsFill {
		t.Errorf("缺答案由 AI 补全：%+v %+v", gen, td.Items[2])
	}
	_ = json.Unmarshal(td.Items[1].Payload, &kept)
	if kept.Answer == "" || !kept.AnswerAI || len(kept.Rubric) != 2 || kept.Rubric[0].Content != "共性与个性的统一" {
		t.Errorf("填了采分点的保留：%+v", kept)
	}
	if _, err := s.FillImportFile(ctx, pid, tf.ID); kind(err) != apperr.BadRequest {
		t.Errorf("都补齐了再点一键补全：%v", err)
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

// other0 建一个别的项目（跨项目访问用）。
func other0(t *testing.T, s *official.Service, lead uint64) uint64 {
	t.Helper()
	id, err := s.CreateProject(context.Background(), lead, "另一所大学", "哲学", "813", "马克思主义哲学", nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestOfficialImportFill 是 T49 的验收：解析后补答案与采分点。
// 缺分值的参考同项目同题型的给分；客观题也补答案；勾「缺答案时由 AI 生成」的解析时就补；
// AI 生成的答案发布后，用户副本标「AI 参考答案」，照常计入预估分。
func TestOfficialImportFill(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	editor, lead := adminUser(t, f, "editor-fill"), adminUser(t, f, "lead-fill")
	pid, err := s.CreateProject(ctx, lead, "四川大学", "西方哲学史", "812", "西方哲学史", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	items := func(fileID uint64) []official.ImportItem {
		t.Helper()
		d, err := s.ImportItems(ctx, pid, fileID)
		if err != nil {
			t.Fatal(err)
		}
		return d.Items
	}
	pl := func(it official.ImportItem) official.QuestionPayload {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		return p
	}

	// 项目里先有一道 5 分的名词解释（表格导入），给没有分值的名词解释参考。
	csv := "题型,分值,题干\n名词解释,5,逻格斯\n"
	base, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: csv})
	if err != nil {
		t.Fatal(err)
	}
	if len(items(base.ID)) != 1 {
		t.Fatal("表格导入一条")
	}
	noScore := "2026 年四川大学西方哲学史真题\n一、名词解释\n1. 理念（柏拉图）\n2. 意志（奥古斯丁）\n二、论述题\n3. 笛卡尔的哲学方法论。"
	ef, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "exam", Format: "text", Text: noScore, ExamYear: 2026})
	if err != nil {
		t.Fatal(err)
	}
	var term, essay official.ImportItem
	for _, it := range items(ef.ID) {
		p := pl(it)
		if p.Score != nil {
			t.Fatalf("原文没有分值：%+v", p)
		}
		switch {
		case p.QType == "term" && term.ID == 0:
			term = it
		case p.QType == "discussion":
			essay = it
		}
	}
	if term.ID == 0 || essay.ID == 0 {
		t.Fatalf("解析：%+v", items(ef.ID))
	}
	// 名词解释参考同项目的 5 分。
	got, err := s.FillImportItem(ctx, pid, term.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p := pl(got); p.Score == nil || *p.Score != 5 || !p.ScoreInferred || !p.AnswerAI || p.Answer == "" || len(p.Rubric) == 0 || !official.RubricMatches(p) {
		t.Errorf("缺分值参考同题型：%+v", p)
	}
	// 论述题没有可参考的：400，原因记在条目上。
	if _, err := s.FillImportItem(ctx, pid, essay.ID); kind(err) != apperr.BadRequest {
		t.Errorf("没有可参考的分值：%v", err)
	}
	for _, it := range items(ef.ID) {
		if it.ID == essay.ID && (it.FillError == "" || !it.NeedsFill) {
			t.Errorf("失败原因记在条目上：%+v", it)
		}
	}

	// 客观题也补答案，不要采分点。
	obj, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: "题型,分值,题干\n客观题,2,提出「知识就是力量」的是\n"})
	if err != nil {
		t.Fatal(err)
	}
	oi := items(obj.ID)[0]
	if !oi.NeedsFill {
		t.Fatalf("客观题缺答案待补：%+v", oi)
	}
	oi, err = s.FillImportItem(ctx, pid, oi.ID)
	if p := pl(oi); err != nil || p.Answer == "" || !p.AnswerAI || len(p.Rubric) != 0 {
		t.Errorf("客观题补答案：%+v %v", p, err)
	}

	// 勾了「缺答案时由 AI 生成」：解析时就补。
	auto, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: "题型,分值,题干\n简答,10,简述洛克的白板说\n", AutoAnswer: true})
	if err != nil {
		t.Fatal(err)
	}
	ai0 := items(auto.ID)[0]
	if p := pl(ai0); ai0.NeedsFill || !p.AnswerAI || len(p.Rubric) == 0 {
		t.Errorf("解析时自动补：%+v %+v", p, ai0)
	}

	// 接受 → 审核 → 发布 → 用户添加：AI 生成的答案在用户题库里是「AI 参考答案」，计入预估分；团队给的答案仍是官方答案。
	for _, it := range []official.ImportItem{got, ai0, items(base.ID)[0]} {
		if it.NeedsFill {
			var err error
			if it, err = s.FillImportItem(ctx, pid, it.ID); err != nil {
				t.Fatal(err)
			}
		}
		p := pl(it)
		if it.ID == items(base.ID)[0].ID {
			p.AnswerAI = false // 编辑改成团队给的答案
			p.Answer = "逻格斯是赫拉克利特提出的万物运动变化的尺度。"
			p.Rubric = []official.Point{{Content: "赫拉克利特提出", Score: 2}, {Content: "万物运动变化的尺度", Score: 3}}
		}
		if _, err := s.ReviewImportItem(ctx, editor, pid, it.ID, "accept", payload(p)); err != nil {
			t.Fatalf("接受 %d：%v", it.ID, err)
		}
	}
	rs, err := s.Reviews(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if err := s.Decide(ctx, lead, r.ID, "approved", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Publish(ctx, lead, pid, ""); err != nil {
		t.Fatal(err)
	}
	var aiOfficial int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM questions WHERE owner_user_id IS NULL AND answer_origin = 'ai_generated'").Scan(&aiOfficial)
	if aiOfficial != 2 {
		t.Errorf("官方题记下 AI 生成的答案：%d", aiOfficial)
	}
	uid, sid := f.user(t)
	if _, err := f.db.Exec("UPDATE subjects SET code = '812' WHERE id = ?", sid); err != nil {
		t.Fatal(err)
	}
	banks, err := s.Banks(ctx, uid)
	if err != nil || len(banks) != 1 {
		t.Fatalf("官方题库：%+v %v", banks, err)
	}
	if err := s.Subscribe(ctx, uid, banks[0].BankID, sid); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Query("SELECT stem, answer, IFNULL(answer_origin, ''), source FROM questions WHERE owner_user_id = ? AND official_question_id IS NOT NULL", uid)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	tiers := map[string]rules.AnswerTier{}
	for rows.Next() {
		var stem, answer, origin, source string
		if err := rows.Scan(&stem, &answer, &origin, &source); err != nil {
			t.Fatal(err)
		}
		tiers[stem] = rules.TierOf(answer, origin, source)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(tiers) != 3 || tiers["逻格斯"] != rules.AnswerMaterial || tiers["简述洛克的白板说"] != rules.AnswerAI {
		t.Errorf("用户副本的答案档位：%+v", tiers)
	}
	for stem, tier := range tiers {
		if !tier.CountsForEstimate() {
			t.Errorf("%s 应计入预估分（%s）", stem, tier)
		}
	}
	// 同步对账不会把没改过的 AI 答案副本当成「用户改过」。
	res, err := s.Sync(ctx, uid, banks[0].BankID, false)
	if err != nil || res.Skipped != 0 || res.Updated != 0 {
		t.Errorf("再次对账：%+v %v", res, err)
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
