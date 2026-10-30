package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/textbook"
)

func textbookIndex(g dbq.TextbookIndexGeneration, completed int64) gen.TextbookIndex {
	var profile ai.EmbeddingProfile
	_ = json.Unmarshal(g.Profile, &profile)
	var params gen.TextbookRetrievalParameters
	_ = json.Unmarshal(g.SplitConfig, &params)
	return gen.TextbookIndex{Id: int64(g.ID), RevisionId: int64(g.RevisionID), SourceVersion: int64(g.SourceVersion), ContentHash: g.ContentHash, ProfileId: g.ProfileHash, Model: profile.Model, Dimensions: profile.Dimensions, Status: gen.TextbookIndexStatus(g.Status), ChunkCount: int(g.ChunkCount), CompletedChunks: int(completed), FailureMessage: g.FailureMessage, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt, Parameters: params}
}
func (h *Handlers) GetTextbookRetrievalSettings(c *gin.Context) {
	p, err := h.deps.Textbooks.RetrievalParameters(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, h.deps.Textbooks.RetrievalSettings(p))
}
func (h *Handlers) ListTextbookIndexes(c *gin.Context, book, edition, revision int64) {
	rows, err := h.deps.Textbooks.ListIndexes(c.Request.Context(), uint64(book), uint64(edition), uint64(revision))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.TextbookIndex, len(rows))
	for i, r := range rows {
		g := dbq.TextbookIndexGeneration{ID: r.ID, RevisionID: r.RevisionID, SourceVersion: r.SourceVersion, ContentHash: r.ContentHash, Profile: r.Profile, ProfileHash: r.ProfileHash, SplitConfig: r.SplitConfig, Status: r.Status, ChunkCount: r.ChunkCount, FailureMessage: r.FailureMessage, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
		out[i] = textbookIndex(g, r.CompletedChunks)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
func (h *Handlers) BuildTextbookIndex(c *gin.Context, book, edition, revision int64) {
	var in gen.BuildTextbookIndexJSONBody
	if !bind(c, &in) {
		return
	}
	g, err := h.deps.Textbooks.BuildIndex(c.Request.Context(), uint64(book), uint64(edition), uint64(revision), currentAdmin(c).ID, uint64(in.ExpectedVersion), in.IdempotencyKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookIndex(g, 0))
}
func (h *Handlers) RetryTextbookIndex(c *gin.Context, book, edition, revision, generation int64) {
	var in gen.RetryTextbookIndexJSONBody
	if !bind(c, &in) {
		return
	}
	g, err := h.deps.Textbooks.RetryIndex(c.Request.Context(), uint64(book), uint64(edition), uint64(revision), uint64(generation), in.IdempotencyKey)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookIndex(g, 0))
}

func (h *Handlers) SearchTextbookRevision(c *gin.Context, book, edition, revision int64) {
	var in gen.SearchTextbookRevisionJSONBody
	if !bind(c, &in) {
		return
	}
	out, err := h.deps.Textbooks.SearchRevision(c.Request.Context(), uint64(book), uint64(edition), uint64(revision), uint64(in.GenerationId), in.Query)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, out)
}
func (h *Handlers) PublishTextbookRevision(c *gin.Context, book, edition, revision int64) {
	var in gen.PublishTextbookRevisionJSONBody
	if !bind(c, &in) {
		return
	}
	b, err := h.deps.Textbooks.Publish(c.Request.Context(), uint64(book), uint64(edition), uint64(revision), currentAdmin(c).ID, textbook.PublishInput{Key: in.IdempotencyKey, RevisionVersion: uint64(in.ExpectedVersion), EditionVersion: uint64(in.ExpectedEditionVersion), Generation: uint64(in.GenerationId), Summary: in.Summary})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookBook(b))
}
func (h *Handlers) SetTextbookEditionAvailability(c *gin.Context, book, edition int64) {
	var in gen.TextbookAvailabilityChange
	if !bind(c, &in) {
		return
	}
	var expires *time.Time
	if v, err := in.LicenseExpiresAt.Get(); err == nil {
		expires = &v
	}
	b, err := h.deps.Textbooks.Availability(c.Request.Context(), uint64(book), uint64(edition), currentAdmin(c).ID, textbook.AvailabilityInput{Key: in.IdempotencyKey, Version: uint64(in.ExpectedVersion), Withdrawn: in.Withdrawn, Authorized: in.Authorized, ExcerptsEnabled: in.ExcerptsEnabled, LicenseNote: in.LicenseNote, Summary: in.Summary, ExpiresAt: expires})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookBook(b))
}
func (h *Handlers) SetTextbookDefaultEdition(c *gin.Context, book, edition int64) {
	var in gen.SetTextbookDefaultEditionJSONBody
	if !bind(c, &in) {
		return
	}
	b, err := h.deps.Textbooks.SetDefault(c.Request.Context(), uint64(book), uint64(edition), currentAdmin(c).ID, uint64(in.ExpectedBookVersion), uint64(in.ExpectedEditionVersion), in.IdempotencyKey, in.Enabled)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookBook(b))
}
func (h *Handlers) ListTextbookPublications(c *gin.Context, book int64) {
	rows, err := h.deps.Textbooks.Publications(c.Request.Context(), uint64(book))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.TextbookPublication, len(rows))
	for i, r := range rows {
		out[i] = gen.TextbookPublication{Id: int64(r.ID), EditionId: int64(r.EditionID), Action: r.Action, Summary: r.Summary, CreatedBy: int64(r.CreatedBy), CreatedAt: r.CreatedAt}
		if r.RevisionID.Valid {
			out[i].RevisionId = &r.RevisionID.Int64
		}
		if r.GenerationID.Valid {
			out[i].GenerationId = &r.GenerationID.Int64
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
