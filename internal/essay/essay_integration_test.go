package essay_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/essay"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/params"
	"peetraining-server/internal/plan"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/score"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type fx struct {
	db    *sql.DB
	es    *essay.Service
	sc    *score.Service
	prof  *profile.Service
	clock time.Time
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
	// 北京时间 2026-10-05（周一）10:00。
	f := &fx{db: db, clock: time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC)}
	now := func() time.Time { return f.clock }
	q := dbq.New(db)
	ps := params.New(q)
	qs := quota.New(q, ps, now)
	engine := ai.NewEngine(ai.Config{Queries: q, UseMock: true})
	f.prof = profile.New(db, ps, oss.NewMock(), now)
	bk := bank.New(bank.Deps{DB: db, AI: engine, Quota: qs, Params: ps, Now: now})
	pl := plan.New(plan.Deps{DB: db, Params: ps, Profile: f.prof, Bank: bk, Now: now})
	f.sc = score.New(score.Deps{DB: db, Params: ps, Profile: f.prof, Bank: bk, Plan: pl, Now: now})
	f.es = essay.New(essay.Deps{DB: db, Params: ps, AI: engine, Quota: qs, Score: f.sc, Now: now})
	return f
}

var phone = 0

func (f *fx) user(t *testing.T) (uid, sid, bankID uint64) {
	t.Helper()
	phone++
	p := "1370000" + strconv.Itoa(1000+phone)
	res, err := f.db.Exec("INSERT INTO users (phone, invite_code) VALUES (?, ?)", p, p[3:])
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	ctx := context.Background()
	if _, err := f.prof.Upsert(ctx, uint64(id), profile.Input{ExamYear: 2027, Stage: rules.Strengthen, DailyMinutes: 45}); err != nil {
		t.Fatal(err)
	}
	s, err := f.prof.CreateSubject(ctx, uint64(id), profile.SubjectInput{Name: "写作", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE subjects SET is_essay = 1 WHERE id = ?", s.ID); err != nil {
		t.Fatal(err)
	}
	return uint64(id), s.ID, s.BankID
}

func kind(err error) apperr.Kind {
	var e *apperr.Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return -1
}

var key = 0

func nextKey() string {
	key++
	return "essay-" + strconv.Itoa(key)
}

// body 是一篇约 n 段、每段约 60 字的作文。
func body(n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("守正与创新相辅相成。守正是根基，创新是动力；只有守住根本，创新才不会偏离方向，这一段继续展开论证第" + strconv.Itoa(i+1) + "个分论点。")
	}
	return b.String()
}

