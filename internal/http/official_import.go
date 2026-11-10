package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/official"
	"peetraining-server/internal/rules"
)

// T47 后台 AI 解析导入（PRD v3 增补 7.17）。内容编辑只能访问分配给自己的项目（canProject）。

func toGenImportFile(f official.ImportFile) gen.OfficialImportFile {
	out := gen.OfficialImportFile{Id: int64(f.ID), Kind: gen.OfficialImportFileKind(f.Kind), FileName: f.FileName, Format: gen.OfficialImportFileFormat(f.Format),
		Status: gen.OfficialImportFileStatus(f.Status), FailReason: optStr(f.FailReason), QuestionCount: f.QuestionCount, KpCount: f.KPCount,
		ReviewedCount: f.ReviewedCount, CreatedAt: f.CreatedAt, UploadUrl: optStr(f.UploadURL), AutoAnswer: ptr(f.AutoAnswer)}
	st := f.Fill.Status
	if st == "" {
		st = "idle"
	}
	out.Fill = &struct {
		Done   int                              `json:"done"`
		Failed int                              `json:"failed"`
		Status gen.OfficialImportFileFillStatus `json:"status"`
		Total  int                              `json:"total"`
	}{Done: f.Fill.Done, Failed: f.Fill.Failed, Status: gen.OfficialImportFileFillStatus(st), Total: f.Fill.Total}
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
	if body.AutoAnswer != nil {
		in.AutoAnswer = *body.AutoAnswer
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
	out := make([]importItemJSON, len(d.Items))
	for i, it := range d.Items {
		out[i] = toImportItemJSON(it)
	}
	c.JSON(http.StatusOK, gin.H{"file": toGenImportFile(d.File), "text": d.Text, "items": out})
}

type importCitationJSON struct {
	ReferenceID uint64 `json:"reference_id"`
	Title       string `json:"title"`
	Heading     string `json:"heading"`
	Kind        string `json:"kind,omitempty"`
	Quote       string `json:"quote,omitempty"`
	Claim       string `json:"claim,omitempty"`
	Page        int    `json:"page,omitempty"`
	PrintedPage string `json:"printed_page,omitempty"`
	Material    string `json:"material,omitempty"`
}

// importItemJSON 是 OfficialImportItem：payload 按原样输出（结构同 7.12 草稿），不经生成的类型转一遍。
type importItemJSON struct {
	ID             uint64               `json:"id"`
	ItemType       string               `json:"item_type"`
	Seq            int                  `json:"seq"`
	Payload        json.RawMessage      `json:"payload"`
	Citations      []importCitationJSON `json:"citations,omitempty"`
	SourceText     *string              `json:"source_text,omitempty"`
	Page           int                  `json:"page"`
	Confidence     string               `json:"confidence"`
	Status         string               `json:"status"`
	DraftID        *int64               `json:"draft_id,omitempty"`
	RubricMismatch bool                 `json:"rubric_mismatch"`
	NeedsFill      bool                 `json:"needs_fill"`
	FillError      *string              `json:"fill_error,omitempty"`
	DuplicateOf    *string              `json:"duplicate_of,omitempty"`
	SeenYears      []int                `json:"seen_years,omitempty"`
	AnswerAI       bool                 `json:"answer_ai,omitempty"`
	Filling        bool                 `json:"filling,omitempty"`
	AnswerBasis    *answerBasisJSON     `json:"answer_basis,omitempty"`
	MissingKPs     []string             `json:"missing_kps,omitempty"`
	ExistingKP     bool                 `json:"existing_kp,omitempty"`
}

type answerBasisJSON struct {
	Reason  string   `json:"reason"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

func toImportItemJSON(it official.ImportItem) importItemJSON {
	cs := make([]importCitationJSON, len(it.Citations))
	for j, ct := range it.Citations {
		cs[j] = importCitationJSON{ReferenceID: ct.ReferenceID, Title: ct.Title, Heading: ct.Heading, Kind: ct.Kind, Quote: ct.Quote, Claim: ct.Claim,
			Page: ct.Page, PrintedPage: ct.PrintedPage, Material: ct.Material}
	}
	out := importItemJSON{ID: it.ID, ItemType: it.ItemType, Seq: it.Seq, Payload: it.Payload, Citations: cs, SourceText: optStr(it.SourceText), Page: it.Page,
		Confidence: it.Confidence, Status: it.Status, DraftID: optID(it.DraftID), RubricMismatch: it.RubricMismatch, NeedsFill: it.NeedsFill,
		FillError: optStr(it.FillError), DuplicateOf: optStr(it.Duplicate), SeenYears: it.SeenYears, AnswerAI: it.AnswerAI, Filling: it.Filling,
		MissingKPs: it.MissingKPs, ExistingKP: it.ExistingKP}
	if b := it.AnswerBasis; b != nil && it.AnswerAI { // 只引用了考纲、经验总结时也说明（T55）
		out.AnswerBasis = &answerBasisJSON{Reason: b.Reason, Message: ai.BasisMessage(b.Reason), Details: b.Details}
	}
	return out
}

// AdminRegenerateOfficialImport 整份按关联教材重新生成（T53）。
func (h *Handlers) AdminRegenerateOfficialImport(c *gin.Context, projectID, fileID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	f, err := h.deps.Official.RegenerateImportFile(c.Request.Context(), uint64(projectID), uint64(fileID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenImportFile(f))
}

// AdminRegenerateOfficialImportItem 按关联教材重新生成一道题（T53）。
func (h *Handlers) AdminRegenerateOfficialImportItem(c *gin.Context, projectID, itemID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	it, err := h.deps.Official.RegenerateImportItem(c.Request.Context(), uint64(projectID), uint64(itemID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportItemJSON(it))
}

// AdminCreateOfficialFramework 用项目已关联的资料生成知识框架（T54）。
func (h *Handlers) AdminCreateOfficialFramework(c *gin.Context, projectID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	var body gen.AdminCreateOfficialFrameworkJSONBody
	if !bind(c, &body) {
		return
	}
	load := func(ctx context.Context, book, edition, revision uint64) ([]rules.TextbookSection, error) {
		r, err := h.deps.Textbooks.GetRevision(ctx, book, edition, revision)
		return r.Sections, err
	}
	f, err := h.deps.Official.CreateFrameworkImport(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), uint64(body.EditionId), load)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenImportFile(f))
}

// AdminDeleteOfficialImport 删除一份导入的资料（重新导入前清理）。
func (h *Handlers) AdminDeleteOfficialImport(c *gin.Context, projectID, fileID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	if err := h.deps.Official.DeleteImportFile(c.Request.Context(), uint64(projectID), uint64(fileID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// AdminRejectOfficialImportDuplicates 一键驳回标为重复的题（T52）。
func (h *Handlers) AdminRejectOfficialImportDuplicates(c *gin.Context, projectID, fileID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	n, err := h.deps.Official.RejectDuplicates(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), uint64(fileID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"rejected": n})
}

// AdminFillOfficialImport 一键补全（T49）。
func (h *Handlers) AdminFillOfficialImport(c *gin.Context, projectID, fileID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	f, err := h.deps.Official.FillImportFile(c.Request.Context(), uint64(projectID), uint64(fileID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenImportFile(f))
}

// AdminFillOfficialImportItem 生成答案 / 提取采分点（T49）。
func (h *Handlers) AdminFillOfficialImportItem(c *gin.Context, projectID, itemID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	it, err := h.deps.Official.FillImportItem(c.Request.Context(), uint64(projectID), uint64(itemID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportItemJSON(it))
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
