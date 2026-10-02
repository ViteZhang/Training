package plan

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

// StagePrompt 是 2.1e 进入新阶段提示。
type StagePrompt struct {
	To      string
	Reason  string
	Current rules.PlanMix
	Next    rules.PlanMix
}

// StagePush 是阶段主推（PRD 模块 2、11.5）。
type StagePush struct {
	Kind, Title, Desc string
	QType             string
	Progress          *float64
}

// BankCard 是「我的题库」里的一门课。
type BankCard struct {
	SubjectID                                    uint64
	Name, Code                                   string
	IsEssay                                      bool
	Questions, KPs                               int
	Organizing                                   bool
	Recognized                                   int
	Unlearned, Learning, Consolidating, Mastered int
}

// Home 是今日首页。
type Home struct {
	State           string // no_material / organizing / done / normal
	DaysToExam      int
	Stage           string
	Prompt          *StagePrompt
	LowCoverage     bool
	Plan            *Plan
	Push            *StagePush
	Banks           []BankCard
	OrganizingJob   uint64
	FalseMastery    int
	Streak          int
	TomorrowMinutes float64
}

// Home 聚合今日首页（2.1）：首页按状态四选一，优先级：还没导入资料 → 题库整理中 → 今日已完成 → 正常。
func (s *Service) Home(ctx context.Context, userID uint64) (Home, error) {
	prof, err := s.profile.Get(ctx, userID)
	if err != nil {
		return Home{}, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Home{}, err
	}
	h := Home{DaysToExam: prof.DaysToExam, Stage: string(prof.Stage), TomorrowMinutes: float64(prof.DailyMinutes)}
	if prof.PendingDailyMinutes.Valid {
		h.TomorrowMinutes = float64(prof.PendingDailyMinutes.Int16)
	}
	subjects, err := s.profile.Subjects(ctx, userID)
	if err != nil {
		return Home{}, err
	}
	jobs, err := s.q.ListActiveImportJobs(ctx, userID)
	if err != nil {
		return Home{}, err
	}
	organizing := map[uint64]int64{}
	for _, j := range jobs {
		organizing[uint64(j.SubjectID.Int64)] += j.Recognized
		if h.OrganizingJob == 0 {
			h.OrganizingJob = j.ID
		}
	}
	hasContent := false
	var coverage []rules.CoverageKP
	for _, sub := range subjects.Items {
		o, err := s.bank.Overview(ctx, userID, sub.ID)
		if err != nil {
			return Home{}, err
		}
		_, org := organizing[sub.ID]
		h.Banks = append(h.Banks, BankCard{SubjectID: sub.ID, Name: sub.Name, Code: sub.Code.String, IsEssay: sub.IsEssay, Questions: o.Questions, KPs: o.KPs,
			Organizing: org, Recognized: int(organizing[sub.ID]), Unlearned: o.Unlearned, Learning: o.Learning, Consolidating: o.Consolidating, Mastered: o.Mastered})
		if o.Questions > 0 || o.KPs > 0 {
			hasContent = true
		}
		for range o.Unlearned {
			coverage = append(coverage, rules.CoverageKP{State: rules.StateUnlearned})
		}
		for range o.Learning + o.Consolidating + o.Mastered {
			coverage = append(coverage, rules.CoverageKP{State: rules.StateLearning})
		}
	}

	// 阶段提示（PRD 11.4）：同一目标阶段只弹一次。
	stage := rules.Stage(prof.Stage)
	adv := rules.AdviseStage(stage, prof.DaysToExam, rules.Coverage(coverage), p.Stage)
	h.LowCoverage = adv.LowCoverage
	if adv.Prompt && prof.StagePrompted.String != string(adv.Suggested) {
		h.Prompt = &StagePrompt{To: string(adv.Suggested), Reason: adv.Reason, Current: p.PlanMix[stage], Next: p.PlanMix[adv.Suggested]}
	}

	if hasContent {
		pl, err := s.Today(ctx, userID)
		if err != nil {
			return Home{}, err
		}
		h.Plan = &pl
		h.Push = s.push(ctx, userID, stage, prof.DaysToExam, h.Banks, p)
	}
	switch {
	case !hasContent && len(jobs) == 0:
		h.State = "no_material"
	case len(jobs) > 0:
		h.State = "organizing"
	case h.Plan != nil && h.Plan.Completed:
		h.State = "done"
	default:
		h.State = "normal"
	}
	if h.FalseMastery, err = s.falseMastery(ctx, userID, p); err != nil {
		return Home{}, err
	}
	h.Streak, err = s.streak(ctx, userID)
	return h, err
}

