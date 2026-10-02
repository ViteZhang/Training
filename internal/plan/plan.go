// Package plan 是今日计划与首页（T16，PRD 11.5、模块 2）：每天 0 点（北京时间）生成计划，首页聚合倒计时、阶段、
// 今日计划、阶段主推、我的题库与「以为会了」。选题与配比都在 internal/rules 里，这里只准备候选和保存结果。
package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"peetraining-server/internal/bank"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/rules"
)

// Service 生成与读取今日计划。
type Service struct {
	db      *sql.DB
	q       *dbq.Queries
	params  *params.Store
	profile *profile.Service
	bank    *bank.Service
	now     func() time.Time
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB      *sql.DB
	Params  *params.Store
	Profile *profile.Service
	Bank    *bank.Service
	Now     func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: d.DB, q: dbq.New(d.DB), params: d.Params, profile: d.Profile, bank: d.Bank, now: now}
}

func (s *Service) today() rules.Day { return rules.DayOf(s.now()) }

// dayStart 是北京时间某天 0 点对应的 UTC 时刻。
func dayStart(d rules.Day) time.Time { return d.Time().UTC() }

// Item 是计划里的一项（daily_plans.plan_groups 的快照）。
type Item struct {
	SubjectID  uint64  `json:"subject_id"`
	Group      string  `json:"group"`
	QuestionID uint64  `json:"question_id,omitempty"`
	KPID       uint64  `json:"kp_id"`
	QType      string  `json:"qtype,omitempty"`
	Minutes    float64 `json:"minutes"`
}

// Snapshot 是保存的计划。
type Snapshot struct {
	Items        []Item  `json:"items"`
	TotalMinutes float64 `json:"total_minutes"`
	Shortfall    float64 `json:"shortfall_minutes"`
}

// Plan 是今日计划与完成情况。
type Plan struct {
	Date          rules.Day
	Stage         string
	BudgetMinutes int
	Snapshot
	Done        map[int]bool // 按 Items 下标
	DoneMinutes float64
	Completed   bool
}

// candidates 按 PRD 11.5 准备一门课的候选：新知识点、到期复习、薄弱查漏、背诵，并算提分收益。
func (s *Service) candidates(ctx context.Context, userID uint64, subject dbq.GetSubjectBankRow, stage rules.Stage, mastery map[uint64]dbq.ListKPMasteryRowsRow,
	wrongDue map[uint64]bool, today rules.Day, p rules.Params) ([]rules.PlanCandidate, []rules.CoverageKP, error) {
	stats, err := s.bank.Stats(ctx, userID, subject.BankID)
	if err != nil {
		return nil, nil, err
	}
	prof := stats.Profile
	scoreEach := map[rules.QType]float64{}
	for _, st := range prof.Structure {
		scoreEach[st.QType] = st.ScoreEach
	}
	sectionCount := stats.SectionCount()
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	kps, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: subject.BankID, Owner: owner})
	if err != nil {
		return nil, nil, err
	}
	type kpInfo struct {
		m      float64
		state  rules.MasteryState
		exam   int
		weight float64
	}
	info := map[uint64]kpInfo{}
	var coverage []rules.CoverageKP
	for _, k := range kps {
		if k.Level != dbq.KnowledgePointsLevelPoint {
			continue
		}
		m, _ := strconv.ParseFloat(k.M, 64)
		st := rules.MasteryState(k.State)
		w := rules.KPWeight(prof.SectionShares[int64(stats.Section(k.ID))], int(k.ExamCount), prof.Ready, sectionCount, p.Plan)
		info[k.ID] = kpInfo{m: m, state: st, exam: int(k.ExamCount), weight: w}
		coverage = append(coverage, rules.CoverageKP{State: st, ExamCount: int(k.ExamCount)})
	}
	qs, err := s.q.ListBankQuestionsFull(ctx, dbq.ListBankQuestionsFullParams{UserID: userID, BankID: subject.BankID, Owner: owner})
	if err != nil {
		return nil, nil, err
	}
	var out []rules.PlanCandidate
	hasQuestion := map[uint64]bool{}
	for _, q := range qs {
		kp := uint64(q.PrimaryKpID)
		k, ok := info[kp]
		if !ok {
			// 没有归到知识点的题按未学习、等权处理，保证也能被选到。
			k = kpInfo{state: rules.StateUnlearned, weight: rules.KPWeight(0, 0, false, max(sectionCount, 1), p.Plan)}
		}
		hasQuestion[kp] = true
		qt := rules.QType(q.Qtype)
		c := rules.PlanCandidate{QuestionID: int64(q.ID), KPID: int64(kp), QType: qt, Gain: rules.Gain(k.weight, k.m, rules.QTypeCoef(qt, scoreEach))}
		due := wrongDue[q.ID]
		if mr, ok := mastery[kp]; ok && mr.NextReviewOn.Valid && rules.DayFromDateColumn(mr.NextReviewOn.Time) <= today {
			due = true
		}
		switch {
		case due:
			c.Group = rules.GroupReview
		case k.state == rules.StateUnlearned && stage == rules.Foundation:
			c.Group = rules.GroupNew
		case k.state == rules.StateUnlearned:
			// 强化期以后「薄弱查漏」包含未学习知识点，保证新用户首日计划不为空（PRD 11.5）。
			c.Group = rules.GroupWeak
		case k.state != rules.StateMastered:
			c.Group = rules.GroupWeak
		default:
			continue
		}
		out = append(out, c)
		if c.Group == rules.GroupWeak && k.state == rules.StateUnlearned && stage != rules.Foundation {
			// 同一题也作为新知识点候选（冲刺期覆盖率不足时有新知识点份额）。
			nc := c
			nc.Group = rules.GroupNew
			out = append(out, nc)
		}
	}
	// 背诵：复习日已到或从没背过、而且有原文或采分点可背的知识点。
	for id, k := range info {
		mr, ok := mastery[id]
		due := !ok || mr.ReciteIntervalStep == 0 || (mr.ReciteNextReviewOn.Valid && rules.DayFromDateColumn(mr.ReciteNextReviewOn.Time) <= today)
		if !due || (k.state == rules.StateUnlearned && !hasQuestion[id] && stage == rules.Foundation) {
			continue
		}
		out = append(out, rules.PlanCandidate{Group: rules.GroupRecite, KPID: int64(id), Gain: rules.Gain(k.weight, k.m, 1)})
	}
	return out, coverage, nil
}

