package practice_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/params"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
)

// TestOfficialTableKPsAndReference 是 T56 的验收：后台表格导入知识点表与题目表（答案来源、解析、出处、多个知识点），
// 题目对上知识点才能发布；发布后用户端看到出处、解析，团队答案显示「官方答案」、AI 生成的显示「AI 参考答案」；
// 再导入同名知识点按修订处理；只改出处的新版本也同步给已添加的用户。
func TestOfficialTableKPsAndReference(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	q := dbq.New(f.db)
	engine := ai.NewEngine(ai.Config{Queries: q, UseMock: true})
	s := official.New(official.Deps{DB: f.db, AI: engine, Now: func() time.Time { return f.clock }})
	ps := params.New(q)
	bk := bank.New(bank.Deps{DB: f.db, AI: engine, Quota: quota.New(q, ps, func() time.Time { return f.clock }), Params: ps, Now: func() time.Time { return f.clock }})
	editor, lead := adminUser(t, f, "editor-t56"), adminUser(t, f, "lead-t56")
	pid, err := s.CreateProject(ctx, lead, "北京师范大学", "中国语言文学", "708", "中国语言文学综合", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	var lastFile uint64
	importCSV := func(text string) []official.ImportItem {
		t.Helper()
		file, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: text})
		if err != nil {
			t.Fatal(err)
		}
		lastFile = file.ID
		d, err := s.ImportItems(ctx, pid, file.ID)
		if err != nil {
			t.Fatal(err)
		}
		if d.File.Status != "parsed" {
			t.Fatalf("解析：%+v", d.File)
		}
		return d.Items
	}
	publish := func() {
		t.Helper()
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
	}
	accept := func(items []official.ImportItem) {
		t.Helper()
		for _, it := range items {
			if _, err := s.ReviewImportItem(ctx, editor, pid, it.ID, "accept", nil); err != nil {
				t.Fatalf("接受第 %d 条：%v", it.Seq, err)
			}
		}
	}

	// 先导题目表：知识点还没有，核对区标出对不上的名称。
	questions := "年份,题号,题型,分值,题干,参考答案,解析,采分点,知识点,出处,答案来源,来源类型\n" +
		"2026,一-1,简答,10,请列出六书并评价,\"1. 六书的名称\n2. 六书的评价\",六书是汉字构形的条例,\"六书的名称（6分）\n六书的评价（4分）\",六书；古注的阅读,王宁《古代汉语》六书,AI生成,真题\n" +
		"2026,一-2,简答,10,简述善本,1. 善本的三性,,善本的三性（10分）,善本,郭英德《中国古典文献学的理论与方法》善本,团队,真题\n"
	qs := importCSV(questions)
	questionFile := lastFile
	if len(qs) != 2 || !slices.Equal(qs[0].MissingKPs, []string{"六书", "古注的阅读"}) || !slices.Equal(qs[1].MissingKPs, []string{"善本"}) {
		t.Fatalf("题目关联的知识点都还没有：%+v", qs)
	}
	if !qs[0].AnswerAI || qs[1].AnswerAI {
		t.Errorf("答案来源：%v %v", qs[0].AnswerAI, qs[1].AnswerAI)
	}

	// 再导知识点表：标记随之消失。
	kps := importCSV("板块,章节,知识点,原文表述,采分点,出处\n" +
		"古代汉语,汉字,六书,六书是汉字构形与使用的六种条例。,\"名称（5分）\n评价（5分）\",王宁《古代汉语》六书\n" +
		"古代汉语,古注,古注的阅读,阅读古注要分清经注疏。,层次（10分）,王宁《古代汉语》古注\n" +
		"古典文献学,版本,善本,善本以三性为标准。,三性（10分）,郭英德《中国古典文献学的理论与方法》善本\n")
	if len(kps) != 3 || kps[0].ItemType != "knowledge_point" || kps[0].ExistingKP {
		t.Fatalf("知识点表：%+v", kps)
	}
	d, err := s.ImportItems(ctx, pid, questionFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range d.Items {
		if len(it.MissingKPs) > 0 {
			t.Errorf("知识点表导入后不应再标对不上：%+v", it.MissingKPs)
		}
	}

	// 只接受题目、不接受知识点时，发布前检查拦下来。
	accept(qs)
	rs, err := s.Reviews(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if err := s.Decide(ctx, lead, r.ID, "approved", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Publish(ctx, lead, pid, ""); err == nil {
		t.Fatal("题目关联的知识点不存在时不能发布")
	}
	pv, err := s.ReleasePreview(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if c := pv.Checks[len(pv.Checks)-1]; c.Name != "题目关联的知识点都存在" || c.Passed {
		t.Errorf("发布预览的检查：%+v", pv.Checks)
	}
	accept(kps)
	publish()

	// 用户添加官方题库：出处、解析、答案档位、知识点关联都到用户题库。
	uid, sid := f.user(t)
	if _, err := f.db.Exec("UPDATE subjects SET code = '708' WHERE id = ?", sid); err != nil {
		t.Fatal(err)
	}
	banks, err := s.Banks(ctx, uid)
	if err != nil || len(banks) != 1 {
		t.Fatalf("官方题库：%+v %v", banks, err)
	}
	if err := s.Subscribe(ctx, uid, banks[0].BankID, sid); err != nil {
		t.Fatal(err)
	}
	ids := map[string]uint64{}
	func() {
		rows, err := f.db.Query("SELECT id, stem FROM questions WHERE owner_user_id = ? AND official_question_id IS NOT NULL", uid)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id uint64
			var stem string
			_ = rows.Scan(&id, &stem)
			ids[stem] = id
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}()
	liushu, err := bk.Question(ctx, uid, ids["请列出六书并评价"])
	if err != nil {
		t.Fatal(err)
	}
	if liushu.Reference != "王宁《古代汉语》六书" || liushu.Analysis != "六书是汉字构形的条例" || liushu.AnswerTier != string(rules.AnswerAI) {
		t.Errorf("六书题：出处 %q 解析 %q 档位 %s", liushu.Reference, liushu.Analysis, liushu.AnswerTier)
	}
	if len(liushu.KPs) != 2 || liushu.KPs[0].Name != "六书" || !liushu.KPs[0].IsPrimary {
		t.Errorf("六书题的知识点：%+v", liushu.KPs)
	}
	shanben, err := bk.Question(ctx, uid, ids["简述善本"])
	if err != nil {
		t.Fatal(err)
	}
	if shanben.AnswerTier != string(rules.AnswerOfficial) || shanben.Reference == "" {
		t.Errorf("善本题：档位 %s 出处 %q", shanben.AnswerTier, shanben.Reference)
	}
	kp, err := bk.KnowledgePoint(ctx, uid, liushu.KPs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if kp.Reference != "王宁《古代汉语》六书" {
		t.Errorf("知识点出处：%q", kp.Reference)
	}

	// 再导同名知识点：标「已有同名」，接受后修订（不新建），新出处同步给用户。
	again := importCSV("板块,章节,知识点,原文表述,采分点,出处\n古代汉语,汉字,六书,六书是汉字构形与使用的六种条例。,\"名称（5分）\n评价（5分）\",王宁《古代汉语》第二章六书\n")
	if len(again) != 1 || !again[0].ExistingKP || again[0].Duplicate != "" {
		t.Fatalf("同名知识点：%+v", again)
	}
	draftID, err := s.ReviewImportItem(ctx, editor, pid, again[0].ID, "accept", json.RawMessage(`{"section":"古代汉语","chapter":"汉字","name":"六书","original_text":"六书是汉字构形与使用的六种条例。","rubric":[{"content":"名称","score":5},{"content":"评价","score":5}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var change string
	_ = f.db.QueryRow("SELECT change_type FROM official_drafts WHERE id = ?", draftID).Scan(&change)
	if change != "revise" {
		t.Errorf("同名知识点应按修订处理：%s", change)
	}
	publish()
	var n int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM knowledge_points WHERE owner_user_id IS NULL AND level = 'point' AND name = '六书'").Scan(&n)
	if n != 1 {
		t.Errorf("不应重复新建同名知识点：%d", n)
	}
	kp, err = bk.KnowledgePoint(ctx, uid, liushu.KPs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	// 「修改后接受」的内容里没带出处，沿用解析出来的新出处。
	if kp.Reference != "王宁《古代汉语》第二章六书" {
		t.Errorf("只改出处的版本应同步给用户：%q", kp.Reference)
	}
}
