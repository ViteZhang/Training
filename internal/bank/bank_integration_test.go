package bank_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/material"
	"peetraining-server/internal/params"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type inlineQueue struct{ h *jobs.Handlers }

func (q *inlineQueue) EnqueueContext(ctx context.Context, t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{}, q.h.Mux().ProcessTask(ctx, t)
}

type fx struct {
	db   *sql.DB
	bank *bank.Service
	imp  *importer.Service
	mat  *material.Service
	prof *profile.Service
	q    *quota.Service
}

func setup(t *testing.T) *fx {
	t.Helper()
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC) }
	q := dbq.New(db)
	ps := params.New(q)
	o := oss.NewMock()
	f := &fx{db: db}
	f.q = quota.New(q, ps, now)
	f.prof = profile.New(db, ps, o, now)
	f.mat = material.New(material.Deps{DB: db, OSS: o, Moderation: moderation.NewMock(), Quota: f.q, Params: ps, OCR: ocr.NewMock(), PDF: ocr.NewMock(), Flags: flags.New(q), Now: now})
	engine := ai.NewEngine(ai.Config{Queries: q, UseMock: true})
	queue := &inlineQueue{}
	f.imp = importer.New(importer.Deps{DB: db, Material: f.mat, Quota: f.q, AI: engine, Queue: queue, Now: now})
	queue.h = &jobs.Handlers{Logger: logx.New(io.Discard, slog.LevelInfo), Import: f.imp, Material: f.mat, Permanent: material.IsPermanent}
	f.bank = bank.New(bank.Deps{DB: db, AI: engine, Quota: f.q, Params: ps, Now: now})
	return f
}

var phone = 0