// Generate 生成某天的计划（PRD 11.5）。force 为 false 时已有计划不重新生成（定时任务可重复执行）。
func (s *Service) Generate(ctx context.Context, userID uint64, day rules.Day, force bool) (Plan, error) {
	if !force {
		if p, err := s.load(ctx, userID, day); err == nil {
			return p, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Plan{}, err
		}
	}
	prof, err := s.profile.Get(ctx, userID)
	if err != nil {
		return Plan{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Plan{}, err
	}
	subjects, err := s.profile.Subjects(ctx, userID)
	if err != nil {
		return Plan{}, err
	}
	rows, err := s.q.ListKPMasteryRows(ctx, userID)
	if err != nil {
		return Plan{}, err
	}
	mastery := map[uint64]dbq.ListKPMasteryRowsRow{}
	for _, r := range rows {
		mastery[r.KpID] = r
	}
	due, err := s.q.ListWrongBookDue(ctx, dbq.ListWrongBookDueParams{OwnerUserID: userID, NextReviewOn: sql.NullTime{Time: day.Date(), Valid: true}})
	if err != nil {
		return Plan{}, err
	}
	wrongDue := map[uint64]bool{}
	for _, q := range due {
		wrongDue[q] = true
	}
	stage := rules.Stage(prof.Stage)
	var in rules.PlanInput
	var coverage []rules.CoverageKP
	for _, sub := range subjects.Items {
		b := dbq.GetSubjectBankRow{SubjectID: sub.ID, Name: sub.Name, BankID: sub.BankID}
		cands, cov, err := s.candidates(ctx, userID, b, stage, mastery, wrongDue, day, p)
		if err != nil {
			return Plan{}, err
		}
		coverage = append(coverage, cov...)
		in.Subjects = append(in.Subjects, rules.SubjectCandidates{SubjectID: int64(sub.ID), Candidates: cands})
	}
	adv := rules.AdviseStage(stage, prof.DaysToExam, rules.Coverage(coverage), p.Stage)
	in.BudgetMinutes = float64(prof.DailyMinutes)
	in.Mix = rules.MixFor(stage, adv.LowCoverage, adv.NewKPShare, p.PlanMix)
	built := rules.BuildPlan(in, p.Plan)
	snap := Snapshot{TotalMinutes: built.TotalMinutes, Items: []Item{}}
	for _, it := range built.Items {
		snap.Items = append(snap.Items, Item{SubjectID: uint64(it.SubjectID), Group: string(it.Group), QuestionID: uint64(it.QuestionID), KPID: uint64(it.KPID),
			QType: string(it.QType), Minutes: it.Minutes})
	}
	for _, groups := range built.Shortfall {
		for _, m := range groups {
			snap.Shortfall += m
		}
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return Plan{}, err
	}
	if err := s.q.UpsertDailyPlan(ctx, dbq.UpsertDailyPlanParams{OwnerUserID: userID, PlanDate: day.Date(), Stage: dbq.DailyPlansStage(stage),
		BudgetMinutes: prof.DailyMinutes, PlanGroups: raw, GeneratedAt: s.now().UTC()}); err != nil {
		return Plan{}, err
	}
	return s.load(ctx, userID, day)
}

// load 读计划并按今天的作答与背诵记录标出完成情况；全部完成时记下完成时间。
func (s *Service) load(ctx context.Context, userID uint64, day rules.Day) (Plan, error) {
	row, err := s.q.GetDailyPlan(ctx, dbq.GetDailyPlanParams{OwnerUserID: userID, PlanDate: day.Date()})
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Date: day, Stage: string(row.Stage), BudgetMinutes: int(row.BudgetMinutes), Done: map[int]bool{}}
	if err := json.Unmarshal(row.PlanGroups, &p.Snapshot); err != nil {
		return Plan{}, err
	}
	since := dayStart(day)
	attempted, err := s.q.ListAttemptedSince(ctx, dbq.ListAttemptedSinceParams{OwnerUserID: userID, AnsweredAt: since})
	if err != nil {
		return Plan{}, err
	}
	recited, err := s.q.ListRecitedSince(ctx, dbq.ListRecitedSinceParams{OwnerUserID: userID, CreatedAt: since})
	if err != nil {
		return Plan{}, err
	}
	doneQ, doneKP := map[uint64]bool{}, map[uint64]bool{}
	for _, q := range attempted {
		doneQ[q] = true
	}
	for _, k := range recited {
		doneKP[k] = true
	}
	for i, it := range p.Items {
		if (it.Group == string(rules.GroupRecite) && doneKP[it.KPID]) || (it.QuestionID != 0 && doneQ[it.QuestionID]) {
			p.Done[i] = true
			p.DoneMinutes += it.Minutes
		}
	}
	p.Completed = len(p.Items) > 0 && len(p.Done) == len(p.Items)
	if p.Completed && !row.CompletedAt.Valid {
		if err := s.q.SetDailyPlanCompleted(ctx, dbq.SetDailyPlanCompletedParams{CompletedAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, OwnerUserID: userID, PlanDate: day.Date()}); err != nil {
			return Plan{}, err
		}
	}
	return p, nil
}

