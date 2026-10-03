package practice_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/cloud/asr"
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
	"peetraining-server/internal/plan"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/score"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type inlineQueue struct{ h *jobs.Handlers }

func (q *inlineQueue) EnqueueContext(ctx context.Context, t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{}, q.h.Mux().ProcessTask(ctx, t)
}

type fx struct {
	db    *sql.DB
	imp   *importer.Service
	mat   *material.Service
	prof  *profile.Service
	pr    *practice.Service
	oss   *oss.Mock
	asr   *asr.Mock
	flags *flags.Service
	plan  *plan.Service
	sc    *score.Service
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
	// 北京时间 2026-10-02 10:00，测试里可以往后拨。
	f := &fx{db: db, clock: time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)}
	now := func() time.Time { return f.clock }
	q := dbq.New(db)
	ps := params.New(q)
	o := oss.NewMock()
	qs := quota.New(q, ps, now)
	f.prof = profile.New(db, ps, o, now)
	f.mat = material.New(material.Deps{DB: db, OSS: o, Moderation: moderation.NewMock(), Quota: qs, Params: ps, OCR: ocr.NewMock(), PDF: ocr.NewMock(), Flags: flags.New(q), Now: now})
	engine := ai.NewEngine(ai.Config{Queries: q, UseMock: true})
	queue := &inlineQueue{}
	f.imp = importer.New(importer.Deps{DB: db, Material: f.mat, Quota: qs, AI: engine, Queue: queue, Now: now})
	queue.h = &jobs.Handlers{Logger: logx.New(io.Discard, slog.LevelInfo), Import: f.imp, Material: f.mat, Permanent: material.IsPermanent}
	bk := bank.New(bank.Deps{DB: db, AI: engine, Quota: qs, Params: ps, Now: now})
	pl := plan.New(plan.Deps{DB: db, Params: ps, Profile: f.prof, Bank: bk, Now: now})
	f.oss, f.asr, f.flags, f.plan = o, asr.NewMock(), flags.New(q), pl
	f.sc = score.New(score.Deps{DB: db, Params: ps, Profile: f.prof, Bank: bk, Plan: pl, Now: now})
	f.mat.OnDeleted = func(ctx context.Context, userID, subjectID uint64) {
		_ = f.sc.Recompute(ctx, userID, subjectID, "material_deleted")
	}
	f.pr = practice.New(practice.Deps{DB: db, Params: ps, Plan: pl, AI: engine, Quota: qs, OSS: o, OCR: ocr.NewMock(), Moderation: moderation.NewMock(),
		ASR: f.asr, Flags: f.flags, Queue: queue, Score: f.sc, Now: now})
	queue.h.Paper = f.pr
	return f
}

var phone = 0