// push 是阶段主推：基础期新知识点进度、强化期本周题型专项（真题里分值占比最高的题型）、冲刺期本周整卷、考前期模拟考试。
func (s *Service) push(ctx context.Context, userID uint64, stage rules.Stage, days int, banks []BankCard, p rules.Params) *StagePush {
	switch stage {
	case rules.Foundation:
		total, learned := 0, 0
		for _, b := range banks {
			total += b.Unlearned + b.Learning + b.Consolidating + b.Mastered
			learned += b.Learning + b.Consolidating + b.Mastered
		}
		v := 0.0
		if total > 0 {
			v = float64(learned) / float64(total)
		}
		return &StagePush{Kind: "new_kp_progress", Title: "新知识点进度", Desc: "已学 " + strconv.Itoa(learned) + " / " + strconv.Itoa(total) + " 个知识点", Progress: &v}
	case rules.Strengthen:
		best, bestTotal := "", 0.0
		for _, b := range banks {
			bk, err := s.bank.Overview(ctx, userID, b.SubjectID)
			if err != nil {
				continue
			}
			st, err := s.bank.Stats(ctx, userID, bk.BankID)
			if err != nil || !st.Profile.Ready {
				continue
			}
			for _, slot := range st.Profile.Structure {
				if slot.Total > bestTotal {
					best, bestTotal = string(slot.QType), slot.Total
				}
			}
		}
		push := &StagePush{Kind: "weekly_qtype_drill", Title: "本周题型专项", Desc: "每周 3 次，专练真题里分值最高的题型", QType: best}
		if best == "" {
			push.Desc = "导入真题卷后按分值最高的题型安排专项"
		}
		return push
	case rules.Sprint:
		return &StagePush{Kind: "weekly_paper", Title: "本周整卷", Desc: "每周 1 套整卷，推荐模拟考试模式"}
	default:
		desc := "每 3–4 天 1 次模拟考试，按考试时长限时完成"
		if days <= 3 {
			desc = "考前最后 3 天不再做新卷，回顾错题和背诵"
		}
		return &StagePush{Kind: "mock_exam", Title: "模拟考试", Desc: desc}
	}
}

// falseMastery 数「以为会了」的知识点：最近一次自评为掌握，且近 7 天作答 ≥ 2 次、正确率 < 50%（PRD 11.2）。
func (s *Service) falseMastery(ctx context.Context, userID uint64, p rules.Params) (int, error) {
	rows, err := s.q.ListKPMasteryRows(ctx, userID)
	if err != nil {
		return 0, err
	}
	self := map[uint64]bool{}
	for _, r := range rows {
		if r.LastSelfAssess.Valid && r.LastSelfAssess.KpMasteryLastSelfAssess == dbq.KpMasteryLastSelfAssessMastered {
			self[r.KpID] = true
		}
	}
	if len(self) == 0 {
		return 0, nil
	}
	since := dayStart(s.today().AddDays(-(p.MasteryState.FalseMasteryWindowDays - 1)))
	atts, err := s.q.ListRecentKPAttempts(ctx, dbq.ListRecentKPAttemptsParams{OwnerUserID: userID, AnsweredAt: since})
	if err != nil {
		return 0, err
	}
	recent := map[uint64][]bool{}
	for _, a := range atts {
		if !self[a.KpID] {
			continue
		}
		score, _ := strconv.ParseFloat(a.Score.String, 64)
		full, _ := strconv.ParseFloat(a.FullScore.String, 64)
		rate := 0.0
		if full > 0 {
			rate = score / full
		}
		recent[a.KpID] = append(recent[a.KpID], rules.IsCorrect(rules.IsObjective(rules.QType(a.Qtype)), a.IsCorrect.Bool, rate, p.MasteryState.SubjectiveCorrectRate))
	}
	n := 0
	for kp, rs := range recent {
		if rules.FalseMastery(rules.SelfMastered, rs, p.MasteryState) && self[kp] {
			n++
		}
	}
	return n, nil
}