func (f *fx) write(t *testing.T, uid uint64, in essay.CreateInput, content string) essay.View {
	t.Helper()
	ctx := context.Background()
	in.Key = nextKey()
	v, err := f.es.Create(ctx, uid, in)
	if err != nil {
		t.Fatal(err)
	}
	if v, err = f.es.SaveDraft(ctx, uid, v.ID, essay.DraftInput{Content: &content}); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestEssayWithoutMaterial(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, _ := f.user(t)

	h, err := f.es.Home(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !h.NoMaterial || h.Rubric.Source != "generic" || h.WeeklyGoal != 2 || h.WeeklyRemaining == nil || *h.WeeklyRemaining != 1 {
		t.Fatalf("没有作文资料（5.1b）：%+v", h)
	}
	// AI 命题：标「AI 出题」，计 AI 出题额度；再出一道不重复。
	t1, err := f.es.GenerateTopic(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	t2, err := f.es.GenerateTopic(ctx, uid, sid)
	if err != nil || t2.Topic == t1.Topic || t1.RequiredWords != 800 {
		t.Fatalf("再出一道：%+v %+v %v", t1, t2, err)
	}
	var aiUsed int
	_ = f.db.QueryRow("SELECT COALESCE(SUM(used), 0) FROM quota_counters WHERE owner_user_id = ? AND quota_type = 'ai_questions'", uid).Scan(&aiUsed)
	if aiUsed != 2 {
		t.Errorf("AI 命题计 AI 出题额度：%d", aiUsed)
	}

	v := f.write(t, uid, essay.CreateInput{SubjectID: sid, Source: "ai", AITopicID: t1.ID}, "太短了")
	if v.Topic != t1.Topic || v.Timed || v.Status != "draft" || v.TimeLimitMinutes != 60 {
		t.Fatalf("AI 命题写作：%+v", v)
	}
	if _, err := f.es.Submit(ctx, uid, v.ID); kind(err) != apperr.BadRequest {
		t.Errorf("少于 50 字不批改：%v", err)
	}
	content := body(5)
	if _, err := f.es.SaveDraft(ctx, uid, v.ID, essay.DraftInput{Content: &content}); err != nil {
		t.Fatal(err)
	}
	// 测试里没有队列，提交后同步批改。
	v, err = f.es.Submit(ctx, uid, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "graded" || v.Score == nil || v.FullScore != 150 || len(v.Dimensions) != 5 || v.Rubric == nil || v.Rubric.Source != "generic" {
		t.Fatalf("按通用五维度批改：%+v", v)
	}
	if v.CountsForEstimate || len(v.Annotations) == 0 || len(v.Suggestions) == 0 || v.Weakest == "" || len(v.Paragraphs) != 5 {
		t.Errorf("通用标准只作参考、不计入预估分；总评与逐段批注：%+v", v)
	}
	if _, err := f.es.SaveDraft(ctx, uid, v.ID, essay.DraftInput{Content: &content}); kind(err) != apperr.Conflict {
		t.Errorf("提交后不能再改：%v", err)
	}
	if again, err := f.es.Submit(ctx, uid, v.ID); err != nil || *again.Score != *v.Score {
		t.Errorf("重复提交返回同一结果：%v", err)
	}
	var msgs int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM messages WHERE owner_user_id = ? AND mtype = 'essay_graded'", uid).Scan(&msgs)
	if msgs != 1 {
		t.Errorf("批改完成发消息：%d", msgs)
	}

	// 免费版每周 1 篇：第二篇提交提示次数用完，草稿保留。
	w := f.write(t, uid, essay.CreateInput{SubjectID: sid, Source: "custom", TopicText: "以「慢」为题写一篇文章"}, body(4))
	if _, err := f.es.Submit(ctx, uid, w.ID); kind(err) != apperr.QuotaExceeded {
		t.Errorf("每周 1 篇：%v", err)
	}
	if cur, _ := f.es.Get(ctx, uid, w.ID); cur.Status != "draft" || cur.Content == "" {
		t.Errorf("草稿保留：%+v", cur.Status)
	}

	// 复核：重批一次，不计次；只能复核一次。
	d, err := f.es.Dispute(ctx, uid, v.ID, "score_unfair", "立意应该更高")
	if err != nil || !d.Disputed || d.ScoreBefore == nil || d.Status != "graded" {
		t.Fatalf("复核：%+v %v", d, err)
	}
	if _, err := f.es.Dispute(ctx, uid, v.ID, "other", ""); kind(err) != apperr.Conflict {
		t.Errorf("只能复核一次：%v", err)
	}
	var used int
	_ = f.db.QueryRow("SELECT COALESCE(SUM(used), 0) FROM quota_counters WHERE owner_user_id = ? AND quota_type = 'essay_grading'", uid).Scan(&used)
	if used != 1 {
		t.Errorf("复核不计次：%d", used)
	}

	// 按建议重写：记为第 2 稿。
	r := f.write(t, uid, essay.CreateInput{ParentID: v.ID}, body(6))
	if r.DraftNo != 2 || r.Topic != v.Topic || r.ParentID != v.ID {
		t.Errorf("第 2 稿：%+v", r)
	}

	h, _ = f.es.Home(ctx, uid, sid)
	if h.WeekDone != 1 || h.AvgScore == nil || len(h.AITopics) != 2 || len(h.Drafts) != 2 || *h.WeeklyRemaining != 0 {
		t.Errorf("作文训练：%+v", h)
	}
	nb, err := f.es.Notebook(ctx, uid, sid)
	if err != nil || nb.Count != 1 || len(nb.Trend) != 1 || nb.Weakest == nil || len(nb.DimAvgs) != 5 || len(nb.Items) != 3 {
		t.Errorf("作文本：%+v %v", nb, err)
	}
	cards, _ := f.sc.Cards(ctx, uid)
	if cards[0].Ready {
		t.Error("通用标准的作文不计入预估分")
	}

	// 别人的作文、题目、评分标准返回 404。
	other, osid, _ := f.user(t)
	if _, err := f.es.Get(ctx, other, v.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的作文：%v", err)
	}
	if _, err := f.es.Create(ctx, other, essay.CreateInput{SubjectID: osid, Source: "ai", AITopicID: t1.ID, Key: nextKey()}); kind(err) != apperr.NotFound {
		t.Errorf("别人的 AI 命题：%v", err)
	}
	if _, err := f.es.Create(ctx, other, essay.CreateInput{ParentID: v.ID, Key: nextKey()}); kind(err) != apperr.NotFound {
		t.Errorf("重写别人的作文：%v", err)
	}
	if _, err := f.es.Rubrics(ctx, other, sid); kind(err) != apperr.NotFound {
		t.Errorf("别人的评分标准：%v", err)
	}
	if _, err := f.es.Notebook(ctx, other, sid); kind(err) != apperr.NotFound {
		t.Errorf("别人的作文本：%v", err)
	}
	if _, err := f.es.Dispute(ctx, other, v.ID, "other", ""); kind(err) != apperr.NotFound {
		t.Errorf("复核别人的作文：%v", err)
	}
}

func TestEssayUserRubricAndEstimate(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid, bankID := f.user(t)
	res, err := f.db.Exec(`INSERT INTO questions (owner_user_id, bank_id, qtype, stem, source, exam_year, required_words, content_hash)
		VALUES (?, ?, 'essay', '以「守正与创新」为题，写一篇不少于 800 字的议论文', 'exam', 2024, 800, SHA2('t', 256))`, uid, bankID)
	if err != nil {
		t.Fatal(err)
	}
	qid, _ := res.LastInsertId()
	if _, err := f.db.Exec(`INSERT INTO model_essays (owner_user_id, bank_id, topic_question_id, title, content, structure)
		VALUES (?, ?, ?, '守正出新', '范文正文', JSON_OBJECT('opening', '开门见山', 'points', JSON_ARRAY('守正', '创新'), 'ending', '回扣题目'))`, uid, bankID, qid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO essay_rubrics (owner_user_id, subject_id, source, name, full_score, dimensions, is_active, origin)
		VALUES (?, ?, 'user_material', '学校评分细则', 100, JSON_ARRAY(JSON_OBJECT('name', '内容', 'score', 60), JSON_OBJECT('name', '表达', 'score', 40)), 1, 'ai_extracted')`, uid, sid); err != nil {
		t.Fatal(err)
	}
	rs, err := f.es.Rubrics(ctx, uid, sid)
	if err != nil || rs.User == nil || rs.ActiveID != rs.User.ID || rs.Generic.FullScore != 150 {
		t.Fatalf("你的资料优先：%+v %v", rs, err)
	}
	h, _ := f.es.Home(ctx, uid, sid)
	if h.NoMaterial || len(h.ExamTopics) != 1 || h.ExamTopics[0].ModelEssayCount != 1 {
		t.Fatalf("真题题目：%+v", h)
	}

	// 真题、限时、按用户评分细则批改：计入作文课预估分；结果页有范文对比。
	v := f.write(t, uid, essay.CreateInput{SubjectID: sid, Source: "exam", QuestionID: uint64(qid)}, body(5))
	if !v.Timed || v.RequiredWords != 800 {
		t.Fatalf("真题默认限时：%+v", v)
	}
	v, err = f.es.Submit(ctx, uid, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.CountsForEstimate || v.FullScore != 100 || len(v.Dimensions) != 2 || len(v.ModelEssays) != 1 || v.Rubric.Name != "学校评分细则" {
		t.Fatalf("按用户细则批改：%+v", v)
	}
	cards, _ := f.sc.Cards(ctx, uid)
	if c := cards[0]; !c.Ready || c.BasisPapers != 1 || c.Low != int(*v.Score*0.9+0.5) || c.MainGapDimension == "" {
		t.Errorf("作文课预估分（1 篇 ±10%%）：%+v score=%v", c, *v.Score)
	}

	// 改评分标准：之后的作文按新标准，已批改的分数不变（PRD 5.9）。
	if _, err := f.es.UpdateRubric(ctx, uid, sid, "改过的细则", []ai.EssayDim{{Name: "立意", Score: 50}, {Name: "论证", Score: 50}, {Name: "语言", Score: 50}}); err != nil {
		t.Fatal(err)
	}
	old, _ := f.es.Get(ctx, uid, v.ID)
	if *old.Score != *v.Score || old.Rubric.Name != "学校评分细则" || len(old.Dimensions) != 2 {
		t.Errorf("旧分数不变：%+v", old)
	}
	f.clock = f.clock.Add(7 * 24 * time.Hour) // 下一周，免费次数恢复
	n := f.write(t, uid, essay.CreateInput{SubjectID: sid, Source: "exam", QuestionID: uint64(qid)}, body(6))
	if n.DraftNo != 2 {
		t.Errorf("同一道真题再写记为第 2 稿：%d", n.DraftNo)
	}
	if n, err = f.es.Submit(ctx, uid, n.ID); err != nil || n.FullScore != 150 || len(n.Dimensions) != 3 || n.Rubric.Name != "改过的细则" {
		t.Fatalf("新作文按新标准：%+v %v", n, err)
	}

	// 切到通用标准：之后的作文按通用五维度批改，不计入预估分；不能改通用标准。
	if rs, err = f.es.SelectRubric(ctx, uid, sid, "generic"); err != nil || rs.ActiveID != rs.Generic.ID {
		t.Fatalf("切换到通用：%+v %v", rs, err)
	}
	f.clock = f.clock.Add(7 * 24 * time.Hour)
	g := f.write(t, uid, essay.CreateInput{SubjectID: sid, Source: "exam", QuestionID: uint64(qid)}, body(5))
	if g, err = f.es.Submit(ctx, uid, g.ID); err != nil || g.Rubric.Source != "generic" || g.CountsForEstimate {
		t.Errorf("通用标准不计入：%+v %v", g, err)
	}
	if rs, err = f.es.SelectRubric(ctx, uid, sid, "user_material"); err != nil || rs.ActiveID != rs.User.ID {
		t.Errorf("切回你的资料：%+v %v", rs, err)
	}
	cards, _ = f.sc.Cards(ctx, uid)
	if c := cards[0]; c.BasisPapers != 2 {
		t.Errorf("预估分依据 2 篇（通用标准那篇不算）：%+v", c)
	}
	d, err := f.sc.Dashboard(ctx, uid, sid)
	if err != nil || len(d.EssayDims) == 0 {
		t.Errorf("看板显示作文各维度平均分：%+v %v", d.EssayDims, err)
	}

	m, err := f.es.ModelEssay(ctx, uid, v.ModelEssays[0].ID)
	if err != nil || m.Title != "守正出新" || len(m.Structure) == 0 {
		t.Errorf("范文详情：%+v %v", m, err)
	}
	other, osid, _ := f.user(t)
	if _, err := f.es.ModelEssay(ctx, other, m.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的范文：%v", err)
	}
	if _, err := f.es.Create(ctx, other, essay.CreateInput{SubjectID: osid, Source: "exam", QuestionID: uint64(qid), Key: nextKey()}); kind(err) != apperr.NotFound {
		t.Errorf("别人的真题：%v", err)
	}
	if _, err := f.es.UpdateRubric(ctx, other, osid, "x", []ai.EssayDim{{Name: "a", Score: 10}}); kind(err) != apperr.Conflict {
		t.Errorf("没有自己的细则时不能改通用标准：%v", err)
	}
}
