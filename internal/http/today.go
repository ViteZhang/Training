package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/plan"
	"peetraining-server/internal/rules"
)

// 2.1 今日首页、2.1e 阶段提示、2.2 今日完成（T16）。

func toGenPlan(p plan.Plan) gen.TodayPlan {
	out := gen.TodayPlan{Date: openapi_types.Date{Time: p.Date.Date()}, Stage: gen.Stage(p.Stage), BudgetMinutes: p.BudgetMinutes,
		TotalMinutes: float32(p.TotalMinutes), DoneMinutes: float32(p.DoneMinutes), Completed: p.Completed}
	if p.Shortfall > 0 {
		v := float32(p.Shortfall)
		out.ShortfallMinutes = &v
	}
	order := []gen.PlanGroupKey{gen.PlanGroupKeyReview, gen.PlanGroupKeyNew, gen.PlanGroupKeyWeak, gen.PlanGroupKeyRecite}
	groups := map[gen.PlanGroupKey]*gen.PlanGroupSummary{}
	for _, k := range order {
		groups[k] = &gen.PlanGroupSummary{Group: k}
	}
	sized(&out.Items, len(p.Items))
	for i, it := range p.Items {
		x := &out.Items[i]
		x.Group, x.KpId, x.SubjectId, x.Minutes, x.Done = gen.PlanGroupKey(it.Group), int64(it.KPID), int64(it.SubjectID), float32(it.Minutes), p.Done[i]
		if it.QuestionID != 0 {
			q := int64(it.QuestionID)
			x.QuestionId = &q
		}
		if it.QType != "" {
			t := gen.QuestionType(it.QType)
			x.Qtype = &t
		}
		if g := groups[x.Group]; g != nil {
			g.Count++
			g.Minutes += x.Minutes
			if x.Done {
				g.Done++
			}
		}
	}
	out.Groups = []gen.PlanGroupSummary{}
	for _, k := range order {
		if groups[k].Count > 0 {
			out.Groups = append(out.Groups, *groups[k])
		}
	}
	return out
}

func mixMap(m rules.PlanMix) map[string]float32 {
	return map[string]float32{"new": float32(m.New), "review": float32(m.Review), "weak": float32(m.Weak), "recite": float32(m.Recite)}
}

func toGenHome(h plan.Home) gen.Home {
	out := gen.Home{State: gen.HomeState(h.State), DaysToExam: h.DaysToExam, Stage: gen.Stage(h.Stage), FalseMasteryCount: h.FalseMastery,
		StreakDays: h.Streak, Estimates: []gen.EstimateCard{}}
	tm := float32(h.TomorrowMinutes)
	out.TomorrowMinutes = &tm
	if h.LowCoverage {
		out.LowCoverage = &h.LowCoverage
	}
	if h.OrganizingJob != 0 {
		id := int64(h.OrganizingJob)
		out.OrganizingJobId = &id
	}
	if h.Plan != nil {
		p := toGenPlan(*h.Plan)
		out.Plan = &p
	}
	if h.Prompt != nil {
		sp := gen.StagePrompt{To: gen.Stage(h.Prompt.To), Reason: gen.StagePromptReason(h.Prompt.Reason)}
		sp.Mix.Current, sp.Mix.Next = mixMap(h.Prompt.Current), mixMap(h.Prompt.Next)
		out.StagePrompt = &sp
	}
	if h.Push != nil {
		out.StagePush = &struct {
			Desc     string                `json:"desc"`
			Kind     gen.HomeStagePushKind `json:"kind"`
			Progress *float32              `json:"progress,omitempty"`
			Qtype    *gen.QuestionType     `json:"qtype,omitempty"`
			Title    string                `json:"title"`
		}{Desc: h.Push.Desc, Kind: gen.HomeStagePushKind(h.Push.Kind), Title: h.Push.Title}
		if h.Push.Progress != nil {
			v := float32(*h.Push.Progress)
			out.StagePush.Progress = &v
		}
		if h.Push.QType != "" {
			t := gen.QuestionType(h.Push.QType)
			out.StagePush.Qtype = &t
		}
	}
	sized(&out.Banks, len(h.Banks))
	for i, b := range h.Banks {
		x := &out.Banks[i]
		x.SubjectId, x.Name, x.QuestionCount, x.KpCount, x.Organizing = int64(b.SubjectID), b.Name, b.Questions, b.KPs, b.Organizing
		if b.Code != "" {
			x.Code = &b.Code
		}
		x.IsEssay = &b.IsEssay
		if b.Organizing {
			x.RecognizedCount = &b.Recognized
		}
		d := &x.MasteryDistribution
		d.Unlearned, d.Learning, d.Consolidating, d.Mastered = b.Unlearned, b.Learning, b.Consolidating, b.Mastered
	}
	return out
}

func (h *Handlers) GetHome(c *gin.Context) {
	home, err := h.deps.Plan.Home(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	h.homeWithEstimates(c, home)
}

func (h *Handlers) AnswerStagePrompt(c *gin.Context) {
	var body gen.AnswerStagePromptJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		_ = c.Error(ErrBadRequest(err.Error()))
		return
	}
	home, err := h.deps.Plan.AnswerStagePrompt(c.Request.Context(), currentUser(c), body.Accept)
	if err != nil {
		_ = c.Error(err)
		return
	}
	h.homeWithEstimates(c, home)
}

func (h *Handlers) GetTodayPlan(c *gin.Context) {
	p, err := h.deps.Plan.Today(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPlan(p))
}

func (h *Handlers) GetTodaySummary(c *gin.Context) {
	s, err := h.deps.Plan.TodaySummary(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.TodaySummary{StreakDays: s.Streak, QuestionCount: s.Questions, CorrectRate: float32(s.CorrectRate), Minutes: float32(s.Minutes), NewMastered: s.NewMastered}
	share := func(k string) *float32 {
		v, ok := s.LossShares[k]
		if !ok {
			return nil
		}
		f := float32(v)
		return &f
	}
	out.LossShares.Knowledge, out.LossShares.Norm, out.LossShares.Time = share("knowledge"), share("norm"), share("time")
	sized(&out.MasteryChanges, 0)
	c.JSON(http.StatusOK, out)
}

// homeWithEstimates 补上预估分卡（T22）后返回首页。
func (h *Handlers) homeWithEstimates(c *gin.Context, home plan.Home) {
	out := toGenHome(home)
	if h.deps.Score != nil {
		cards, err := h.deps.Score.Cards(c.Request.Context(), currentUser(c))
		if err != nil {
			_ = c.Error(err)
			return
		}
		for _, card := range cards {
			out.Estimates = append(out.Estimates, toGenEstimate(card))
		}
	}
	c.JSON(http.StatusOK, out)
}
