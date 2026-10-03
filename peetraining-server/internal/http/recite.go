package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/practice"
)

// 4.14 挖空、4.15 默写、4.16 口述、4.17 背诵完成（T20）。

func toGenRecite(s practice.ReciteSession) gen.ReciteSession {
	out := gen.ReciteSession{Id: int64(s.ID), SubjectId: int64(s.SubjectID), Title: s.Title, DoneCount: s.DoneCount, OralEnabled: s.OralEnabled,
		Items: make([]gen.ReciteItem, len(s.Items))}
	for i, it := range s.Items {
		x := gen.ReciteItem{KpId: int64(it.KPID), Name: it.Name, Path: it.Path, OriginalText: it.OriginalText, Keywords: it.Keywords,
			Segments: make([]gen.ReciteSegment, len(it.Segments))}
		if x.Keywords == nil {
			x.Keywords = []string{}
		}
		for j, sg := range it.Segments {
			x.Segments[j] = gen.ReciteSegment{Text: sg.Text, Blank: sg.Blank}
		}
		if it.Result != "" {
			x.Result = ptr(gen.ReciteResultLevel(it.Result))
		}
		if it.FileName != "" {
			ref := gen.SourceRef{MaterialId: int64(it.MaterialID), FileName: it.FileName}
			if it.Page > 0 {
				ref.Page = ptr(it.Page)
			}
			x.SourceRef = &ref
		}
		out.Items[i] = x
	}
	return out
}

func (h *Handlers) CreateReciteSession(c *gin.Context) {
	var body gen.CreateReciteSessionJSONBody
	if !bind(c, &body) {
		return
	}
	var retry uint64
	if body.RetryOf != nil {
		retry = uint64(*body.RetryOf)
	}
	s, err := h.deps.Practice.CreateRecite(c.Request.Context(), currentUser(c), uint64(body.SubjectId), retry)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenRecite(s))
}

func (h *Handlers) GetReciteSession(c *gin.Context, sessionID gen.SessionId) {
	s, err := h.deps.Practice.GetRecite(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenRecite(s))
}

func (h *Handlers) RecordRecite(c *gin.Context, sessionID gen.SessionId) {
	var body gen.ReciteRecordRequest
	if !bind(c, &body) {
		return
	}
	in := practice.ReciteInput{KPID: uint64(body.KpId), Mode: string(body.Mode), Key: body.IdempotencyKey}
	if body.Result != nil {
		in.Result = string(*body.Result)
	}
	if body.Text != nil {
		in.Text = *body.Text
	}
	if body.AudioKey != nil {
		in.AudioKey = *body.AudioKey
	}
	r, err := h.deps.Practice.RecordRecite(c.Request.Context(), currentUser(c), uint64(sessionID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.ReciteRecordResult{Result: gen.ReciteResultLevel(r.Result), M: float32(r.M), NextReviewOn: openapi_types.Date{Time: r.NextReview.Date()}, Transcript: optStr(r.Transcript)}
	if r.Coverage != nil {
		out.Coverage = &struct {
			Hit      int `json:"hit"`
			Keywords []struct {
				Hit  bool   `json:"hit"`
				Text string `json:"text"`
			} `json:"keywords"`
			Total int `json:"total"`
		}{Total: len(r.Coverage)}
		sized(&out.Coverage.Keywords, len(r.Coverage))
		for i, k := range r.Coverage {
			out.Coverage.Keywords[i].Text, out.Coverage.Keywords[i].Hit = k.Text, k.Hit
			if k.Hit {
				out.Coverage.Hit++
			}
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) RequestReciteAudioUpload(c *gin.Context, sessionID gen.SessionId) {
	var body gen.RequestReciteAudioUploadJSONBody
	if !bind(c, &body) {
		return
	}
	t, err := h.deps.Practice.ReciteAudioUpload(c.Request.Context(), currentUser(c), uint64(sessionID), string(body.ContentType), int64(body.Size))
	if err != nil {
		_ = c.Error(err)
		return
	}
	headers := t.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	c.JSON(http.StatusOK, gin.H{"object_key": t.ObjectKey, "upload_url": t.URL, "upload_headers": headers, "expires_at": t.ExpiresAt})
}

func (h *Handlers) FinishReciteSession(c *gin.Context, sessionID gen.SessionId) {
	s, err := h.deps.Practice.FinishRecite(c.Request.Context(), currentUser(c), uint64(sessionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.ReciteSummary{Remembered: s.Remembered, Vague: s.Vague, Forgot: s.Forgot, ForgotCount: s.Forgot, PreviousRemembered: s.PreviousRemembered}
	out.Schedule.Tomorrow, out.Schedule.TwoDays, out.Schedule.Later = s.Tomorrow, s.TwoDays, s.Later
	c.JSON(http.StatusOK, out)
}
