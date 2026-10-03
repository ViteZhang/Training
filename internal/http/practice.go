package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/rules"
)

// 4.1 训练首页、4.2 自定义练习、4.3 客观题、4.10 退出、4.11 本组总结、4.12 错题本（T17）。

func ptr[T any](v T) *T { return &v }

func toConfig(c *gen.PracticeConfig) practice.Config {
	var out practice.Config
	if c == nil {
		return out
	}
	if c.SectionIds != nil {
		for _, id := range *c.SectionIds {
			out.SectionIDs = append(out.SectionIDs, uint64(id))
		}
	}
	if c.Qtypes != nil {
		for _, t := range *c.Qtypes {
			out.QTypes = append(out.QTypes, string(t))
		}
	}
	if c.Count != nil {
		out.Count = *c.Count
	}
	out.OnlyUnmastered = c.OnlyUnmastered != nil && *c.OnlyUnmastered
	out.AIFill = c.AiFill != nil && *c.AiFill
	return out
}

func toGenSession(s practice.Session) gen.PracticeSession {
	out := gen.PracticeSession{Id: int64(s.ID), Kind: gen.PracticeKind(s.Kind), Title: s.Title, SubjectId: int64(s.SubjectID),
		Status: gen.PracticeSessionStatus(s.Status), CursorIndex: s.CursorIndex, StartedAt: s.StartedAt, DoneCount: s.DoneCount,
		Questions: make([]gen.PracticeQuestion, 0, len(s.Questions))}
	if s.AIFilled > 0 {
		out.AiFilled = ptr(s.AIFilled)
	}
	if s.Shortfall > 0 {
		out.Shortfall = ptr(s.Shortfall)
	}
	for _, q := range s.Questions {
		x := gen.PracticeQuestion{Id: int64(q.ID), Qtype: gen.QuestionType(q.QType), Stem: q.Stem, Source: gen.QuestionSource(q.Source), ExamYear: q.ExamYear,
			RubricCount: q.RubricN, OriginTags: []string{}}
		if q.Source == "ai_generated" {
			x.OriginTags = append(x.OriginTags, "ai_generated")
		}
		if q.Source == "official" {
			x.OriginTags = append(x.OriginTags, "official")
		}
		if q.Answer != "" {
			x.Answer = ptr(q.Answer)
		}
		if q.Analysis != "" {
			x.Analysis = ptr(q.Analysis)
		}
		if q.Score != nil {
			x.Score = ptr(float32(*q.Score))
		}
		if len(q.Options) > 0 {
			opts := make([]gen.ChoiceOption, len(q.Options))
			for i, o := range q.Options {
				opts[i] = gen.ChoiceOption{Key: o.Key, Text: o.Text}
			}
			x.Options = &opts
		}
		if q.FileName != "" {
			ref := gen.SourceRef{MaterialId: int64(q.MaterialID), FileName: q.FileName}
			if q.Page > 0 {
				ref.Page = ptr(q.Page)
			}
			x.SourceRef = &ref
		}
		if q.PlanGroup != "" {
			x.PlanGroup = ptr(gen.PlanGroupKey(q.PlanGroup))
		}
		sized(&x.KnowledgePoints, len(q.KPs))
		for i, k := range q.KPs {
			x.KnowledgePoints[i].Id, x.KnowledgePoints[i].Name, x.KnowledgePoints[i].IsPrimary = int64(k.ID), k.Name, k.IsPrimary
		}
		if a := q.Answered; a != nil {
			b := gen.AttemptBrief{IsCorrect: a.IsCorrect, Revealed: a.Revealed}
			if a.SelfAssess != "" {
				b.SelfAssess = ptr(gen.SelfAssessLevel(a.SelfAssess))
			}
			if len(a.Selected) > 0 {
				b.Selected = ptr(a.Selected)
			}
			x.Answered = &b
		}
		out.Questions = append(out.Questions, x)
	}
	return out
}

