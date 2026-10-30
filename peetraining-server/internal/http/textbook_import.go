package http

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/rules"
)

func textbookImport(j dbq.ListTextbookImportsRow) gen.TextbookImport {
	return gen.TextbookImport{CanReupload: !j.Sealed && (j.Status == "uploading" || j.Status == "failed"), Id: int64(j.ID), RevisionId: int64(j.RevisionID), FileName: j.FileName, Format: j.Format, Status: gen.TextbookImportStatus(j.Status), PageCount: int(j.PageCount), CompletedPages: int(j.CompletedPages), UncertainPages: int(j.UncertainPages), Attempt: int(j.Attempt), FailureMessage: opt(j.FailureMessage), CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt}
}
func (h *Handlers) GetTextbookImportSettings(c *gin.Context) {
	l, err := h.deps.Textbooks.ImportSettings(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, l)
}
func (h *Handlers) ListTextbookImports(c *gin.Context, bookID, editionID, revisionID int64) {
	rows, err := h.deps.Textbooks.ListImports(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.TextbookImport, len(rows))
	for i, j := range rows {
		out[i] = textbookImport(j)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}
func (h *Handlers) GetTextbookImport(c *gin.Context, bookID, editionID, revisionID, importID int64) {
	rows, err := h.deps.Textbooks.ListImports(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	for _, j := range rows {
		if j.ID == uint64(importID) {
			c.JSON(http.StatusOK, textbookImport(j))
			return
		}
	}
	_ = c.Error(ErrNotFound())
}
func (h *Handlers) CreateTextbookImport(c *gin.Context, bookID, editionID, revisionID int64) {
	var body gen.TextbookImportCreate
	if !bind(c, &body) {
		return
	}
	j, upload, err := h.deps.Textbooks.CreateImport(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID), currentAdmin(c).ID, body.IdempotencyKey, rules.TextbookImportFile{Name: body.FileName, Format: string(body.Format), Size: body.Size, SHA256: body.Sha256})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.TextbookImportUpload{Job: gen.TextbookImport{CanReupload: !j.Sealed && (j.Status == "uploading" || j.Status == "failed"), Id: int64(j.ID), RevisionId: int64(j.RevisionID), FileName: j.FileName, Format: j.Format, Status: gen.TextbookImportStatus(j.Status), PageCount: int(j.PageCount), Attempt: int(j.Attempt), CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt}, UploadUrl: upload.URL, UploadHeaders: upload.Headers, ExpiresAt: upload.ExpiresAt})
}
func (h *Handlers) CommitTextbookImport(c *gin.Context, bookID, editionID, revisionID, importID int64) {
	var body gen.CommitTextbookImportJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Textbooks.QueueImport(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID), uint64(importID), body.IdempotencyKey, false); err != nil {
		_ = c.Error(err)
		return
	}
	h.GetTextbookImport(c, bookID, editionID, revisionID, importID)
}
func (h *Handlers) RetryTextbookImport(c *gin.Context, bookID, editionID, revisionID, importID int64) {
	var body gen.RetryTextbookImportJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Textbooks.QueueImport(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID), uint64(importID), body.IdempotencyKey, true); err != nil {
		_ = c.Error(err)
		return
	}
	h.GetTextbookImport(c, bookID, editionID, revisionID, importID)
}
func (h *Handlers) ApplyTextbookImport(c *gin.Context, bookID, editionID, revisionID, importID int64) {
	var body gen.TextbookImportApply
	if !bind(c, &body) {
		return
	}
	r, err := h.deps.Textbooks.ApplyImport(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID), uint64(importID), currentAdmin(c).ID, uint64(body.ExpectedVersion), string(body.Mode))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, textbookRevision(r))
}
func (h *Handlers) GetTextbookImportPage(c *gin.Context, bookID, editionID, revisionID, importID, pageNo int64) {
	p, url, err := h.deps.Textbooks.ImportPage(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID), uint64(importID), int(pageNo))
	if err != nil {
		_ = c.Error(err)
		return
	}
	blocks := make([]struct {
		Heading   string `json:"heading"`
		Text      string `json:"text"`
		Uncertain bool   `json:"uncertain"`
	}, len(p.Blocks))
	for i, b := range p.Blocks {
		blocks[i].Heading = b.Heading
		blocks[i].Text = b.Text
		blocks[i].Uncertain = b.Uncertain
	}
	c.JSON(http.StatusOK, gin.H{"page_no": p.PageNo, "printed_page": p.PrintedPage, "blank": p.Blank, "blocks": blocks, "image_url": url})
}

func (h *Handlers) RenewTextbookImportUpload(c *gin.Context, bookID, editionID, revisionID, importID int64) {
	j, upload, err := h.deps.Textbooks.RenewImportUpload(c.Request.Context(), uint64(bookID), uint64(editionID), uint64(revisionID), uint64(importID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.TextbookImportUpload{Job: gen.TextbookImport{CanReupload: !j.Sealed && (j.Status == "uploading" || j.Status == "failed"), Id: int64(j.ID), RevisionId: int64(j.RevisionID), FileName: j.FileName, Format: j.Format, Status: gen.TextbookImportStatus(j.Status), PageCount: int(j.PageCount), Attempt: int(j.Attempt), CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt}, UploadUrl: upload.URL, UploadHeaders: upload.Headers, ExpiresAt: upload.ExpiresAt})
}