func (f *fx) user(t *testing.T) (uint64, uint64) {
	t.Helper()
	phone++
	p := "1390000" + strconv.Itoa(1000+phone)
	res, err := f.db.Exec("INSERT INTO users (phone, invite_code) VALUES (?, ?)", p, p[3:])
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	ctx := context.Background()
	if _, err := f.prof.Upsert(ctx, uint64(id), profile.Input{ExamYear: 2027, Stage: rules.Strengthen, DailyMinutes: 45}); err != nil {
		t.Fatal(err)
	}
	s, err := f.prof.CreateSubject(ctx, uint64(id), profile.SubjectInput{Name: "中国语言文学基础", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id), s.ID
}

const exam = `2024 年某某大学中国语言文学基础考研真题
一、单项选择题（每题 2 分）
1. 下列属于唐传奇的是
A. 莺莺传
B. 搜神记
答案：A
2. 《文心雕龙》的作者是
A. 钟嵘
B. 刘勰
答案：B
二、名词解释（每题 10 分）
3. 意境
答案：意境是情景交融、虚实相生的艺术境界。
4. 典型
答案：典型是共性与个性的统一。`

const notes = `第一章 文学理论
一、审美范畴
意象：融入主观情意的客观物象。
意境：情景交融、虚实相生的艺术境界。
典型：共性与个性的统一。`

func (f *fx) imported(t *testing.T, uid, sid uint64) {
	t.Helper()
	ctx := context.Background()
	for i, c := range []struct{ mode, text string }{{"question", exam}, {"reference", notes}} {
		m, err := f.mat.CreatePasted(ctx, uid, sid, c.mode, "资料"+strconv.Itoa(i+1), c.text, true)
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

func choice(s practice.Session, stemPrefix string) practice.Question {
	for _, q := range s.Questions {
		if q.QType == "single_choice" && len(q.Stem) >= len(stemPrefix) && q.Stem[:len(stemPrefix)] == stemPrefix {
			return q
		}
	}
	return practice.Question{}
}

var key = 0

func nextKey() string {
	key++
	return "key-" + strconv.Itoa(100000+key)
}

func TestObjectiveWrongBookAndMastery(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	s, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindTypeDrill, QType: "single_choice"})
	if err != nil {
		t.Fatal(err)
	}
	q := choice(s, "下列属于唐传奇")
	if q.ID == 0 || q.Answer != "A" || len(q.Options) != 2 {
		t.Fatalf("会话里应有带答案的客观题（供离线判分）：%+v", s.Questions)
	}

	// 第一天答错：收录错题本，掌握分 −15 截断到 0，次日复习。
	k1 := nextKey()
	r, err := f.pr.Submit(ctx, uid, s.ID, practice.AttemptInput{QuestionID: q.ID, Key: k1, Selected: []string{"B"}, Duration: 30})
	if err != nil {
		t.Fatal(err)
	}
	if r.IsCorrect == nil || *r.IsCorrect || r.WrongBook != "added" || r.Answer != "A" {
		t.Fatalf("答错：%+v", r)
	}
	if len(r.KPs) == 0 || r.KPs[0].To != rules.StateConsolidating {
		t.Errorf("答错后知识点应为待巩固：%+v", r.KPs)
	}
	// 同一个幂等键重复提交：返回同一条作答，不重复收录。
	again, err := f.pr.Submit(ctx, uid, s.ID, practice.AttemptInput{QuestionID: q.ID, Key: k1, Selected: []string{"B"}, Duration: 30})
	if err != nil || again.AttemptID != r.AttemptID {
		t.Fatalf("幂等：%+v %v", again, err)
	}
	var n int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM attempts WHERE owner_user_id = ?", uid).Scan(&n)
	if n != 1 {
		t.Errorf("重复提交不应多写作答：%d", n)
	}
	wb, err := f.pr.WrongBook(ctx, uid, sid)
	if err != nil || wb.Total != 1 || wb.WeekNew != 1 {
		t.Fatalf("错题本：%+v %v", wb, err)
	}

	// 第二天答对一次：仍在错题本；第三天再答对：两个不同日期连续答对，自动移出（已消灭）。
	for day, want := range []string{"still", "removed"} {
		f.clock = f.clock.Add(24 * time.Hour)
		redo, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindWrongRedo})
		if err != nil {
			t.Fatal(err)
		}
		if len(redo.Questions) != 1 || redo.Questions[0].ID != q.ID {
			t.Fatalf("错题重做应只有这道题：%+v", redo.Questions)
		}
		r, err := f.pr.Submit(ctx, uid, redo.ID, practice.AttemptInput{QuestionID: q.ID, Key: nextKey(), Selected: []string{"A"}, Duration: 20})
		if err != nil {
			t.Fatal(err)
		}
		if r.WrongBook != want {
			t.Errorf("第 %d 次答对：错题本 %s，期望 %s", day+1, r.WrongBook, want)
		}
	}
	wb, _ = f.pr.WrongBook(ctx, uid, sid)
	if wb.Total != 0 || wb.Eliminated != 1 {
		t.Errorf("移出后：%+v", wb)
	}

	// 看答案：收录，掌握分 −10。
	q2 := choice(s, "《文心雕龙》")
	r, err = f.pr.Submit(ctx, uid, s.ID, practice.AttemptInput{QuestionID: q2.ID, Key: nextKey(), Revealed: true})
	if err != nil || r.WrongBook != "added" || r.IsCorrect != nil {
		t.Fatalf("看答案：%+v %v", r, err)
	}

	sum, err := f.pr.Finish(ctx, uid, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Answered != 2 || sum.FromBank != 2 || len(sum.ReviewKPs) == 0 || sum.Comment == "" {
		t.Errorf("本组总结：%+v", sum)
	}
	again2, _ := f.pr.Finish(ctx, uid, s.ID)
	if again2.Comment != sum.Comment {
		t.Error("已结束的会话应返回同一份总结")
	}
}

