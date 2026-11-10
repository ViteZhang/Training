package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/practice"
)

// TestOfficialExamPapers 是 T57 的验收：添加官方题库后，官方真题按年份组成真题卷（能整卷作答、交卷后计入估分）、计入知识点考频；
// 某一年用户自己也导入过真题时只用官方的，免得同一年的题算两遍；移除官方题库后那一年的真题卷改回用户自己的。
func TestOfficialExamPapers(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	editor, lead := adminUser(t, f, "editor-t57"), adminUser(t, f, "lead-t57")
	pid, err := s.CreateProject(ctx, lead, "某某大学", "中国语言文学", "654", "中国语言文学基础", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	importAndAccept := func(text string) {
		t.Helper()
		file, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		d, err := s.ImportItems(ctx, pid, file.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range d.Items {
			if _, err := s.ReviewImportItem(ctx, editor, pid, it.ID, "accept", nil); err != nil {
				t.Fatalf("接受第 %d 条：%v", it.Seq, err)
			}
		}
	}
	importAndAccept("板块,章节,知识点,原文表述,采分点,出处\n文学理论,审美范畴,意境,意境是情景交融、虚实相生的艺术境界。,\"情景交融（5分）\n虚实相生（5分）\",童庆炳《文学理论教程》\n")
	importAndAccept("年份,题型,分值,题干,参考答案,采分点,知识点,出处,答案来源,来源类型\n" +
		"2024,名词解释,10,意境,意境是情景交融、虚实相生的艺术境界。,\"情景交融（5分）\n虚实相生（5分）\",意境,童庆炳《文学理论教程》,AI生成,真题\n" +
		"2024,论述题,30,论述意境的特征,意境的特征是情景交融、虚实相生、韵味无穷。,\"情景交融（10分）\n虚实相生（10分）\n韵味无穷（10分）\",意境,童庆炳《文学理论教程》,团队,真题\n" +
		"2025,名词解释,10,典型,典型是共性与个性的统一。,\"共性（5分）\n个性（5分）\",,童庆炳《文学理论教程》,团队,真题\n")
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

	// 用户先自己导入了 2024 年真题（4 题），组成了自己的 2024 年真题卷。
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	if pb := realPaper(t, f, uid, sid); pb.QuestionCount != 4 || *pb.ExamYear != 2024 {
		t.Fatalf("自己的 2024 年真题卷：%+v", pb)
	}
	if _, err := f.db.Exec("UPDATE subjects SET code = '654' WHERE id = ?", sid); err != nil {
		t.Fatal(err)
	}
	banks, err := s.Banks(ctx, uid)
	if err != nil || len(banks) != 1 {
		t.Fatalf("官方题库：%+v %v", banks, err)
	}
	if err := s.Subscribe(ctx, uid, banks[0].BankID, sid); err != nil {
		t.Fatal(err)
	}

	// 添加后：2025 年有官方真题卷；2024 年改用官方的 2 题，不和自己导入的 4 题混在一起。
	papers := func() map[int]practice.PaperBrief {
		t.Helper()
		l, err := f.pr.Papers(ctx, uid, sid)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int]practice.PaperBrief{}
		for _, p := range l.Real {
			out[*p.ExamYear] = p
		}
		return out
	}
	ps := papers()
	if ps[2025].QuestionCount != 1 || ps[2024].QuestionCount != 2 {
		t.Fatalf("官方真题卷：2025 %+v；2024 %+v", ps[2025], ps[2024])
	}
	d, err := f.pr.Paper(ctx, uid, ps[2024].ID)
	if err != nil || !d.CountsForEstimate {
		t.Fatalf("官方真题卷计入估分：%+v %v", d, err)
	}

	// 考频：官方知识点「意境」在 2024 年考了 2 题；自己导入的 2024 年真题不再重复计。
	var examCount int
	if err := f.db.QueryRow("SELECT exam_count FROM knowledge_points WHERE owner_user_id = ? AND name = '意境' AND official_kp_id IS NOT NULL", uid).Scan(&examCount); err != nil {
		t.Fatal(err)
	}
	if examCount != 2 {
		t.Errorf("意境的考频：%d", examCount)
	}

	// 整卷作答官方 2024 年真题卷：交卷批改完计入估分。
	v, err := f.pr.StartPaper(ctx, uid, ps[2024].ID, "practice", nextKey())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range v.Items {
		a := "意境是情景交融、虚实相生的艺术境界，韵味无穷。"
		if _, err := f.pr.UpdatePaperItem(ctx, uid, v.ID, it.Seq, practice.ItemUpdate{Draft: &a, TimeDelta: 60}); err != nil {
			t.Fatal(err)
		}
	}
	if v, err = f.pr.SubmitPaper(ctx, uid, v.ID); err != nil || v.Status != "graded" || v.Score == nil {
		t.Fatalf("交卷：%+v %v", v, err)
	}
	var estimates int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM score_estimates WHERE owner_user_id = ? AND subject_id = ?", uid, sid).Scan(&estimates)
	if estimates == 0 {
		t.Error("官方真题卷交卷后应参与估分")
	}

	// 移除官方题库：2024 年真题卷改回自己导入的 4 题，2025 年的卷子没有题了。
	if err := s.Unsubscribe(ctx, uid, banks[0].BankID); err != nil {
		t.Fatal(err)
	}
	ps = papers()
	if ps[2024].QuestionCount != 4 {
		t.Errorf("移除后 2024 年真题卷：%+v", ps[2024])
	}
	if p, ok := ps[2025]; ok {
		t.Errorf("移除后 2025 年不应再有真题卷：%+v", p)
	}
}
