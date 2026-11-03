package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/practice"
)

// publishOfficial 立项并发布一个只有两道名词解释真题的官方题库，返回项目与题库 id。
func publishOfficial(t *testing.T, f *fx, s *official.Service, school, code, name string) (uint64, uint64) {
	t.Helper()
	ctx := context.Background()
	editor, lead := adminUser(t, f, "editor-"+code), adminUser(t, f, "lead-"+code)
	pid, err := s.CreateProject(ctx, lead, school, "哲学", code, name, []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	rub := []official.Point{{Content: "要点一", Score: 3}, {Content: "要点二", Score: 2}}
	kpA, err := s.SaveDraft(ctx, editor, pid, 0, "knowledge_point", "add", 0, payload(official.KPPayload{Section: "近代哲学", Chapter: "康德", Name: "绝对命令", OriginalText: "绝对命令：无条件的道德律令。", Rubric: rub}))
	if err != nil {
		t.Fatal(err)
	}
	kpB, err := s.SaveDraft(ctx, editor, pid, 0, "knowledge_point", "add", 0, payload(official.KPPayload{Section: "近代哲学", Chapter: "康德", Name: "先验统觉", OriginalText: "先验统觉：自我意识的统一。", Rubric: rub}))
	if err != nil {
		t.Fatal(err)
	}
	sc := 5.0
	y1, y2, y3 := 2023, 2024, 2022
	// 「绝对命令」考过两次（高频），「先验统觉」只考过一次但年份更近：应先出高频的。
	qs := []official.QuestionPayload{
		{QType: "term", Stem: "名词解释：先验统觉", Answer: "自我意识的统一。", Score: &sc, ExamYear: &y2, KPNames: []string{"先验统觉"}, Rubric: rub},
		{QType: "term", Stem: "名词解释：绝对命令", Answer: "无条件的道德律令。", Score: &sc, ExamYear: &y1, KPNames: []string{"绝对命令"}, Rubric: rub},
		{QType: "short_answer", Stem: "简述绝对命令的三种表述", Answer: "普遍法则、人是目的、意志自律。", Score: &sc, ExamYear: &y3, KPNames: []string{"绝对命令"}, Rubric: rub},
	}
	drafts := []uint64{kpA.ID, kpB.ID}
	for _, q := range qs {
		d, err := s.SaveDraft(ctx, editor, pid, 0, "question", "add", 0, payload(q))
		if err != nil {
			t.Fatal(err)
		}
		drafts = append(drafts, d.ID)
	}
	approveAll(t, s, editor, lead, pid, drafts...)
	if _, err := s.Publish(ctx, lead, pid, ""); err != nil {
		t.Fatal(err)
	}
	p, err := s.Project(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	return pid, p.BankID
}

// TestOfficialFirstRoute 是 T44 的验收：院校专业课库 → 1.1 匹配到官方题库 → 添加 → 1.1b 首题批改不扣额度；
// 手动添加的课进待入库，入库后上线通知能找到选了这门课的用户。
func TestOfficialFirstRoute(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})

	// 迁移已录入四川大学哲学两门课；还没立项时都是「暂无」。
	if got, err := s.SearchSchools(ctx, "四川"); err != nil || len(got) != 1 || got[0].CourseCount != 2 {
		t.Fatalf("搜索院校：%+v %v", got, err)
	}
	uid, sid := f.user(t)
	cs, err := s.Courses(ctx, uid, "四川大学", 2027, true)
	if err != nil || len(cs) != 2 || cs[0].OfficialStatus != official.OfficialNone {
		t.Fatalf("课程列表：%+v %v", cs, err)
	}
	// 立项后是「生产中」，发布后是「有」并带摘要；开关关闭时一律按「暂无」。
	pid, bankID := publishOfficial(t, f, s, "四川大学", "812", "西方哲学史")
	cs, _ = s.Courses(ctx, uid, "四川大学", 2027, true)
	var west official.CatalogCourse
	for _, c := range cs {
		if c.SubjectCode == "812" {
			west = c
		}
	}
	if west.OfficialStatus != official.OfficialAvailable || west.Official == nil || west.Official.BankID != bankID ||
		west.Official.ExamYearCount != 3 || west.Official.KPCount != 2 || west.Official.QuestionCount != 3 {
		t.Fatalf("1.1a 摘要：%+v %+v", west, west.Official)
	}
	if cs, _ := s.Courses(ctx, uid, "四川大学", 2027, false); cs[0].OfficialStatus != official.OfficialNone || cs[1].OfficialStatus != official.OfficialNone {
		t.Errorf("官方题库开关关闭时都按暂无：%+v", cs)
	}
	_ = pid

	// 用户在 1.1 选了这门课：关联到库里的课。
	if err := s.AttachSubject(ctx, uid, sid, official.CourseChoice{CatalogCourseID: west.ID}); err != nil {
		t.Fatal(err)
	}
	var school string
	if err := f.db.QueryRow("SELECT school FROM subjects WHERE id = ?", sid).Scan(&school); err != nil || school != "四川大学" {
		t.Fatalf("专业课记下院校：%q %v", school, err)
	}

	// 没添加官方题库前不能取首题；添加后取到高频考点的名词解释真题。
	if _, err := f.pr.CreateOfficialFirst(ctx, uid, bankID); kind(err) != apperr.NotFound {
		t.Errorf("没添加官方题库：%v", err)
	}
	if err := s.Subscribe(ctx, uid, bankID, sid); err != nil {
		t.Fatal(err)
	}
	ses, err := f.pr.CreateOfficialFirst(ctx, uid, bankID)
	if err != nil {
		t.Fatal(err)
	}
	if ses.Kind != practice.KindOfficialFirst || len(ses.Questions) != 1 || ses.Questions[0].Stem != "名词解释：绝对命令" {
		t.Fatalf("首题：%+v", ses)
	}
	again, err := f.pr.CreateOfficialFirst(ctx, uid, bankID)
	if err != nil || again.ID != ses.ID {
		t.Errorf("每个账号只有一组首题：%d %d %v", again.ID, ses.ID, err)
	}
	// 别人拿不到这一组。
	other, _ := f.user(t)
	if _, err := f.pr.Get(ctx, other, ses.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的首题 404：%v", err)
	}

	// 把今天的批改次数用完，首题照样批改，而且不扣额度。
	if _, err := f.db.Exec("INSERT INTO quota_counters (owner_user_id, quota_type, period_key, used) VALUES (?, 'grading', ?, 3)", uid, f.clock.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	g, err := f.pr.SubmitSubjective(ctx, uid, ses.ID, practice.SubjectiveInput{QuestionID: ses.Questions[0].ID, Key: "official-first-1", Answer: "绝对命令是无条件的道德律令。"})
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != "done" {
		t.Fatalf("首题应直接批改：%+v", g)
	}
	var charged bool
	if err := f.db.QueryRow("SELECT quota_charged FROM gradings WHERE id = ?", g.ID).Scan(&charged); err != nil || charged {
		t.Errorf("首题批改不扣额度：%v %v", charged, err)
	}
	var ledger int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM quota_ledger WHERE owner_user_id = ? AND quota_type = 'grading'", uid).Scan(&ledger)
	if ledger != 0 {
		t.Errorf("额度台账不应有批改记录：%d", ledger)
	}

	// 手动添加库里没有的课：同一门课多人添加只算人数，同一人只算一次；入库后关联上。
	u2, s2 := f.user(t)
	u3, s3 := f.user(t)
	for _, c := range []struct{ u, s uint64 }{{u2, s2}, {u3, s3}, {u3, s3}} {
		if err := s.AttachSubject(ctx, c.u, c.s, official.CourseChoice{School: "南京大学", Major: "中国语言文学", Name: "文学理论", Code: "831"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec("UPDATE subjects SET code = '831' WHERE id IN (?, ?)", s2, s3); err != nil {
		t.Fatal(err)
	}
	pend, err := s.PendingCourses(ctx)
	if err != nil || len(pend) != 1 || pend[0].RequestCount != 2 {
		t.Fatalf("待入库：%+v %v", pend, err)
	}
	cid, err := s.ResolvePending(ctx, 0, pend[0].ID, "add", 0, official.CatalogInput{Discipline: "文学"})
	if err != nil || cid == 0 {
		t.Fatalf("入库：%d %v", cid, err)
	}
	var linked int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM subjects WHERE catalog_course_id = ?", cid).Scan(&linked)
	if linked != 2 {
		t.Errorf("入库后关联手动添加的用户：%d", linked)
	}
	if _, err := s.ResolvePending(ctx, 0, pend[0].ID, "ignore", 0, official.CatalogInput{}); kind(err) != apperr.Conflict {
		t.Errorf("重复处理：%v", err)
	}
	if _, err := s.CreateCatalogCourse(ctx, 0, official.CatalogInput{School: "南京大学", Major: "中国语言文学", SubjectName: "文学理论"}); kind(err) != apperr.Conflict {
		t.Errorf("同名课程：%v", err)
	}

	// 南京大学文学理论首次上线：在 1.1 选了它的两个人收到通知。
	publishOfficial(t, f, s, "南京大学", "831", "文学理论")
	if n := countMsgs(t, f, u2, "official_bank"); n != 1 {
		t.Errorf("选了这门课的用户收到上线通知：%d", n)
	}
}