func (f *fx) user(t *testing.T) (uint64, uint64) {
	t.Helper()
	phone++
	p := "1370000" + strings.Repeat("0", 4-len(itoa(phone))) + itoa(phone)
	res, err := f.db.Exec("INSERT INTO users (phone, invite_code) VALUES (?, ?)", p, p[3:])
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	s, err := f.prof.CreateSubject(context.Background(), uint64(id), profile.SubjectInput{Name: "中国语言文学基础", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id), s.ID
}

func itoa(n int) string {
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

const exam = `2024 年某某大学中国语言文学基础考研真题
一、名词解释（每题 10 分）
1. 意境
答案：意境是情景交融、虚实相生的艺术境界。
2. 典型
答案：典型是共性与个性的统一。
二、单项选择题（每题 2 分）
3. 下列属于唐传奇的是
A. 莺莺传
B. 搜神记
答案：A`

const notes = `第一章 文学理论
一、审美范畴
意象：融入主观情意的客观物象。
意境：情景交融、虚实相生的艺术境界。`

// imported 导入一份真题和一份讲义并全部确认入库。
func (f *fx) imported(t *testing.T, uid, sid uint64) {
	t.Helper()
	ctx := context.Background()
	for i, c := range []struct{ mode, text string }{{"question", exam}, {"reference", notes}} {
		m, err := f.mat.CreatePasted(ctx, uid, sid, c.mode, "资料"+itoa(i+1), c.text, true)
		if err != nil {
			t.Fatal(err)
		}
		job, err := f.imp.CreateJob(ctx, uid, sid, c.mode, []uint64{m.ID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.imp.Confirm(ctx, uid, job.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func find(nodes []*bank.Node, name string) *bank.Node {
	for _, n := range nodes {
		if n.Name == name {
			return n
		}
		if x := find(n.Children, name); x != nil {
			return x
		}
	}
	return nil
}

// notesKP 在讲义的板块里找知识点（真题里同名的考点在「待整理」板块下，是另一个节点）。
func notesKP(roots []*bank.Node, name string) *bank.Node {
	return find(find(roots, "第一章 文学理论").Children, name)
}

func TestTreeOverviewAndKnowledgePoint(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	o, err := f.bank.Overview(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if o.Questions != 3 || o.KPs < 4 || o.Materials != 2 || o.Papers != 1 || o.Unlearned != o.KPs {
		t.Fatalf("概况：%+v", o)
	}
	roots, _, err := f.bank.Tree(ctx, uid, sid, "all")
	if err != nil {
		t.Fatal(err)
	}
	sec := find(roots, "第一章 文学理论")
	if sec == nil || sec.Level != "section" || sec.KPCount != 2 || !sec.IsWeak {
		t.Fatalf("板块：%+v", sec)
	}
	yijing := notesKP(roots, "意境")
	if yijing == nil {
		t.Fatal("没有意境")
	}
	exams, _, _ := f.bank.Tree(ctx, uid, sid, "exam")
	if find(exams, "意象") != nil || find(exams, "典型") == nil {
		t.Fatal("「真题考过」筛选不对")
	}

	// 知识点卡片：第一次打开生成并缓存 AI 解读
	d, err := f.bank.KnowledgePoint(ctx, uid, yijing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.AIExplanation == "" || d.Source == nil || d.Source.Page != 1 || len(d.Path) != 2 || len(d.Rubric) == 0 {
		t.Fatalf("卡片：%+v", d)
	}
	var cached sql.NullString
	_ = f.db.QueryRow("SELECT ai_explanation FROM knowledge_points WHERE id = ?", yijing.ID).Scan(&cached)
	if cached.String != d.AIExplanation {
		t.Fatal("AI 解读应缓存")
	}

	// 编辑：改采分点后解读作废，下次打开重新生成
	ten := 10.0
	name := "意境（审美范畴）"
	d, err = f.bank.UpdateKnowledgePoint(ctx, uid, yijing.ID, bank.KPPatch{Name: &name, RubricSet: true, Rubric: []bank.RubricInput{{Content: "情景交融", Score: &ten}}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != name || len(d.Rubric) != 1 || d.Rubric[0].Origin != "user_confirmed" || d.AIExplanation != "" || d.Origin != "user_confirmed" {
		t.Fatalf("编辑后：%+v", d)
	}

	// 自评：没有作答记录时按自评设掌握分；有作答记录后只记录
	m, err := f.bank.SelfAssess(ctx, uid, yijing.ID, "vague")
	if err != nil || m.M != 30 || m.State != "learning" || m.LastSelfAssess != "vague" {
		t.Fatalf("自评：%+v %v", m, err)
	}
	if _, err := f.db.Exec("UPDATE kp_mastery SET answered = 1, m = 65, state = 'consolidating' WHERE kp_id = ?", yijing.ID); err != nil {
		t.Fatal(err)
	}
	if m, _ = f.bank.SelfAssess(ctx, uid, yijing.ID, "mastered"); m.M != 65 || m.State != "consolidating" || m.LastSelfAssess != "mastered" {
		t.Fatalf("有作答记录后自评只记录：%+v", m)
	}
	if _, err := f.bank.SelfAssess(ctx, uid, yijing.ID, "bad"); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal(err)
	}
}

func TestMergeSplitMoveDelete(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	roots, _, _ := f.bank.Tree(ctx, uid, sid, "all")
	yixiang, yijing, dianxing := notesKP(roots, "意象"), notesKP(roots, "意境"), find(roots, "典型")

	// 合并：意象 → 意境；题目关联、出处、采分点迁过去
	d, err := f.bank.Merge(ctx, uid, yixiang.ID, yijing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rubric) < 2 {
		t.Fatalf("合并后采分点：%+v", d.Rubric)
	}
	if _, err := f.bank.KnowledgePoint(ctx, uid, yixiang.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("来源知识点应删除")
	}
	if _, err := f.bank.Merge(ctx, uid, yijing.ID, yijing.ID); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("不能合并到自己")
	}
	sec := find(roots, "第一章 文学理论")
	if _, err := f.bank.Merge(ctx, uid, yijing.ID, sec.ID); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("不同层级不能合并")
	}

	// 拆分：典型 → 两个知识点，都关联原来的题
	parts, err := f.bank.Split(ctx, uid, dianxing.ID, []bank.SplitPart{{Name: "典型人物"}, {Name: "典型环境"}})
	if err != nil || len(parts) != 2 || len(parts[0].Related) != 1 || len(parts[1].Related) != 1 {
		t.Fatalf("拆分：%v %+v", err, parts)
	}
	if _, err := f.bank.Split(ctx, uid, parts[0].ID, []bank.SplitPart{{Name: "x"}}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("至少拆成两个")
	}

	// 新建与调整归属：层级规则
	ch, err := f.bank.CreateNode(ctx, uid, sid, bank.NodeInput{Level: "chapter", Name: "新章节", ParentID: &sec.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.bank.CreateNode(ctx, uid, sid, bank.NodeInput{Level: "chapter", Name: "x", ParentID: &yijing.ID}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("章节只能放在板块下")
	}
	if _, err := f.bank.CreateNode(ctx, uid, sid, bank.NodeInput{Level: "section", Name: "x", ParentID: &sec.ID}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("板块没有上级")
	}
	moved, err := f.bank.UpdateKnowledgePoint(ctx, uid, yijing.ID, bank.KPPatch{ParentID: &ch.ID})
	if err != nil || len(moved.Path) != 2 || moved.Path[1] != "新章节" {
		t.Fatalf("调整归属：%v %+v", err, moved.Path)
	}
	if _, err := f.bank.UpdateKnowledgePoint(ctx, uid, sec.ID, bank.KPPatch{ParentID: &ch.ID}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("不能移到自己下面")
	}

	// 删除知识点：题目保留
	if err := f.bank.DeleteKnowledgePoint(ctx, uid, parts[0].ID); err != nil {
		t.Fatal(err)
	}
	if o, _ := f.bank.Overview(ctx, uid, sid); o.Questions != 3 {
		t.Fatal("删除知识点不删题")
	}
}

func TestQuestions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	all, facets, next, err := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || next != 0 || facets.ByQType["term"] != 2 || facets.ByQType["single_choice"] != 1 || facets.BySource["exam"] != 3 || facets.Years[0] != 2024 {
		t.Fatalf("列表：%d %+v", len(all), facets)
	}
	if all[0].StatusTag != "unanswered" || len(all[0].Path) == 0 {
		t.Fatalf("状态与章节：%+v", all[0])
	}
	page1, _, next, _ := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{Limit: 2})
	page2, _, _, _ := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{Limit: 2, After: next})
	if len(page1) != 2 || next != 2 || len(page2) != 1 {
		t.Fatal("分页")
	}
	terms, _, _, _ := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{QType: "term"})
	if len(terms) != 2 {
		t.Fatal("按题型筛选")
	}
	var q1 uint64
	for _, q := range all {
		if q.Stem == "意境" {
			q1 = q.ID
		}
	}

	// 作答一次、进错题本 → 状态「错题」，详情里有作答记录与遗漏的采分点
	res, err := f.db.Exec("INSERT INTO attempts (owner_user_id, question_id, answer_mode, answer_text, score, full_score, is_correct) VALUES (?, ?, 'typed', '答', 4, 10, 0)", uid, q1)
	if err != nil {
		t.Fatal(err)
	}
	aid, _ := res.LastInsertId()
	snapshot := `{"version":1,"points":[{"content":"情景交融"}]}`
	if _, err := f.db.Exec(`INSERT INTO gradings (owner_user_id, attempt_id, kind, status, rubric_version, rubric_snapshot, score, full_score, point_results, loss)
		VALUES (?, ?, 'subjective', 'done', 1, ?, 4, 10, '[{"content":"虚实相生","result":"miss"}]', '{"knowledge":{"points":6}}')`, uid, aid, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("INSERT INTO wrong_book (owner_user_id, question_id, added_reason) VALUES (?, ?, 'wrong')", uid, q1); err != nil {
		t.Fatal(err)
	}
	wrong, _, _, _ := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{Status: "wrong"})
	if len(wrong) != 1 || wrong[0].ID != q1 || wrong[0].AttemptCount != 1 || *wrong[0].LastScore != 4 {
		t.Fatalf("错题筛选：%+v", wrong)
	}
	d, err := f.bank.Question(ctx, uid, q1)
	if err != nil {
		t.Fatal(err)
	}
	if !d.InWrongBook || len(d.Attempts) != 1 || d.Attempts[0].Missed[0] != "虚实相生" || d.Attempts[0].LossTypes[0] != "knowledge" || d.SourceRef == nil || d.RubricVersion != 1 {
		t.Fatalf("详情：%+v", d)
	}

	// 改采分点：版本加 1，之后按新采分点批改；历史批改的快照不变（PRD 11.14）
	five := 5.0
	bad := []bank.RubricInput{{Content: "情景交融", Score: &five}}
	if _, err := f.bank.UpdateQuestion(ctx, uid, q1, bank.QuestionInput{Rubric: &bad}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("采分点合计应等于分值：%v", err)
	}
	good := []bank.RubricInput{{Content: "情景交融", Score: &five}, {Content: "虚实相生", Score: &five}}
	no := false
	d, err = f.bank.UpdateQuestion(ctx, uid, q1, bank.QuestionInput{Rubric: &good, NeedsReview: &no})
	if err != nil || d.RubricVersion != 2 || len(d.Rubric) != 2 || d.Rubric[0].Origin != "user_confirmed" {
		t.Fatalf("改采分点：%v %+v", err, d)
	}
	var snap string
	var ver int
	_ = f.db.QueryRow("SELECT rubric_snapshot, rubric_version FROM gradings WHERE attempt_id = ?", aid).Scan(&snap, &ver)
	if !strings.Contains(snap, `"情景交融"`) || ver != 1 {
		t.Fatalf("历史批改不应变：%s %d", snap, ver)
	}

	// 手动加题：计入导入题数；挂到别的题库的知识点 → 400
	roots, _, _ := f.bank.Tree(ctx, uid, sid, "all")
	kp := find(roots, "意象").ID
	qt, stem := "short_answer", "简述意象的特点"
	nd, err := f.bank.CreateQuestion(ctx, uid, sid, bank.QuestionInput{QType: &qt, Stem: &stem, KPIDs: &[]uint64{kp}})
	if err != nil || len(nd.KPs) != 1 || !nd.KPs[0].IsPrimary || nd.Source != "exercise" {
		t.Fatalf("加题：%v %+v", err, nd)
	}
	under, _, _, _ := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{KPID: &kp})
	if len(under) != 1 {
		t.Fatalf("按知识点筛选：%d", len(under))
	}
	other, otherSubject := f.user(t)
	f.imported(t, other, otherSubject)
	otherRoots, _, _ := f.bank.Tree(ctx, other, otherSubject, "all")
	if _, err := f.bank.CreateQuestion(ctx, uid, sid, bank.QuestionInput{QType: &qt, Stem: &stem, KPIDs: &[]uint64{find(otherRoots, "意象").ID}}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("别人的知识点：%v", err)
	}

	// 删除题目：连同作答与错题
	if err := f.bank.DeleteQuestion(ctx, uid, q1); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM attempts WHERE question_id = ?", q1).Scan(&n)
	if n != 0 {
		t.Fatal("作答记录应删除")
	}
}

func TestPageAndSearch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	var mid uint64
	_ = f.db.QueryRow("SELECT id FROM materials WHERE owner_user_id = ? AND file_name = '资料2'", uid).Scan(&mid)

	p, err := f.bank.MaterialPage(ctx, uid, mid, 1, "情景交融、 虚实相生")
	if err != nil {
		t.Fatal(err)
	}
	runes := []rune(p.Text)
	if len(p.Highlights) != 1 || string(runes[p.Highlights[0].Start:p.Highlights[0].End]) != "情景交融、虚实相生" || len(p.KPs) != 2 || p.PageCount != 1 {
		t.Fatalf("原文：%+v", p)
	}
	if _, err := f.bank.MaterialPage(ctx, uid, mid, 2, ""); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("不存在的页")
	}

	r, err := f.bank.Search(ctx, uid, sid, "意境")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.KPs) == 0 || len(r.Questions) != 1 || len(r.Pages) != 2 {
		t.Fatalf("搜索：%d %d %d", len(r.KPs), len(r.Questions), len(r.Pages))
	}
	h := r.Pages[0].Hit
	if string([]rune(h.Text)[h.Highlights[0].Start:h.Highlights[0].End]) != "意境" {
		t.Fatalf("高亮：%+v", h)
	}
	if r, _ = f.bank.Search(ctx, uid, sid, "100%"); len(r.KPs)+len(r.Questions)+len(r.Pages) != 0 {
		t.Fatal("通配符应转义")
	}
	if _, err := f.bank.Search(ctx, uid, sid, " "); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("空搜索")
	}
}

func TestFind(t *testing.T) {
	cases := []struct {
		text, q string
		want    []bank.Range
	}{
		{"意境是情景\n交融", "情景交融", []bank.Range{{Start: 3, End: 8}}},
		{"abcABC", "abc", []bank.Range{{Start: 0, End: 3}, {Start: 3, End: 6}}},
		{"abc", "", []bank.Range{}},
		{"abc", "x", []bank.Range{}},
	}
	for _, c := range cases {
		got := bank.Find(c.text, c.q)
		if len(got) != len(c.want) {
			t.Fatalf("%q %q：%+v", c.text, c.q, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q %q：%+v", c.text, c.q, got)
			}
		}
	}
}

func TestBankOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	other, _ := f.user(t)
	roots, _, _ := f.bank.Tree(ctx, uid, sid, "all")
	kp := notesKP(roots, "意境").ID
	kp2 := notesKP(roots, "意象").ID
	qs, _, _, _ := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{})
	qid := qs[0].ID
	var mid uint64
	_ = f.db.QueryRow("SELECT id FROM materials WHERE owner_user_id = ? LIMIT 1", uid).Scan(&mid)
	name := "x"
	qt, stem := "term", "x"

	checks := map[string]func() error{
		"overview": func() error { _, err := f.bank.Overview(ctx, other, sid); return err },
		"tree":     func() error { _, _, err := f.bank.Tree(ctx, other, sid, "all"); return err },
		"create": func() error {
			_, err := f.bank.CreateNode(ctx, other, sid, bank.NodeInput{Level: "section", Name: "x"})
			return err
		},
		"kp": func() error { _, err := f.bank.KnowledgePoint(ctx, other, kp); return err },
		"update kp": func() error {
			_, err := f.bank.UpdateKnowledgePoint(ctx, other, kp, bank.KPPatch{Name: &name})
			return err
		},
		"delete kp": func() error { return f.bank.DeleteKnowledgePoint(ctx, other, kp) },
		"merge":     func() error { _, err := f.bank.Merge(ctx, other, kp2, kp); return err },
		"split": func() error {
			_, err := f.bank.Split(ctx, other, kp, []bank.SplitPart{{Name: "a"}, {Name: "b"}})
			return err
		},
		"regenerate": func() error { _, err := f.bank.RegenerateExplanation(ctx, other, kp); return err },
		"assess":     func() error { _, err := f.bank.SelfAssess(ctx, other, kp, "vague"); return err },
		"questions":  func() error { _, _, _, err := f.bank.Questions(ctx, other, sid, bank.QuestionFilter{}); return err },
		"question":   func() error { _, err := f.bank.Question(ctx, other, qid); return err },
		"create q": func() error {
			_, err := f.bank.CreateQuestion(ctx, other, sid, bank.QuestionInput{QType: &qt, Stem: &stem})
			return err
		},
		"update q": func() error {
			_, err := f.bank.UpdateQuestion(ctx, other, qid, bank.QuestionInput{Stem: &stem})
			return err
		},
		"delete q": func() error { return f.bank.DeleteQuestion(ctx, other, qid) },
		"page":     func() error { _, err := f.bank.MaterialPage(ctx, other, mid, 1, ""); return err },
		"search":   func() error { _, err := f.bank.Search(ctx, other, sid, "意境"); return err },
	}
	for name, fn := range checks {
		if err := fn(); !apperr.IsKind(err, apperr.NotFound) {
			t.Errorf("%s：拿别人的 ID 应返回 404，got %v", name, err)
		}
	}
	if _, err := f.bank.Question(ctx, uid, qid); err != nil {
		t.Fatal("别人的操作不应影响我的题")
	}
}