// Today 返回今天的计划，没有时当场生成（新用户、定时任务没跑到）。
func (s *Service) Today(ctx context.Context, userID uint64) (Plan, error) {
	return s.Generate(ctx, userID, s.today(), false)
}

// Regenerate 在题库变化后重排今天的计划（确认入库后，PRD 1.8、2.1b）；今天已经开始做了就不重排，返回是否有计划。
func (s *Service) Regenerate(ctx context.Context, userID uint64) bool {
	cur, err := s.load(ctx, userID, s.today())
	if err == nil && len(cur.Done) > 0 {
		return len(cur.Items) > 0
	}
	p, err := s.Generate(ctx, userID, s.today(), true)
	return err == nil && len(p.Items) > 0
}

// GenerateAll 给所有用户生成今天的计划（每天 0 点的定时任务，可重复执行：已有计划的不重排）。返回处理的人数。
// 某个用户出错不影响其他人，错误汇总后返回，任务按 Asynq 规则重试。
func (s *Service) GenerateAll(ctx context.Context, batch int) (int, error) {
	day := s.today()
	n := 0
	var after uint64
	var errs []error
	for {
		ids, err := s.q.ListPlanUsers(ctx, dbq.ListPlanUsersParams{ID: after, Limit: int32(batch)})
		if err != nil {
			return n, errors.Join(append(errs, err)...)
		}
		for _, id := range ids {
			after = id
			if _, err := s.Generate(ctx, id, day, false); err != nil {
				errs = append(errs, fmt.Errorf("用户 %d：%w", id, err))
				continue
			}
			n++
		}
		if len(ids) < batch {
			return n, errors.Join(errs...)
		}
	}
}