// streak 是连续打卡天数：截至今天（今天还没练则截至昨天）连续有作答或背诵的天数。
func (s *Service) streak(ctx context.Context, userID uint64) (int, error) {
	today := s.today()
	days, err := s.q.ListActivityDays(ctx, dbq.ListActivityDaysParams{UserID: userID, Since: dayStart(today.AddDays(-400))})
	if err != nil {
		return 0, err
	}
	set := map[string]bool{}
	for _, d := range days {
		if v, ok := d.(string); ok {
			set[v] = true
		} else if b, ok := d.([]byte); ok {
			set[string(b)] = true
		}
	}
	cur := today
	if !set[cur.String()] {
		cur = cur.AddDays(-1)
	}
	n := 0
	for set[cur.String()] {
		n++
		cur = cur.AddDays(-1)
	}
	return n, nil
}

// AnswerStagePrompt 处理 2.1e：接受时立即切换阶段并重排今天的计划；拒绝时留在当前阶段。同一目标阶段都只弹一次。
func (s *Service) AnswerStagePrompt(ctx context.Context, userID uint64, accept bool) (Home, error) {
	h, err := s.Home(ctx, userID)
	if err != nil || h.Prompt == nil {
		return h, err
	}
	if err := s.q.AnswerStagePrompt(ctx, dbq.AnswerStagePromptParams{Target: sql.NullString{String: h.Prompt.To, Valid: true}, Accept: accept, UserID: userID}); err != nil {
		return Home{}, err
	}
	if accept {
		s.Regenerate(ctx, userID)
	}
	return s.Home(ctx, userID)
}

// Summary 是今日训练完成（2.2）。
type Summary struct {
	Streak      int
	Questions   int
	CorrectRate float64
	Minutes     float64
	NewMastered int
	LossShares  map[string]float64
}

// TodaySummary 统计今天的训练：题数、正确率、用时、新增已掌握、主观题失分归因占比。
func (s *Service) TodaySummary(ctx context.Context, userID uint64) (Summary, error) {
	p, err := s.params.Rules(ctx)
	if err != nil {
		return Summary{}, err
	}
	since := dayStart(s.today())
	atts, err := s.q.ListAttemptsSince(ctx, dbq.ListAttemptsSinceParams{OwnerUserID: userID, AnsweredAt: since})
	if err != nil {
		return Summary{}, err
	}
	out := Summary{Questions: len(atts), LossShares: map[string]float64{}}
	correct := 0
	for _, a := range atts {
		score, _ := strconv.ParseFloat(a.Score.String, 64)
		full, _ := strconv.ParseFloat(a.FullScore.String, 64)
		rate := 0.0
		if full > 0 {
			rate = score / full
		}
		if rules.IsCorrect(rules.IsObjective(rules.QType(a.Qtype)), a.IsCorrect.Bool, rate, p.MasteryState.SubjectiveCorrectRate) {
			correct++
		}
		out.Minutes += float64(a.DurationSeconds) / 60
	}
	if len(atts) > 0 {
		out.CorrectRate = float64(correct) / float64(len(atts))
	}
	n, err := s.q.CountMasteredSince(ctx, dbq.CountMasteredSinceParams{OwnerUserID: userID, UpdatedAt: since})
	if err != nil {
		return Summary{}, err
	}
	out.NewMastered = int(n)
	losses, err := s.q.ListGradingLossSince(ctx, dbq.ListGradingLossSinceParams{OwnerUserID: userID, CreatedAt: since})
	if err != nil {
		return Summary{}, err
	}
	total := 0.0
	for _, l := range losses {
		var m map[string]struct {
			Points float64 `json:"points"`
		}
		if json.Unmarshal(l, &m) != nil {
			continue
		}
		for k, v := range m {
			out.LossShares[k] += v.Points
			total += v.Points
		}
	}
	for k := range out.LossShares {
		if total > 0 {
			out.LossShares[k] /= total
		}
	}
	out.Streak, err = s.streak(ctx, userID)
	return out, err
}
