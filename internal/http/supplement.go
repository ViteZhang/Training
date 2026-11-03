package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
)

// T48 3.1e 补充资料提示（PRD v3 增补）。

func (h *Handlers) GetSupplementPrompt(c *gin.Context, subjectID gen.SubjectId) {
	p, err := h.deps.Bank.SupplementPrompt(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	if p == nil {
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	c.JSON(http.StatusOK, gin.H{"prompt": gin.H{"reason": p.Reason, "section_id": optID(p.SectionID), "section_name": optStr(p.SectionName),
		"official_count": p.Official, "attempts": p.Attempts, "score_rate": p.ScoreRate}})
}

func (h *Handlers) DismissSupplementPrompt(c *gin.Context, subjectID gen.SubjectId) {
	var body gen.DismissSupplementPromptJSONBody
	if !bind(c, &body) {
		return
	}
	var section uint64
	if body.SectionId != nil {
		section = uint64(*body.SectionId)
	}
	if err := h.deps.Bank.DismissSupplementPrompt(c.Request.Context(), currentUser(c), uint64(subjectID), string(body.Reason), section); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
