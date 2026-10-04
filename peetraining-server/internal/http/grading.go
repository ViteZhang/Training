package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/practice"
)

// 4.4 主观题、4.6 批改中、4.7 批改结果、4.8 异议、4.9 待批改（T18）。

func toGenGrading(g practice.Grading, reference string) gen.GradingResult {
	out := gen.GradingResult{GradingId: int64(g.ID), AttemptId: int64(g.AttemptID), QuestionId: int64(g.QuestionID), Status: gen.GradingResultStatus(g.Status),
		FullScore: float32(g.FullScore), RubricVersion: g.Rubric.Version, RubricSource: gen.RubricSource(g.Rubric.Source), StructureOk: g.Extra.StructureOK,
		Suggestions: g.Extra.Items, Trigger: gen.GradingResultTrigger(g.Trigger), Disputed: g.Disputed, QuotaCharged: g.QuotaCharged,
		WrongBook: gen.GradingResultWrongBook("none"), Points: make([]gen.GradingPoint, len(g.Points)), Loss: make([]gen.LossItem, len(g.Loss)),
		KpChanges: make([]gen.MasteryChange, len(g.Extra.KPs))}
	if g.Extra.WrongBook != "" {
		out.WrongBook = gen.GradingResultWrongBook(g.Extra.WrongBook)
	}
	if g.Score != nil {
		out.Score = ptr(float32(*g.Score))
	}
	if g.Extra.StructureNote != "" {
		out.StructureNote = ptr(g.Extra.StructureNote)
	}
	if g.ParentID != 0 {
		out.ParentGradingId = ptr(int64(g.ParentID))
	}
	if g.AnswerText != "" {
		out.AnswerText = ptr(g.AnswerText)
	}
	if reference == "" {
		reference = g.Rubric.Reference
	}
	if reference != "" {
		out.ReferenceAnswer = ptr(reference)
	}
	if g.RubricChanged {
		out.RubricChanged = ptr(true)
	}
	if g.Rubric.FileName != "" {
		ref := gen.SourceRef{MaterialId: int64(g.Rubric.MaterialID), FileName: g.Rubric.FileName}
		if g.Rubric.Page > 0 {
			ref.Page = ptr(g.Rubric.Page)
		}
		out.RubricRef = &ref
	}
	if g.Status == "done" {
		out.Counts = &struct {
			Hit     int `json:"hit"`
			Miss    int `json:"miss"`
			Partial int `json:"partial"`
		}{}
	}
	for i, p := range g.Points {
		x := gen.GradingPoint{Seq: p.Seq, Content: p.Content, Score: float32(p.Score), Got: float32(p.Got), Verdict: gen.GradingPointVerdict(p.Verdict)}
		if p.Quote != "" {
			x.Quote = ptr(p.Quote)
		}
		if p.Reason != "" {
			x.Reason = ptr(p.Reason)
		}
		out.Points[i] = x
		if out.Counts != nil {
			switch p.Verdict {
			case "hit":
				out.Counts.Hit++
			case "partial":
				out.Counts.Partial++
			default:
				out.Counts.Miss++
			}
		}
	}
	for i, l := range g.Loss {
		out.Loss[i] = gen.LossItem{Type: gen.LossItemType(l.Type), Points: float32(l.Points), Reason: l.Reason}
	}
	for i, k := range g.Extra.KPs {
		out.KpChanges[i] = gen.MasteryChange{KpId: int64(k.KPID), Name: k.Name, From: gen.MasteryState(k.From), To: gen.MasteryState(k.To), M: float32(k.M)}
	}
	return out
}

func (h *Handlers) SubmitSubjective(c *gin.Context, sessionID gen.SessionId) {
	var body gen.SubmitSubjectiveRequest
	if !bind(c, &body) {
		return
	}
	in := practice.SubjectiveInput{QuestionID: uint64(body.QuestionId), Key: body.IdempotencyKey, Answer: body.AnswerText, Duration: body.DurationSeconds,
		Timed: body.Timed != nil && *body.Timed}
	if body.PhotoKeys != nil {
		in.PhotoKeys = *body.PhotoKeys
	}
	if body.AnswerMode != nil {
		in.Mode = string(*body.AnswerMode)
	}
	g, err := h.deps.Practice.SubmitSubjective(c.Request.Context(), currentUser(c), uint64(sessionID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenGrading(g, ""))
}

func (h *Handlers) GetGrading(c *gin.Context, gradingID gen.GradingId) {
	g, err := h.deps.Practice.Grading(c.Request.Context(), currentUser(c), uint64(gradingID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenGrading(g, ""))
}

func (h *Handlers) RegradeAfterRubricChange(c *gin.Context, gradingID gen.GradingId) {
	g, err := h.deps.Practice.RegradeAfterRubricChange(c.Request.Context(), currentUser(c), uint64(gradingID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenGrading(g, ""))
}

func (h *Handlers) CreateDispute(c *gin.Context, gradingID gen.GradingId) {
	var body gen.DisputeRequest
	if !bind(c, &body) {
		return
	}
	in := practice.DisputeInput{Reason: string(body.Reason), AllowAccess: body.AllowAccess != nil && *body.AllowAccess}
	if body.Note != nil {
		in.Note = *body.Note
	}
	g, err := h.deps.Practice.Dispute(c.Request.Context(), currentUser(c), uint64(gradingID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenGrading(g, ""))
}

func (h *Handlers) ListPendingGradings(c *gin.Context) {
	items, left, err := h.deps.Practice.Pending(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	var out gen.PendingGradings
	if left != nil {
		out.RemainingToday = nullable.NewNullableWithValue(*left)
	} else {
		out.RemainingToday = nullable.NewNullNullable[int]()
	}
	sized(&out.Items, len(items))
	for i, it := range items {
		x := &out.Items[i]
		x.GradingId, x.QuestionId, x.Qtype, x.Stem, x.SavedAt = int64(it.GradingID), int64(it.QuestionID), gen.QuestionType(it.QType), it.Stem, it.SavedAt
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) SubmitPendingGradings(c *gin.Context) {
	done, left, err := h.deps.Practice.SubmitPending(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	results := make([]gen.GradingResult, len(done))
	for i, g := range done {
		results[i] = toGenGrading(g, "")
	}
	c.JSON(http.StatusOK, gin.H{"graded": len(done), "remaining_pending": left, "results": results})
}
