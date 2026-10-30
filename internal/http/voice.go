package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/practice"
)

func (h *Handlers) RequestVoiceUpload(c *gin.Context) {
	var b gen.RequestVoiceUploadJSONBody
	if !bind(c, &b) {
		return
	}
	v, err := h.deps.Practice.RequestVoiceUpload(c.Request.Context(), currentUser(c), string(b.ContentType), int64(b.Size))
	if err != nil {
		_ = c.Error(err)
		return
	}
	headers := v.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	c.JSON(http.StatusOK, gen.VoiceUpload{ObjectKey: v.ObjectKey, UploadUrl: v.URL, UploadHeaders: headers, ExpiresAt: v.ExpiresAt})
}
func toGenVoice(v practice.VoiceTranscription) gen.VoiceTranscription {
	return gen.VoiceTranscription{Id: int64(v.ID), CaptureOrder: &v.CaptureOrder, IdempotencyKey: v.Key, ObjectKey: v.ObjectKey, Status: gen.VoiceTranscriptionStatus(v.Status), Text: v.Text, Error: optStr(v.Error)}
}
func (h *Handlers) ListPracticeVoice(c *gin.Context, sessionID gen.SessionId, questionID gen.QuestionId) {
	list, err := h.deps.Practice.ListPracticeVoice(c.Request.Context(), currentUser(c), uint64(sessionID), uint64(questionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.VoiceTranscription, len(list))
	for i, v := range list {
		items[i] = toGenVoice(v)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
func (h *Handlers) StartPracticeVoice(c *gin.Context, sessionID gen.SessionId, questionID gen.QuestionId) {
	var b gen.StartPracticeVoiceJSONBody
	if !bind(c, &b) {
		return
	}
	var order int64
	if b.CaptureOrder != nil {
		order = *b.CaptureOrder
	}
	v, err := h.deps.Practice.StartPracticeVoice(c.Request.Context(), currentUser(c), uint64(sessionID), uint64(questionID), b.IdempotencyKey, b.ObjectKey, order)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenVoice(v))
}
func (h *Handlers) DiscardPracticeVoice(c *gin.Context, sessionID gen.SessionId, questionID gen.QuestionId, voiceID int64) {
	if err := h.deps.Practice.DiscardPracticeVoice(c.Request.Context(), currentUser(c), uint64(sessionID), uint64(questionID), uint64(voiceID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
