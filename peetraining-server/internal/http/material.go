package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/material"
)

// 1.5 上传资料、1.5b 粘贴文字、3.1c/3.1d/6.3 资料列表与删除、额度（T08）。

func (h *Handlers) GetQuota(c *gin.Context) {
	items, member, err := h.deps.Quota.Summary(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.QuotaSummary{Membership: gen.MembershipStatus{IsMember: member}, Items: make([]gen.QuotaItem, len(items))}
	for i, it := range items {
		q := gen.QuotaItem{QuotaType: gen.QuotaType(it.Type), Used: it.Used, Period: gen.QuotaItemPeriod(it.Period)}
		if it.Limit == nil {
			q.Limit = nullable.NewNullNullable[int]()
		} else {
			q.Limit = nullable.NewNullableWithValue(*it.Limit)
		}
		if !it.ResetsAt.IsZero() {
			t := it.ResetsAt
			q.ResetsAt = &t
		}
		out.Items[i] = q
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) CreateUploadRequests(c *gin.Context, _ gen.CreateUploadRequestsParams) {
	var req gen.UploadRequest
	if !bind(c, &req) {
		return
	}
	files := make([]material.File, len(req.Files))
	for i, f := range req.Files {
		files[i] = material.File{Name: f.FileName, Format: string(f.Format), Size: f.SizeBytes, SHA256: f.Sha256}
		if f.PageCount != nil {
			files[i].PageCount = *f.PageCount
		}
		if f.ContentType != nil {
			files[i].ContentType = *f.ContentType
		}
	}
	targets, err := h.deps.Material.RequestUploads(c.Request.Context(), currentUser(c), uint64(req.SubjectId), category(req.Category), files, req.RightConfirmed)
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.UploadTarget, len(targets))
	for i, t := range targets {
		items[i] = gen.UploadTarget{Index: t.Index, MaterialId: int64(t.MaterialID)}
		if t.Duplicate {
			dup := true
			items[i].Duplicate = &dup
		}
		if t.Upload != nil {
			u, exp, hdr := t.Upload.URL, t.Upload.ExpiresAt, t.Upload.Headers
			items[i].UploadUrl, items[i].ExpiresAt, items[i].UploadHeaders = &u, &exp, &hdr
		}
	}
	c.JSON(http.StatusOK, gen.UploadRequestResponse{Items: items})
}

func (h *Handlers) ConfirmMaterialUploaded(c *gin.Context, id gen.MaterialId) {
	m, err := h.deps.Material.ConfirmUploaded(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toMaterial(m))
}

func (h *Handlers) CreatePastedMaterial(c *gin.Context, _ gen.CreatePastedMaterialParams) {
	var req gen.PasteMaterialRequest
	if !bind(c, &req) {
		return
	}
	title := ""
	if req.Title != nil {
		title = *req.Title
	}
	m, err := h.deps.Material.CreatePasted(c.Request.Context(), currentUser(c), uint64(req.SubjectId), category(req.Category), title, req.Text, req.RightConfirmed)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toMaterial(m))
}

func (h *Handlers) ListMaterials(c *gin.Context, subjectID gen.SubjectId) {
	list, err := h.deps.Material.List(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.Material, len(list))
	for i, m := range list {
		g := toMaterial(dbq.GetMaterialRow{
			ID: m.ID, Category: m.Category, SubType: m.SubType, FileName: m.FileName, Format: m.Format, PageCount: m.PageCount,
			BilledPages: m.BilledPages, Status: m.Status, FailReason: m.FailReason, QuestionCount: m.QuestionCount, KpCount: m.KpCount, CreatedAt: m.CreatedAt,
		})
		g.SubjectId = subjectID
		papers, review := int(m.PaperCount), int(m.NeedsReviewCount)
		g.PaperCount, g.NeedsReviewCount = &papers, &review
		items[i] = g
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handlers) GetMaterial(c *gin.Context, id gen.MaterialId) {
	m, err := h.deps.Material.Get(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toMaterial(m))
}

func (h *Handlers) GetMaterialDeletionImpact(c *gin.Context, id gen.MaterialId) {
	im, err := h.deps.Material.DeletionImpact(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.MaterialDeletionImpact{
		QuestionCount: im.Questions, AttemptCount: im.Attempts, WrongCount: im.Wrong,
		KpDeleteCount: im.KPDelete, KpKeepCount: im.KPKeep, PaperSessionCount: im.PaperSessions,
	})
}

func (h *Handlers) DeleteMaterial(c *gin.Context, id gen.MaterialId) {
	if err := h.deps.Material.Delete(c.Request.Context(), currentUser(c), uint64(id)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func category(c *gen.MaterialCategory) string {
	if c == nil {
		return ""
	}
	return string(*c)
}

func toMaterial(m dbq.GetMaterialRow) gen.Material {
	g := gen.Material{
		Id: int64(m.ID), SubjectId: m.SubjectID.Int64, FileName: m.FileName, Format: gen.MaterialFormat(m.Format),
		Status: gen.MaterialStatus(m.Status), PageCount: int(m.PageCount), BilledPages: int(m.BilledPages),
		QuestionCount: int(m.QuestionCount), KpCount: int(m.KpCount), CreatedAt: m.CreatedAt,
	}
	if m.Category.Valid {
		cat := gen.MaterialCategory(m.Category.MaterialsCategory)
		g.Category = &cat
	}
	if m.SubType.Valid {
		g.SubType = &m.SubType.String
	}
	if m.FailReason.Valid {
		g.FailReason = &m.FailReason.String
	}
	return g
}
