package http

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	"strconv"
	"time"
)

func projectBookMatches(rows []dbq.ListProjectTextbooksRow) []gen.UserTextbookMatch {
	out := []gen.UserTextbookMatch{}
	for _, v := range rows {
		out = append(out, gen.UserTextbookMatch{Input: v.Title, Title: v.Title, Author: v.Author, Edition: v.Edition, EditionId: int64(v.ID), BookId: int64(v.BookID), CatalogVersion: strconv.FormatUint(v.CatalogVersion, 10), Method: "explicit", Available: v.Status == "published" && v.Authorized && (!v.LicenseExpiresAt.Valid || v.LicenseExpiresAt.Time.After(time.Now().UTC()))})
	}
	return out
}
func (h *Handlers) ListProjectTextbooks(c *gin.Context, projectID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	rows, version, err := h.deps.Official.ProjectTextbooks(c.Request.Context(), uint64(projectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": projectBookMatches(rows), "version": version})
}
func (h *Handlers) SetProjectTextbooks(c *gin.Context, projectID int64) {
	var in gen.SetProjectTextbooksJSONBody
	if !bind(c, &in) || !h.canProject(c, projectID) {
		return
	}
	ids := []uint64{}
	for _, v := range in.EditionIds {
		ids = append(ids, uint64(v))
	}
	if err := h.deps.Official.SetProjectTextbooks(c.Request.Context(), uint64(projectID), ids, uint64(in.ExpectedVersion), in.IdempotencyKey); err != nil {
		_ = c.Error(err)
		return
	}
	h.ListProjectTextbooks(c, projectID)
}
