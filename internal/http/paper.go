package http

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/practice"
)

// 4.18 整卷列表、4.19 选择作答模式、4.20 练习模式、4.21 模拟考试、4.22 答题卡、4.23 交卷（T21）。

func f32p(v *float64) *float32 {
	if v == nil {
		return nil
	}
	return ptr(float32(*v))
}

func toGenBrief(b practice.PaperBrief) gen.PaperBrief {
	out := gen.PaperBrief{Id: int64(b.ID), Kind: gen.PaperKind(b.Kind), Title: b.Title, ExamYear: b.ExamYear, QuestionCount: b.QuestionCount,
		FullScore: float32(b.FullScore), ActualScore: float32(b.ActualScore), MissingNote: optStr(b.MissingNote), DurationMinutes: b.DurationMinutes,
		AiFilled: b.AIFilled, Status: gen.PaperBriefStatus(b.Status)}
	if a := b.Active; a != nil {
		out.Session = &struct {
			Answered int                    `json:"answered"`
			Id       int64                  `json:"id"`
			Mode     gen.PaperMode          `json:"mode"`
			Status   gen.PaperSessionStatus `json:"status"`
			Total    int                    `json:"total"`
		}{Answered: a.Answered, Id: int64(a.SessionID), Mode: gen.PaperMode(a.Mode), Status: gen.PaperSessionStatus(a.Status), Total: a.Total}
	}
	if l := b.Last; l != nil {
		st := gen.PaperSessionStatus(l.Status)
		out.Last = &struct {
			FinishedAt time.Time               `json:"finished_at"`
			FullScore  *float32                `json:"full_score,omitempty"`
			Mode       gen.PaperMode           `json:"mode"`
			Score      *float32                `json:"score,omitempty"`
			SessionId  int64                   `json:"session_id"`
			Status     *gen.PaperSessionStatus `json:"status,omitempty"`
		}{FinishedAt: l.FinishedAt, FullScore: ptr(float32(l.FullScore)), Mode: gen.PaperMode(l.Mode), Score: f32p(l.Score), SessionId: int64(l.SessionID), Status: &st}
	}
	return out
}

func toGenDetail(d practice.PaperDetail) gen.PaperDetail {
	b := toGenBrief(d.PaperBrief)
	out := gen.PaperDetail{Id: b.Id, Kind: b.Kind, Title: b.Title, ExamYear: b.ExamYear, QuestionCount: b.QuestionCount, FullScore: b.FullScore,
		ActualScore: b.ActualScore, MissingNote: b.MissingNote, DurationMinutes: b.DurationMinutes, AiFilled: b.AiFilled, Status: gen.PaperDetailStatus(b.Status),
		Session: b.Session, Last: b.Last, CheckMinutes: d.CheckMinutes, RecommendedMode: gen.PaperMode(d.RecommendedMode), CountsForEstimate: d.CountsForEstimate,
		Sections: make([]gen.PaperSection, len(d.Sections))}
	for i, s := range d.Sections {
		out.Sections[i] = gen.PaperSection{Qtype: gen.QuestionType(s.QType), Count: s.Count, ScoreEach: float32(s.ScoreEach), Total: float32(s.Total), SuggestedMinutes: s.SuggestedMinutes}
	}
	return out
}

func toGenPaperItem(it practice.PaperItem) gen.PaperItem {
	out := gen.PaperItem{Seq: it.Seq, Section: it.Section, Qtype: gen.QuestionType(it.QType), Score: float32(it.Score), QuestionId: int64(it.QuestionID), Stem: it.Stem,
		DraftText: optStr(it.Draft), Marked: it.Marked, Answered: it.Answered, TimeSpentSeconds: it.TimeSpent, RequiredWords: it.RequiredWords, Got: f32p(it.Got)}
	if len(it.Options) > 0 {
		opts := make([]gen.ChoiceOption, len(it.Options))
		for i, o := range it.Options {
			opts[i] = gen.ChoiceOption{Key: o.Key, Text: o.Text}
		}
		out.Options = &opts
	}
	if it.AIGenerated {
		out.OriginTags = &[]string{"ai_generated"}
	}
	if it.GradingID != 0 {
		out.GradingId = ptr(int64(it.GradingID))
	}
	return out
}