func TestOfflineAttemptVerifiedByServer(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	s, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindTypeDrill, QType: "single_choice"})
	if err != nil {
		t.Fatal(err)
	}
	q := choice(s, "《文心雕龙》")
	// 飞行模式下前一天答的，联网后补交：按客户端时间记，服务端复核判分（客户端说对了也以服务端为准）。
	at := f.clock.Add(-20 * time.Hour)
	r, err := f.pr.Submit(ctx, uid, s.ID, practice.AttemptInput{QuestionID: q.ID, Key: nextKey(), Selected: []string{"A"}, Offline: true, AnsweredAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	if r.IsCorrect == nil || *r.IsCorrect {
		t.Errorf("服务端复核应判错：%+v", r)
	}
	var offline, verified bool
	var answeredAt time.Time
	if err := f.db.QueryRow("SELECT offline, verified_at IS NOT NULL, answered_at FROM attempts WHERE id = ?", r.AttemptID).Scan(&offline, &verified, &answeredAt); err != nil {
		t.Fatal(err)
	}
	if !offline || !verified || !answeredAt.Equal(at.Truncate(time.Millisecond)) {
		t.Errorf("离线作答：offline=%v verified=%v at=%v", offline, verified, answeredAt)
	}
	// 未来时间不可信，按收到时间记。
	future := f.clock.Add(time.Hour)
	q1 := choice(s, "下列属于唐传奇")
	r2, err := f.pr.Submit(ctx, uid, s.ID, practice.AttemptInput{QuestionID: q1.ID, Key: nextKey(), Selected: []string{"A"}, Offline: true, AnsweredAt: &future})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.db.QueryRow("SELECT answered_at FROM attempts WHERE id = ?", r2.AttemptID).Scan(&answeredAt)
	if !answeredAt.Equal(f.clock) {
		t.Errorf("未来的客户端时间应改用服务端时间：%v", answeredAt)
	}
	got, _ := f.pr.Get(ctx, uid, s.ID)
	if got.DoneCount != 2 || got.CursorIndex < 1 {
		t.Errorf("断点：%+v", got)
	}
}

func TestDailyCustomAIFillAndReport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	d1, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindDaily})
	if err != nil {
		t.Fatal(err)
	}
	if len(d1.Questions) == 0 || d1.Questions[0].PlanGroup == "" {
		t.Fatalf("今日训练取今日计划的题并带分组：%+v", d1.Questions)
	}
	d2, _ := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindDaily})
	if d2.ID != d1.ID {
		t.Error("今日训练当天应复用同一个会话")
	}
	if err := f.pr.SaveProgress(ctx, uid, d1.ID, 1); err != nil {
		t.Fatal(err)
	}
	home, err := f.pr.Home(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if home.TodaySession != d1.ID || home.InProgress == nil || home.InProgress.Done != 1 || home.Total == 0 || home.DrillQType == "" {
		t.Errorf("训练首页：%+v", home)
	}

	pv, err := f.pr.Preview(ctx, uid, sid, practice.Config{QTypes: []string{"single_choice"}, Count: 5, AIFill: true})
	if err != nil || pv.Available != 2 || pv.Count != 2 || pv.AIFill != 3 || pv.Minutes <= 0 {
		t.Fatalf("预览：%+v %v", pv, err)
	}
	s, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindCustom, Config: practice.Config{QTypes: []string{"single_choice"}, Count: 5, AIFill: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Questions) != 5 || s.AIFilled != 3 {
		t.Fatalf("AI 补题：%d 题，补 %d", len(s.Questions), s.AIFilled)
	}
	var aiq practice.Question
	for _, q := range s.Questions {
		if q.Source == "ai_generated" {
			aiq = q
		}
	}
	if aiq.ID == 0 || len(aiq.KPs) != 1 || aiq.FileName == "" || aiq.Answer == "" {
		t.Errorf("AI 出的题要标出依据的知识点和资料页：%+v", aiq)
	}
	var used int
	_ = f.db.QueryRow("SELECT used FROM quota_counters WHERE owner_user_id = ? AND quota_type = 'ai_questions'", uid).Scan(&used)
	if used != 3 {
		t.Errorf("AI 出题额度应扣 3：%d", used)
	}

	// 「题目有问题」：AI 题报错 3 次自动下线，之后不再出现在练习里。
	for i := 1; i <= 3; i++ {
		off, err := f.pr.Report(ctx, uid, aiq.ID, "答案不对")
		if err != nil {
			t.Fatal(err)
		}
		if off != (i == 3) {
			t.Errorf("第 %d 次报错 offline=%v", i, off)
		}
	}
	got, _ := f.pr.Get(ctx, uid, s.ID)
	for _, q := range got.Questions {
		if q.ID == aiq.ID {
			t.Error("下线的题不应再出现")
		}
	}
	// 题库原题报错不下线。
	if off, _ := f.pr.Report(ctx, uid, s.Questions[0].ID, ""); off && s.Questions[0].Source != "ai_generated" {
		t.Error("题库原题不应因报错下线")
	}

	pm, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindPlacement})
	if err != nil || len(pm.Questions) == 0 || len(pm.Questions) > 20 {
		t.Fatalf("摸底测：%d %v", len(pm.Questions), err)
	}
}

func TestPracticeOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a, sa := f.user(t)
	b, _ := f.user(t)
	f.imported(t, a, sa)
	s, err := f.pr.Create(ctx, a, practice.CreateInput{SubjectID: sa, Kind: practice.KindTypeDrill, QType: "single_choice"})
	if err != nil {
		t.Fatal(err)
	}
	q := s.Questions[0]
	if _, err := f.pr.Submit(ctx, a, s.ID, practice.AttemptInput{QuestionID: q.ID, Key: nextKey(), Selected: []string{"B"}}); err != nil {
		t.Fatal(err)
	}
	notFound := func(name string, err error) {
		t.Helper()
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Kind != apperr.NotFound {
			t.Errorf("%s：拿别人的 ID 应返回 404，got %v", name, err)
		}
	}
	_, err = f.pr.Get(ctx, b, s.ID)
	notFound("会话", err)
	_, err = f.pr.Submit(ctx, b, s.ID, practice.AttemptInput{QuestionID: q.ID, Key: nextKey(), Selected: []string{"A"}})
	notFound("作答", err)
	notFound("断点", f.pr.SaveProgress(ctx, b, s.ID, 1))
	_, err = f.pr.Finish(ctx, b, s.ID)
	notFound("总结", err)
	_, err = f.pr.Create(ctx, b, practice.CreateInput{SubjectID: sa, Kind: practice.KindCustom})
	notFound("开始练习", err)
	_, err = f.pr.Preview(ctx, b, sa, practice.Config{})
	notFound("预览", err)
	_, err = f.pr.Home(ctx, b, sa)
	notFound("训练首页", err)
	_, err = f.pr.WrongBook(ctx, b, sa)
	notFound("错题本", err)
	notFound("移出错题", f.pr.RemoveWrong(ctx, b, q.ID))
	_, err = f.pr.Report(ctx, b, q.ID, "")
	notFound("报错", err)
	// 别人会话里的题不能借自己的会话提交。
	sb, _ := f.user(t)
	_ = sb
	if err := f.pr.RemoveWrong(ctx, a, q.ID); err != nil {
		t.Errorf("自己移出错题：%v", err)
	}
}
