package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/score"
)

// 4.24 整卷报告、4.25 时间分析报告、2.1 预估分卡、6.2 提分看板（T22）。

func toGenEstimate(c score.Card) gen.EstimateCard {
	out := gen.EstimateCard{SubjectId: int64(c.SubjectID), Name: c.Name, IsEssay: c.IsEssay, FullScore: c.FullScore, Ready: c.Ready, TargetScore: c.Target,
		Gap: c.Gap, TodayChange: c.TodayChange}
	if c.Ready {
		out.Low, out.High, out.BasisPapers, out.BasisQuestions = ptr(c.Low), ptr(c.High), ptr(c.BasisPapers), ptr(c.BasisQuestions)
		out.ComputedAt = ptr(c.ComputedAt)
		if c.MainGap != "" {
			out.MainGapQtype = ptr(gen.QuestionType(c.MainGap))
		}
	}
	return out
}

func lossPoints(m map[string]float64) gen.LossPoints {
	return gen.LossPoints{Knowledge: float32(m["knowledge"]), Norm: float32(m["norm"]), Time: float32(m["time"])}
}

func (h *Handlers) GetPaperReport(c *gin.Context, sessionID gen.SessionId) {
	r, err := h.deps.Score.PaperReport(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.PaperReport{SessionId: int64(r.SessionID), SubjectId: int64(r.SubjectID), Title: r.Title, Kind: gen.PaperKind(r.Kind), Mode: gen.PaperMode(r.Mode),
		Score: float32(r.Score), FullScore: float32(r.FullScore), CountsForEstimate: r.CountsForEstimate, GradedAt: r.GradedAt, PrevDelta: f32p(r.PrevDelta),
		TargetScore: r.Target, Gap: f32p(r.Gap), Loss: lossPoints(r.Loss), LossTotal: float32(r.LossTotal), ByQtype: make([]gen.QTypeScore, len(r.ByQType))}
	if r.PaperID != 0 {
		out.PaperId = ptr(int64(r.PaperID))
	}
	if r.WeakestQType != "" {
		out.WeakestQtype = ptr(gen.QuestionType(r.WeakestQType))
	}
	for i, q := range r.ByQType {
		out.ByQtype[i] = gen.QTypeScore{Qtype: gen.QuestionType(q.QType), Got: float32(q.Got), Full: float32(q.Full)}
	}
	if t := r.Time; t != nil {
		tr := gen.TimeReport{TotalMinutes: t.TotalMinutes, UsedMinutes: t.UsedMinutes, UsedFull: t.UsedFull, Unanswered: t.Unanswered, TimeLoss: float32(t.TimeLoss),
			CheckSuggestedMinutes: t.CheckSuggestedMinutes, CheckActualMinutes: t.CheckActualMinutes, CheckStatus: gen.TimeStatus(t.CheckStatus),
			Conclusion: t.Conclusion, Advice: append([]string{}, t.Advice...), Sections: make([]gen.SectionTime, len(t.Sections)), Trend: make([]gen.UnansweredPoint, len(t.Trend))}
		for i, s := range t.Sections {
			tr.Sections[i] = gen.SectionTime{Qtype: gen.QuestionType(s.QType), Count: s.Count, Answered: s.Answered, SuggestedMinutes: s.SuggestedMinutes,
				ActualMinutes: s.ActualMinutes, Status: gen.TimeStatus(s.Status), DiffMinutes: s.DiffMinutes, Unfinished: s.Unfinished}
		}
		for i, p := range t.Trend {
			tr.Trend[i] = gen.UnansweredPoint{SessionId: int64(p.SessionID), Date: p.Date, Unanswered: p.Unanswered}
		}
		out.Time = &tr
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetDashboard(c *gin.Context, subjectID gen.SubjectId) {
	d, err := h.deps.Score.Dashboard(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.Dashboard{Estimate: toGenEstimate(d.Card), LossPoints: lossPoints(d.LossPoints), LossShares: lossPoints(d.LossShares), SectionsReady: d.SectionsReady,
		Trend: make([]gen.EstimateWeek, len(d.Trend)), Sections: make([]gen.SectionMastery, len(d.Sections)), FalseMastery: make([]gen.KPRef, len(d.FalseMastery)),
		RecentPapers: make([]gen.RecentPaper, len(d.RecentPapers))}
	for i, w := range d.Trend {
		out.Trend[i] = gen.EstimateWeek{WeekStart: openapi_types.Date{Time: w.WeekStart.Date()}, Low: w.Low, High: w.High, Mid: float32(w.Mid)}
	}
	for i, s := range d.Sections {
		out.Sections[i] = gen.SectionMastery{Id: int64(s.ID), Name: s.Name, Share: float32(s.Share), Mastery: float32(s.Mastery), KpCount: s.KPCount}
	}
	for i, k := range d.FalseMastery {
		out.FalseMastery[i] = gen.KPRef{KpId: int64(k.ID), Name: k.Name}
	}
	for i, p := range d.RecentPapers {
		out.RecentPapers[i] = gen.RecentPaper{SessionId: int64(p.SessionID), Title: p.Title, Kind: gen.PaperKind(p.Kind), Mode: gen.PaperMode(p.Mode),
			Score: float32(p.Score), FullScore: float32(p.FullScore), CountsForEstimate: p.CountsForEstimate, GradedAt: p.GradedAt}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) AddFalseMasteryToToday(c *gin.Context, subjectID gen.SubjectId) {
	n, err := h.deps.Plan.AddFalseMastery(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"added": n})
}
