package plan_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
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
	"peetraining-server/internal/plan"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

type inlineQueue struct{ h *jobs.Handlers }

func (q *inlineQueue) EnqueueContext(ctx context.Context, t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{}, q.h.Mux().ProcessTask(ctx, t)
}

type fx struct {
	db   *sql.DB
	imp  *importer.Service
	mat  *material.Service
	prof *profile.Service
	plan *plan.Service
}

// 北京时间 2026-10-02 10:00。
var now = func() time.Time { return time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC) }

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
	q := dbq.New(db)
	ps := params.New(q)
	o := oss.NewMock()
	qs := quota.New(q, ps, now)
	f := &fx{db: db}
	f.prof = profile.New(db, ps, o, now)
	f.mat = material.New(material.Deps{DB: db, OSS: o, Moderation: moderation.NewMock(), Quota: qs, Params: ps, OCR: ocr.NewMock(), PDF: ocr.NewMock(), Flags: flags.New(q), Now: now})
	engine := ai.NewEngine(ai.Config{Queries: q, UseMock: true})
	queue := &inlineQueue{}
	f.imp = importer.New(importer.Deps{DB: db, Material: f.mat, Quota: qs, AI: engine, Queue: queue, Now: now})
	queue.h = &jobs.Handlers{Logger: logx.New(io.Discard, slog.LevelInfo), Import: f.imp, Material: f.mat, Permanent: material.IsPermanent}
	bk := bank.New(bank.Deps{DB: db, AI: engine, Quota: qs, Params: ps, Now: now})
	f.plan = plan.New(plan.Deps{DB: db, Params: ps, Profile: f.prof, Bank: bk, Now: now})
	f.imp.AfterConfirm = func(ctx context.Context, userID, _ uint64) bool { return f.plan.Regenerate(ctx, userID) }
	return f
}

var phone = 0

// user 建一个完成引导的考生：2027 研考（距考试 79 天）、给定阶段、每天 45 分钟、一门专业课。
func (f *fx) user(t *testing.T, stage rules.Stage) (uint64, uint64) {
	t.Helper()
	phone++
	p := "1380000" + pad(phone)
	res, err := f.db.Exec("INSERT INTO users (phone, invite_code) VALUES (?, ?)", p, p[3:])
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	ctx := context.Background()
	if _, err := f.prof.Upsert(ctx, uint64(id), profile.Input{ExamYear: 2027, Stage: stage, DailyMinutes: 45}); err != nil {
		t.Fatal(err)
	}
	s, err := f.prof.CreateSubject(ctx, uint64(id), profile.SubjectInput{Name: "中国语言文学基础", FullScore: 150})
	if err != nil {
		t.Fatal(err)
	}
	return uint64(id), s.ID
}

func pad(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

const exam = `2024 年某某大学中国语言文学基础考研真题
一、名词解释（每题 10 分）
1. 意境
答案：意境是情景交融、虚实相生的艺术境界。
2. 典型
答案：典型是共性与个性的统一。
3. 风骨
答案：风骨是刘勰提出的审美范畴。
二、简答题（每题 20 分）
4. 简述唐传奇的艺术成就。
答案：情节完整，人物鲜明。
三、单项选择题（每题 2 分）
5. 下列属于唐传奇的是
A. 莺莺传
B. 搜神记
答案：A`

const notes = `第一章 文学理论
一、审美范畴
意象：融入主观情意的客观物象。
意境：情景交融、虚实相生的艺术境界。
典型：共性与个性的统一。`

func (f *fx) imported(t *testing.T, uid, sid uint64) importer.ConfirmResult {
	t.Helper()
	ctx := context.Background()
	var last importer.ConfirmResult
	for i, c := range []struct{ mode, text string }{{"question", exam}, {"reference", notes}} {
		m, err := f.mat.CreatePasted(ctx, uid, sid, c.mode, "资料"+strconv.Itoa(i+1), c.text, true)
		if err != nil {
			t.Fatal(err)
		}
		job, err := f.imp.CreateJob(ctx, uid, sid, c.mode, []uint64{m.ID})
		if err != nil {
			t.Fatal(err)
		}
		if last, err = f.imp.Confirm(ctx, uid, job.ID, nil); err != nil {
			t.Fatal(err)
		}
	}
	return last
}

func groupCount(p plan.Plan) map[string]int {
	m := map[string]int{}
	for _, it := range p.Items {
		m[it.Group]++
	}
	return m
}

func TestHomeStatesAndPlanAfterImport(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t, rules.Strengthen)

	h, err := f.plan.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != "no_material" || h.Plan != nil || h.DaysToExam != 79 {
		t.Fatalf("还没导入资料：%+v", h)
	}

	res := f.imported(t, uid, sid)
	if !res.PlanReady {
		t.Error("确认入库后应立即生成今日计划（PRD 1.8）")
	}
	h, err = f.plan.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != "normal" || h.Plan == nil || len(h.Plan.Items) == 0 {
		t.Fatalf("导入后应进入正常首页并有计划：%+v", h)
	}
	p := *h.Plan
	if p.BudgetMinutes != 45 || p.TotalMinutes > 45+0.01 || p.TotalMinutes <= 0 {
		t.Errorf("计划用时应不超过每日 45 分钟：%+v", p.Snapshot)
	}
	if p.Stage != string(rules.Strengthen) {
		t.Errorf("计划阶段 %s", p.Stage)
	}
	sum := 0.0
	for _, it := range p.Items {
		sum += it.Minutes
		if it.SubjectID != sid {
			t.Errorf("条目科目 %d", it.SubjectID)
		}
	}
	if d := sum - p.TotalMinutes; d > 0.01 || d < -0.01 {
		t.Errorf("条目用时之和 %.2f 应等于计划总用时 %.2f", sum, p.TotalMinutes)
	}
	if len(h.Banks) != 1 || h.Banks[0].Questions == 0 || h.Banks[0].Unlearned == 0 {
		t.Errorf("我的题库卡：%+v", h.Banks)
	}
	if h.Push == nil || h.Push.Kind != "weekly_qtype_drill" {
		t.Errorf("强化期主推应为本周题型专项：%+v", h.Push)
	}

	// 同一天再取不重排（幂等）。
	again, err := f.plan.Today(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Items) != len(p.Items) || again.Items[0] != p.Items[0] {
		t.Error("同一天的计划应保持不变")
	}
	gc := groupCount(p)
	if gc["new"]+gc["review"]+gc["weak"]+gc["recite"] != len(p.Items) {
		t.Errorf("未知分组：%v", gc)
	}
}

