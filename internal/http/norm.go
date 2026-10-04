package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/practice"
)

// 4.5 拍手写稿、4.13 答题规范（T19）。

func (h *Handlers) RequestHandwritingUploads(c *gin.Context) {
	var body gen.RequestHandwritingUploadsJSONBody
	if !bind(c, &body) {
		return
	}
	files := make([]practice.PhotoFile, len(body.Files))
	for i, f := range body.Files {
		files[i] = practice.PhotoFile{ContentType: string(f.ContentType), Size: int64(f.Size)}
	}
	ts, err := h.deps.Practice.RequestUploads(c.Request.Context(), currentUser(c), files)
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gin.H, len(ts))
	for i, t := range ts {
		headers := t.Headers
		if headers == nil {
			headers = map[string]string{}
		}
		items[i] = gin.H{"object_key": t.ObjectKey, "upload_url": t.URL, "upload_headers": headers, "expires_at": t.ExpiresAt}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func ranges(rs [][2]int) []gen.TextRange {
	out := make([]gen.TextRange, len(rs))
	for i, r := range rs {
		out[i] = gen.TextRange{Start: r[0], End: r[1]}
	}
	return out
}

func (h *Handlers) RecognizeHandwriting(c *gin.Context) {
	var body gen.RecognizeHandwritingJSONBody
	if !bind(c, &body) {
		return
	}
	r, err := h.deps.Practice.Recognize(c.Request.Context(), currentUser(c), body.ObjectKeys)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.HandwritingResult{Text: r.Text, LowConfidence: ranges(r.LowConfidence), UncertainCount: len(r.LowConfidence)}
	sized(&out.Pages, len(r.Pages))
	for i, p := range r.Pages {
		out.Pages[i].ObjectKey, out.Pages[i].Text, out.Pages[i].LowConfidence = p.ObjectKey, p.Text, ranges(p.LowConfidence)
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetAnswerNorm(c *gin.Context, subjectID gen.SubjectId, qtype string) {
	n, err := h.deps.Practice.AnswerNorm(c.Request.Context(), currentUser(c), uint64(subjectID), qtype)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AnswerNorm{Qtype: gen.QuestionType(n.QType), Tips: n.Tips, Elements: make([]gen.NormElement, len(n.Elements))}
	for i, e := range n.Elements {
		out.Elements[i] = gen.NormElement{Name: e.Name, Desc: e.Desc, Share: float32(e.Share)}
	}
	if e := n.Example; e != nil {
		out.Example = &struct {
			KpName          *string        `json:"kp_name,omitempty"`
			OriginalText    *string        `json:"original_text,omitempty"`
			QuestionId      int64          `json:"question_id"`
			ReferenceAnswer *string        `json:"reference_answer,omitempty"`
			RubricPoints    []string       `json:"rubric_points"`
			SourceRef       *gen.SourceRef `json:"source_ref,omitempty"`
			Stem            string         `json:"stem"`
		}{QuestionId: int64(e.QuestionID), Stem: e.Stem, RubricPoints: e.Points, KpName: optStr(e.KPName), OriginalText: optStr(e.OriginalText), ReferenceAnswer: optStr(e.Reference)}
		if out.Example.RubricPoints == nil {
			out.Example.RubricPoints = []string{}
		}
		if e.FileName != "" {
			ref := gen.SourceRef{MaterialId: int64(e.MaterialID), FileName: e.FileName}
			if e.Page > 0 {
				ref.Page = ptr(e.Page)
			}
			out.Example.SourceRef = &ref
		}
	}
	if l := n.Last; l != nil {
		out.Last = &struct {
			AnswerText string   `json:"answer_text"`
			FullScore  *float32 `json:"full_score,omitempty"`
			GradingId  int64    `json:"grading_id"`
			Missing    []string `json:"missing"`
			QuestionId int64    `json:"question_id"`
			Score      *float32 `json:"score,omitempty"`
			Stem       string   `json:"stem"`
		}{AnswerText: l.Answer, GradingId: int64(l.GradingID), Missing: l.Missing, QuestionId: int64(l.QuestionID), Stem: l.Stem}
		if l.Score != nil {
			out.Last.Score = ptr(float32(*l.Score))
		}
		if l.FullScore != nil {
			out.Last.FullScore = ptr(float32(*l.FullScore))
		}
	}
	if n.PracticeID != 0 {
		out.PracticeQuestionId = ptr(int64(n.PracticeID))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) CheckAnswerNorm(c *gin.Context, questionID gen.QuestionId) {
	var body gen.CheckAnswerNormJSONBody
	if !bind(c, &body) {
		return
	}
	r, err := h.deps.Practice.CheckNorm(c.Request.Context(), currentUser(c), uint64(questionID), body.AnswerText, body.IdempotencyKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.NormCheckResult{GradingId: int64(r.GradingID), Complete: r.Complete, Suggestions: r.Suggestions}
	if out.Suggestions == nil {
		out.Suggestions = []string{}
	}
	sized(&out.Elements, len(r.Elements))
	for i, e := range r.Elements {
		x := &out.Elements[i]
		x.Name, x.Present, x.Quote, x.Suggestion = e.Name, e.Present, optStr(e.Quote), optStr(e.Suggestion)
	}
	c.JSON(http.StatusOK, out)
}
