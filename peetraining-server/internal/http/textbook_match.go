package http

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/rules"
	"strconv"
)

// 仅公开身份元数据，不提供原件、片段或全库浏览入口。
func (h *Handlers) MatchSubjectTextbooks(c *gin.Context, subjectID int64) {
	var req gen.MatchSubjectTextbooksJSONBody
	if !bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	user := currentUser(c)
	sub, err := h.deps.Profile.Subject(ctx, user, uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := []gen.UserTextbookMatch{}
	enabled, err := h.deps.Flags.Enabled(ctx, flags.TextbookRetrieval, user)
	if err != nil || !enabled {
		c.JSON(http.StatusOK, gin.H{"items": out})
		return
	}
	names := profile.Textbooks(sub.Textbooks)
	if req.Titles != nil {
		names = *req.Titles
	}
	var selections []rules.TextbookSelection
	_ = json.Unmarshal(sub.TextbookSelections, &selections)
	for _, name := range names {
		ids := []uint64{}
		method := gen.UserTextbookMatchMethod("auto")
		for _, v := range selections {
			if v.Input == name {
				ids = append(ids, v.EditionID)
				method = "explicit"
			}
		}
		matchNames := []string{name}
		if len(ids) > 0 {
			matchNames = nil
		}
		rows, err := h.deps.Textbooks.MatchEditions(ctx, matchNames, sub.Name, ids)
		if err != nil {
			continue
		}
		for _, v := range rows {
			out = append(out, gen.UserTextbookMatch{Input: name, BookId: int64(v.BookID), EditionId: int64(v.ID), Title: v.Title, Author: v.Author, Edition: v.Edition, Method: method, CatalogVersion: strconv.FormatUint(v.Version, 10), Available: true})
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