func TestPlanProgressAndSummary(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t, rules.Strengthen)
	f.imported(t, uid, sid)
	p, err := f.plan.Today(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	var qid uint64
	for _, it := range p.Items {
		if it.QuestionID != 0 {
			qid = it.QuestionID
			break
		}
	}
	if qid == 0 {
		t.Fatalf("计划里应有题目：%+v", p.Items)
	}
	if _, err := f.db.Exec(`INSERT INTO attempts (owner_user_id, question_id, answer_mode, is_correct, score, full_score, duration_seconds, answered_at)
		VALUES (?, ?, 'typed', 1, 8, 10, 300, ?)`, uid, qid, now()); err != nil {
		t.Fatal(err)
	}
	p2, err := f.plan.Today(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	done := 0
	for i := range p2.Items {
		if p2.Done[i] {
			done++
		}
	}
	if done != 1 || p2.DoneMinutes <= 0 || p2.Completed {
		t.Errorf("做完一题：done=%d minutes=%.1f completed=%v", done, p2.DoneMinutes, p2.Completed)
	}
	// 已经开始做了，题库变化也不重排。
	if !f.plan.Regenerate(ctx, uid) {
		t.Error("应报告有计划")
	}
	p3, _ := f.plan.Today(ctx, uid)
	if len(p3.Items) != len(p2.Items) {
		t.Error("开始做以后不应重排")
	}

	s, err := f.plan.TodaySummary(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if s.Questions != 1 || s.Minutes != 5 || s.CorrectRate != 1 || s.Streak != 1 {
		t.Errorf("今日小结：%+v", s)
	}
	h, _ := f.plan.Home(ctx, uid)
	if h.Streak != 1 {
		t.Errorf("连续打卡 %d", h.Streak)
	}
}

func TestStagePrompt(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	// 距考试 79 天按日期应在强化期，选了基础期的人会收到提示。
	uid, sid := f.user(t, rules.Foundation)
	f.imported(t, uid, sid)
	h, err := f.plan.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if h.Prompt == nil || h.Prompt.To != string(rules.Strengthen) || h.Prompt.Reason != "by_date" {
		t.Fatalf("应提示进入强化期：%+v", h.Prompt)
	}
	if h.Push == nil || h.Push.Kind != "new_kp_progress" || h.Push.Progress == nil {
		t.Errorf("基础期主推：%+v", h.Push)
	}
	h, err = f.plan.AnswerStagePrompt(ctx, uid, false)
	if err != nil {
		t.Fatal(err)
	}
	if h.Prompt != nil || h.Stage != string(rules.Foundation) {
		t.Errorf("拒绝后留在基础期且不再弹：%+v", h)
	}

	uid2, sid2 := f.user(t, rules.Foundation)
	f.imported(t, uid2, sid2)
	h, err = f.plan.AnswerStagePrompt(ctx, uid2, true)
	if err != nil {
		t.Fatal(err)
	}
	if h.Prompt != nil || h.Stage != string(rules.Strengthen) {
		t.Errorf("接受后立即进入强化期：stage=%s prompt=%+v", h.Stage, h.Prompt)
	}
	if h.Plan == nil || h.Plan.Stage != string(rules.Strengthen) {
		t.Errorf("接受后今天的计划按新阶段重排：%+v", h.Plan)
	}
}

func TestGenerateAllAndIsolation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a, sa := f.user(t, rules.Strengthen)
	b, _ := f.user(t, rules.Strengthen)
	f.imported(t, a, sa)
	if _, err := f.db.Exec("DELETE FROM daily_plans"); err != nil {
		t.Fatal(err)
	}
	n, err := f.plan.GenerateAll(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("应至少给有题库的用户生成计划：%d", n)
	}
	var before string
	if err := f.db.QueryRow("SELECT CAST(plan_groups AS CHAR) FROM daily_plans WHERE owner_user_id = ?", a).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.plan.GenerateAll(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var after string
	var rows int
	if err := f.db.QueryRow("SELECT CAST(plan_groups AS CHAR), (SELECT COUNT(*) FROM daily_plans WHERE owner_user_id = ?) FROM daily_plans WHERE owner_user_id = ?", a, a).Scan(&after, &rows); err != nil {
		t.Fatal(err)
	}
	if after != before || rows != 1 {
		t.Error("重跑不应重排或重复生成")
	}
	// B 看不到 A 的题库和计划。
	hb, err := f.plan.Home(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if hb.State != "no_material" || hb.Plan != nil || hb.Banks[0].Questions != 0 {
		t.Errorf("用户隔离：%+v", hb)
	}
}