func toGenPaperSession(v practice.PaperSessionView) gen.PaperSession {
	out := gen.PaperSession{Id: int64(v.ID), PaperId: int64(v.PaperID), SubjectId: int64(v.SubjectID), Title: v.Title, Kind: gen.PaperKind(v.Kind), Mode: gen.PaperMode(v.Mode),
		Status: gen.PaperSessionStatus(v.Status), StartedAt: v.StartedAt, DeadlineAt: v.Deadline, ServerNow: v.ServerNow, ElapsedSeconds: v.ElapsedSeconds,
		TotalMinutes: v.TotalMinutes, CheckMinutes: v.CheckMinutes, RemindLeftMinutes: v.RemindLeftMinutes, ResumeAvailable: v.ResumeAvailable,
		Score: f32p(v.Score), FullScore: float32(v.FullScore), CountsForEstimate: ptr(v.CountsForEstimate),
		Reminders: make([]gen.PaperReminder, len(v.Reminders)), Items: make([]gen.PaperItem, len(v.Items))}
	for i, r := range v.Reminders {
		out.Reminders[i] = gen.PaperReminder{Qtype: gen.QuestionType(r.QType), AtMinutes: r.AtMinutes, SuggestedMinutes: r.SuggestedMinutes, Text: r.Text}
	}
	for i, it := range v.Items {
		out.Items[i] = toGenPaperItem(it)
	}
	return out
}

func (h *Handlers) ListPapers(c *gin.Context, subjectID gen.SubjectId) {
	l, err := h.deps.Practice.Papers(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.PaperList{RealExam: make([]gen.PaperBrief, len(l.Real)), AiPapers: make([]gen.PaperBrief, len(l.AI))}
	for i, b := range l.Real {
		out.RealExam[i] = toGenBrief(b)
	}
	for i, b := range l.AI {
		out.AiPapers[i] = toGenBrief(b)
	}
	if l.WeeklyRemaining != nil {
		out.WeeklyRemaining = nullable.NewNullableWithValue(*l.WeeklyRemaining)
	} else {
		out.WeeklyRemaining = nullable.NewNullNullable[int]()
	}
	if p := l.InProgress; p != nil {
		out.InProgress = &struct {
			PaperId   int64  `json:"paper_id"`
			SessionId int64  `json:"session_id"`
			SubjectId int64  `json:"subject_id"`
			Title     string `json:"title"`
		}{PaperId: int64(p.PaperID), SessionId: int64(p.SessionID), SubjectId: int64(p.SubjectID), Title: p.Title}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) ComposePaper(c *gin.Context, subjectID gen.SubjectId) {
	var body gen.ComposePaperJSONBody
	if !bind(c, &body) {
		return
	}
	d, err := h.deps.Practice.ComposePaper(c.Request.Context(), currentUser(c), uint64(subjectID), string(body.Kind))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenDetail(d))
}

func (h *Handlers) GetPaper(c *gin.Context, paperID gen.PaperId) {
	d, err := h.deps.Practice.Paper(c.Request.Context(), currentUser(c), uint64(paperID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenDetail(d))
}

func (h *Handlers) StartPaper(c *gin.Context, paperID gen.PaperId) {
	var body gen.StartPaperJSONBody
	if !bind(c, &body) {
		return
	}
	v, err := h.deps.Practice.StartPaper(c.Request.Context(), currentUser(c), uint64(paperID), string(body.Mode), body.IdempotencyKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenPaperSession(v))
}

func (h *Handlers) GetPaperSession(c *gin.Context, sessionID gen.SessionId) {
	v, err := h.deps.Practice.PaperSession(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPaperSession(v))
}

func (h *Handlers) UpdatePaperItem(c *gin.Context, sessionID gen.SessionId, seq int) {
	var body gen.UpdatePaperItemJSONBody
	if !bind(c, &body) {
		return
	}
	in := practice.ItemUpdate{Draft: body.DraftText, Marked: body.Marked}
	if body.TimeSpentDelta != nil {
		in.TimeDelta = *body.TimeSpentDelta
	}
	it, err := h.deps.Practice.UpdatePaperItem(c.Request.Context(), currentUser(c), uint64(sessionID), seq, in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPaperItem(it))
}

func (h *Handlers) PausePaper(c *gin.Context, sessionID gen.SessionId) {
	v, err := h.deps.Practice.PausePaper(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPaperSession(v))
}

func (h *Handlers) ResumePaper(c *gin.Context, sessionID gen.SessionId) {
	var body gen.ResumePaperJSONBody
	if c.Request.ContentLength > 0 && !bind(c, &body) {
		return
	}
	v, err := h.deps.Practice.ResumePaper(c.Request.Context(), currentUser(c), uint64(sessionID), body.InterruptedAt)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPaperSession(v))
}

func (h *Handlers) SubmitPaper(c *gin.Context, sessionID gen.SessionId) {
	v, err := h.deps.Practice.SubmitPaper(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPaperSession(v))
}

func (h *Handlers) AbandonPaper(c *gin.Context, sessionID gen.SessionId) {
	if err := h.deps.Practice.AbandonPaper(c.Request.Context(), currentUser(c), uint64(sessionID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