func (h *Handlers) GetPracticeHome(c *gin.Context, subjectID gen.SubjectId) {
	p, err := h.deps.Practice.Home(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.PracticeHome{SubjectId: int64(p.SubjectID), Stage: gen.Stage(p.Stage), TotalQuestions: p.Total, ReciteDue: p.ReciteDue, PaperFirst: p.PaperFirst}
	out.WrongBook.Total, out.WrongBook.Due = p.WrongTotal, p.WrongDue
	if p.TodayTotal > 0 || p.TodaySession != 0 {
		out.Today = &struct {
			Done      int     `json:"done"`
			Minutes   float32 `json:"minutes"`
			SessionId *int64  `json:"session_id,omitempty"`
			Total     int     `json:"total"`
		}{Done: p.TodayDone, Minutes: float32(p.TodayMinutes), Total: p.TodayTotal}
		if p.TodaySession != 0 {
			out.Today.SessionId = ptr(int64(p.TodaySession))
		}
	}
	if rules.Stage(p.Stage) != rules.Foundation {
		out.TypeDrill = &struct {
			DoneThisWeek int               `json:"done_this_week"`
			Qtype        *gen.QuestionType `json:"qtype,omitempty"`
			WeeklyTarget int               `json:"weekly_target"`
		}{DoneThisWeek: p.DrillsWeek, WeeklyTarget: 3}
		if p.DrillQType != "" {
			out.TypeDrill.Qtype = ptr(gen.QuestionType(p.DrillQType))
		}
	}
	sized(&out.QtypeCounts, len(p.QTypeCounts))
	for i, q := range p.QTypeCounts {
		out.QtypeCounts[i].Qtype, out.QtypeCounts[i].Count = gen.QuestionType(q.QType), q.Count
	}
	if ip := p.InProgress; ip != nil {
		out.InProgress = &struct {
			Done      int              `json:"done"`
			Kind      gen.PracticeKind `json:"kind"`
			SessionId int64            `json:"session_id"`
			Title     string           `json:"title"`
			Total     int              `json:"total"`
		}{Done: ip.Done, Kind: gen.PracticeKind(ip.Kind), SessionId: int64(ip.SessionID), Title: ip.Title, Total: ip.Total}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) PreviewPractice(c *gin.Context, subjectID gen.SubjectId) {
	var body gen.PracticeConfig
	if !bind(c, &body) {
		return
	}
	p, err := h.deps.Practice.Preview(c.Request.Context(), currentUser(c), uint64(subjectID), toConfig(&body))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.PracticePreview{Available: p.Available, Count: p.Count, Minutes: float32(p.Minutes)}
	if p.AIFill > 0 {
		out.AiFill = ptr(p.AIFill)
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) CreatePracticeSession(c *gin.Context) {
	var body gen.CreatePracticeSessionRequest
	if !bind(c, &body) {
		return
	}
	in := practice.CreateInput{SubjectID: uint64(body.SubjectId), Kind: practice.Kind(body.Kind), Config: toConfig(body.Config), AIFill: body.AiFill != nil && *body.AiFill}
	if body.Qtype != nil {
		in.QType = string(*body.Qtype)
	}
	if body.WrongGroup != nil {
		in.WrongGroup = &practice.WrongGroup{By: string(body.WrongGroup.By), Key: body.WrongGroup.Key}
	}
	s, err := h.deps.Practice.Create(c.Request.Context(), currentUser(c), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenSession(s))
}

func (h *Handlers) GetPracticeSession(c *gin.Context, sessionID gen.SessionId) {
	s, err := h.deps.Practice.Get(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenSession(s))
}

func (h *Handlers) SavePracticeProgress(c *gin.Context, sessionID gen.SessionId) {
	var body gen.SavePracticeProgressJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Practice.SaveProgress(c.Request.Context(), currentUser(c), uint64(sessionID), body.CursorIndex); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) SubmitAttempt(c *gin.Context, sessionID gen.SessionId) {
	var body gen.SubmitAttemptRequest
	if !bind(c, &body) {
		return
	}
	in := practice.AttemptInput{QuestionID: uint64(body.QuestionId), Key: body.IdempotencyKey, Duration: body.DurationSeconds, AnsweredAt: body.AnsweredAt}
	if body.Selected != nil {
		in.Selected = *body.Selected
	}
	if body.AnswerText != nil {
		in.AnswerText = *body.AnswerText
	}
	in.Revealed = body.Revealed != nil && *body.Revealed
	in.Offline = body.Offline != nil && *body.Offline
	if body.SelfAssess != nil {
		in.SelfAssess = string(*body.SelfAssess)
	}
	r, err := h.deps.Practice.Submit(c.Request.Context(), currentUser(c), uint64(sessionID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AttemptResult{AttemptId: int64(r.AttemptID), IsCorrect: r.IsCorrect, WrongBook: gen.AttemptResultWrongBook(r.WrongBook),
		KpChanges: make([]gen.MasteryChange, len(r.KPs))}
	if r.Answer != "" {
		out.CorrectAnswer = ptr(r.Answer)
	}
	if r.Analysis != "" {
		out.Analysis = ptr(r.Analysis)
	}
	for i, k := range r.KPs {
		out.KpChanges[i] = gen.MasteryChange{KpId: int64(k.KPID), Name: k.Name, From: gen.MasteryState(k.From), To: gen.MasteryState(k.To), M: float32(k.M)}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) FinishPracticeSession(c *gin.Context, sessionID gen.SessionId) {
	s, err := h.deps.Practice.Finish(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.PracticeSummary{SessionId: int64(s.SessionID), Title: s.Title, QuestionCount: s.QuestionCount, Answered: s.Answered,
		CorrectRate: float32(s.CorrectRate), Minutes: float32(s.Minutes), FromBank: s.FromBank, FromAi: s.FromAI, Comment: s.Comment}
	sized(&out.ByQtype, len(s.ByQType))
	for i, q := range s.ByQType {
		out.ByQtype[i].Qtype, out.ByQtype[i].Total, out.ByQtype[i].Correct = gen.QuestionType(q.QType), q.Total, q.Correct
	}
	sized(&out.ReviewKps, len(s.ReviewKPs))
	for i, k := range s.ReviewKPs {
		out.ReviewKps[i].KpId, out.ReviewKps[i].Name, out.ReviewKps[i].Reason = int64(k.KPID), k.Name, k.Reason
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetWrongBook(c *gin.Context, subjectID gen.SubjectId) {
	w, err := h.deps.Practice.WrongBook(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.WrongBook{Total: w.Total, ToRedo: w.ToRedo, WeekNew: w.WeekNew, Eliminated: w.Eliminated, Items: make([]gen.WrongItem, len(w.Items))}
	for i, it := range w.Items {
		x := gen.WrongItem{QuestionId: int64(it.QuestionID), Qtype: gen.QuestionType(it.Qtype), Stem: it.Stem, Source: gen.QuestionSource(it.Source),
			WrongCount: int(it.WrongCount), AddedReason: gen.WrongItemAddedReason(it.AddedReason)}
		if it.ExamYear.Valid {
			x.ExamYear = ptr(int(it.ExamYear.Int16))
		}
		if it.LastScoreRate.Valid {
			v, _ := strconv.ParseFloat(it.LastScoreRate.String, 32)
			x.LastScoreRate = ptr(float32(v))
		}
		if it.LastLossType.Valid {
			x.LossType = ptr(gen.WrongItemLossType(it.LastLossType.WrongBookLastLossType))
		}
		if it.NextReviewOn.Valid {
			x.NextReviewOn = &openapi_types.Date{Time: it.NextReviewOn.Time}
		}
		if it.KpID != 0 {
			x.Kp = &struct {
				Id   int64  `json:"id"`
				Name string `json:"name"`
			}{Id: it.KpID, Name: anyString(it.KpName)}
		}
		out.Items[i] = x
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) RemoveFromWrongBook(c *gin.Context, questionID gen.QuestionId) {
	if err := h.deps.Practice.RemoveWrong(c.Request.Context(), currentUser(c), uint64(questionID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ReportQuestion(c *gin.Context, questionID gen.QuestionId) {
	var body gen.ReportQuestionJSONBody
	if !bind(c, &body) {
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = *body.Reason
	}
	off, err := h.deps.Practice.Report(c.Request.Context(), currentUser(c), uint64(questionID), reason)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"offline": off})
}

// anyString 读出 sqlc 推断为 interface{} 的字符串列（驱动返回 []byte 或 string）。
func anyString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	}
	return ""
}
