package http

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/rules"
)

func (h *Handlers) candidateResponse(c *gin.Context, row dbq.AnswerUpdateCandidate) (map[string]any, error) {
	out := map[string]any{"id": row.ID, "question_id": row.QuestionID, "status": row.Status, "failure_message": row.FailureMessage, "created_at": row.CreatedAt, "updated_at": row.UpdatedAt}
	if row.Answer.Valid {
		out["answer"] = row.Answer.String
		out["points"] = json.RawMessage(row.Points)
	}
	if row.SnapshotID.Valid {
		snapshot, err := h.deps.AnswerUpdates.Snapshot(c.Request.Context(), currentUser(c), uint64(row.SnapshotID.Int64))
		if err != nil {
			return nil, err
		}
		out["snapshot"] = snapshot
	}
	return out, nil
}
func (h *Handlers) CreateAnswerUpdateCandidate(c *gin.Context, question int64) {
	var in gen.CreateAnswerUpdateCandidateJSONBody
	if !bind(c, &in) {
		return
	}
	row, err := h.deps.AnswerUpdates.Create(c.Request.Context(), currentUser(c), uint64(question), in.IdempotencyKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out, err := h.candidateResponse(c, row)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, out)
}
func (h *Handlers) GetAnswerUpdateCandidate(c *gin.Context, question, candidate int64) {
	row, err := h.deps.AnswerUpdates.Get(c.Request.Context(), currentUser(c), uint64(question), uint64(candidate))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out, err := h.candidateResponse(c, row)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, out)
}
func (h *Handlers) ListAnswerUpdateCandidates(c *gin.Context, question int64) {
	rows, err := h.deps.AnswerUpdates.List(c.Request.Context(), currentUser(c), uint64(question))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := []any{}
	for _, row := range rows {
		out, err := h.candidateResponse(c, row)
		if err != nil {
			_ = c.Error(err)
			return
		}
		items = append(items, out)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
func (h *Handlers) AdoptAnswerUpdateCandidate(c *gin.Context, question, candidate int64) {
	var in gen.AdoptAnswerUpdateCandidateJSONBody
	if !bind(c, &in) {
		return
	}
	h.decideAnswerCandidate(c, question, candidate, in.IdempotencyKey, true)
}
func (h *Handlers) DiscardAnswerUpdateCandidate(c *gin.Context, question, candidate int64) {
	var in gen.DiscardAnswerUpdateCandidateJSONBody
	if !bind(c, &in) {
		return
	}
	h.decideAnswerCandidate(c, question, candidate, in.IdempotencyKey, false)
}
func (h *Handlers) decideAnswerCandidate(c *gin.Context, question, candidate int64, key string, adopt bool) {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		_ = c.Error(err)
		return
	}
	row, err := h.deps.AnswerUpdates.Decide(c.Request.Context(), currentUser(c), uint64(question), uint64(candidate), key, adopt)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out, err := h.candidateResponse(c, row)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, out)
}
func (h *Handlers) GetQuestionAnswerEvidence(c *gin.Context, question int64) {
	snapshot, err := h.deps.AnswerUpdates.QuestionSnapshot(c.Request.Context(), currentUser(c), uint64(question))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshot": snapshot})
}

func (h *Handlers) GetGradingAnswerEvidence(c *gin.Context, id int64) {
	snapshot, err := h.deps.AnswerUpdates.GradingSnapshot(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshot": snapshot})
}
func (h *Handlers) GetImportAnswerEvidence(c *gin.Context, id int64) {
	snapshot, err := h.deps.AnswerUpdates.ImportSnapshot(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshot": snapshot})
}
