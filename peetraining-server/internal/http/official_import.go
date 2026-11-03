package http

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/official"
)

// T47 后台 AI 解析导入（PRD v3 增补 7.17）。内容编辑只能访问分配给自己的项目（canProject）。

func toGenImportFile(f official.ImportFile) gen.OfficialImportFile {
	out := gen.OfficialImportFile{Id: int64(f.ID), Kind: gen.OfficialImportFileKind(f.Kind), FileName: f.FileName, Format: gen.OfficialImportFileFormat(f.Format),
		Status: gen.OfficialImportFileStatus(f.Status), FailReason: optStr(f.FailReason), QuestionCount: f.QuestionCount, KpCount: f.KPCount,
		ReviewedCount: f.ReviewedCount, CreatedAt: f.CreatedAt, UploadUrl: optStr(f.UploadURL)}
	if f.ExamYear > 0 {
		out.ExamYear = ptr(f.ExamYear)
	}
	if len(f.UploadHeaders) > 0 {
		h := f.UploadHeaders
		out.UploadHeaders = &h
	}
	return out
}

func (h *Handlers) AdminListOfficialImports(c *gin.Context, projectID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	rows, err := h.deps.Official.ImportFiles(c.Request.Context(), uint64(projectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.OfficialImportFile, len(rows))
	for i, r := range rows {
		out[i] = toGenImportFile(r)
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) AdminCreateOfficialImport(c *gin.Context, projectID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	var body gen.AdminCreateOfficialImportJSONBody
	if !bind(c, &body) {
		return
	}
	in := official.ImportFileInput{Kind: string(body.Kind), Format: string(body.Format)}
	if body.FileName != nil {
		in.FileName = *body.FileName
	}
	if body.Size != nil {
		in.Size = *body.Size
	}
	if body.Text != nil {
		in.Text = *body.Text
	}
	if body.ExamYear != nil {
		in.ExamYear = *body.ExamYear
	}
	if body.IsRecollection != nil {
		in.Recollection = *body.IsRecollection
	}
	f, err := h.deps.Official.CreateImportFile(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenImportFile(f))
}

func (h *Handlers) AdminConfirmOfficialImport(c *gin.Context, projectID, fileID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	if err := h.deps.Official.ConfirmImportFile(c.Request.Context(), uint64(projectID), uint64(fileID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) AdminGetOfficialImport(c *gin.Context, projectID, fileID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	d, err := h.deps.Official.ImportItems(c.Request.Context(), uint64(projectID), uint64(fileID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	type citation struct {
		ReferenceID uint64 `json:"reference_id"`
		Title       string `json:"title"`
		Heading     string `json:"heading"`
	}
	type item struct {
		ID             uint64          `json:"id"`
		ItemType       string          `json:"item_type"`
		Seq            int             `json:"seq"`
		Payload        json.RawMessage `json:"payload"`
		Citations      []citation      `json:"citations,omitempty"`
		SourceText     *string         `json:"source_text,omitempty"`
		Page           int             `json:"page"`
		Confidence     string          `json:"confidence"`
		Status         string          `json:"status"`
		DraftID        *int64          `json:"draft_id,omitempty"`
		RubricMismatch bool            `json:"rubric_mismatch"`
	}
	out := make([]item, len(d.Items))
	for i, it := range d.Items {
		cs := make([]citation, len(it.Citations))
		for j, ct := range it.Citations {
			cs[j] = citation{ReferenceID: ct.ReferenceID, Title: ct.Title, Heading: ct.Heading}
		}
		out[i] = item{ID: it.ID, ItemType: it.ItemType, Seq: it.Seq, Payload: it.Payload, Citations: cs, SourceText: optStr(it.SourceText), Page: it.Page,
			Confidence: it.Confidence, Status: it.Status, DraftID: optID(it.DraftID), RubricMismatch: it.RubricMismatch}
	}
	c.JSON(http.StatusOK, gin.H{"file": toGenImportFile(d.File), "text": d.Text, "items": out})
}

func (h *Handlers) AdminReviewOfficialImportItem(c *gin.Context, projectID, itemID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	var body gen.AdminReviewOfficialImportItemJSONBody
	if !bind(c, &body) {
		return
	}
	var edited json.RawMessage
	if body.Payload != nil {
		edited = fromMap(*body.Payload)
	}
	id, err := h.deps.Official.ReviewImportItem(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), uint64(itemID), string(body.Decision), edited)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"draft_id": optID(id)})
}
