package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/official"
)

// 官方题库（T30）：App 添加 / 移除官方题库（受 official_bank 开关控制），后台 7.10–7.14。

func init() {
	// 官方题库开关关闭时 App 侧接口返回 404（ADR 0009）。后台接口不受开关影响：内容团队要在开关打开之前把题库做好。
	flagGuards["GET "+APIPrefix+"/official-banks"] = "official_bank"
	flagGuards["PUT "+APIPrefix+"/official-banks/:bankId/subscription"] = "official_bank"
	flagGuards["DELETE "+APIPrefix+"/official-banks/:bankId/subscription"] = "official_bank"
}

func optID(v uint64) *int64 {
	if v == 0 {
		return nil
	}
	x := int64(v)
	return &x
}

func (h *Handlers) ListOfficialBanks(c *gin.Context) {
	bs, err := h.deps.Official.Banks(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.OfficialBank, len(bs))
	for i, b := range bs {
		out[i] = gen.OfficialBank{BankId: int64(b.BankID), Title: b.Title, School: b.School, Major: b.Major, SubjectCode: b.SubjectCode, SubjectName: b.SubjectName,
			Version: b.Version, KpCount: b.KPCount, QuestionCount: b.QuestionCount, AddedSubjectId: optID(b.AddedTo), SuggestedSubjectId: optID(b.Suggested)}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) SubscribeOfficialBank(c *gin.Context, bankID int64) {
	var body gen.SubscribeOfficialBankJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Official.Subscribe(c.Request.Context(), currentUser(c), uint64(bankID), uint64(body.SubjectId)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) UnsubscribeOfficialBank(c *gin.Context, bankID int64) {
	if err := h.deps.Official.Unsubscribe(c.Request.Context(), currentUser(c), uint64(bankID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- 后台 ----------

// canProject 判断后台账号能不能访问这个项目：内容负责人、数据分析（只读接口才放行，见契约 x-roles）看全部；内容编辑只看被分配的。
func (h *Handlers) canProject(c *gin.Context, projectID int64) bool {
	a := currentAdmin(c)
	if a.Has(admin.RoleContentLead, admin.RoleAnalyst) {
		return true
	}
	ok, err := h.deps.Official.IsEditorOf(c.Request.Context(), a.ID, uint64(projectID))
	if err != nil {
		_ = c.Error(err)
		return false
	}
	if !ok {
		_ = c.Error(apperr.NotFoundErr())
	}
	return ok
}

func toGenProject(p official.Project) gen.AdminOfficialProject {
	out := gen.AdminOfficialProject{Id: int64(p.ID), School: p.School, Major: p.Major, SubjectCode: p.SubjectCode, SubjectName: p.SubjectName, BankId: int64(p.BankID),
		Stage: gen.OfficialStage(p.Stage), EditorIds: make([]int64, len(p.EditorIDs)), Materials: make([]gen.OfficialMaterial, len(p.Materials)), KpCount: p.KPCount,
		QuestionCount: p.QuestionCount, ExamCount: p.ExamCount, Version: p.Version, Subscribers: p.Subscribers, CreatedAt: p.CreatedAt}
	for i, id := range p.EditorIDs {
		out.EditorIds[i] = int64(id)
	}
	for i, m := range p.Materials {
		out.Materials[i] = gen.OfficialMaterial{Name: m.Name, Basis: gen.OfficialMaterialBasis(m.Basis), Note: opt(m.Note)}
	}
	return out
}

func ids(xs []int64) []uint64 {
	out := make([]uint64, len(xs))
	for i, x := range xs {
		out[i] = uint64(x)
	}
	return out
}

func toMap(b json.RawMessage) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

func fromMap(m map[string]any) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}

func (h *Handlers) ListAdminDemand(c *gin.Context) {
	ds, err := h.deps.Official.Demands(c.Request.Context(), time.Now().UTC().AddDate(0, 0, -7))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminDemand, len(ds))
	for i, d := range ds {
		out[i] = gen.AdminDemand{SubjectCode: d.Code, Users: d.Users, AvgQuestions: d.AvgQuestions, WeekNew: d.WeekNew, WithTarget: d.WithTarget, PaidRate: float32(d.PaidRate),
			AvgReview: d.AvgReview, ProjectId: optID(d.ProjectID), ProjectStage: opt(d.ProjectStage), Unplanned: d.Unplanned}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) MarkAdminDemand(c *gin.Context, code string) {
	var body gen.MarkAdminDemandJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Official.MarkUnplanned(c.Request.Context(), currentAdmin(c).ID, code, body.Unplanned); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ListOfficialProjects(c *gin.Context) {
	ps, err := h.deps.Official.Projects(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	a := currentAdmin(c)
	out := []gen.AdminOfficialProject{}
	for _, p := range ps {
		mine := false
		for _, id := range p.EditorIDs {
			mine = mine || id == a.ID
		}
		if a.Has(admin.RoleContentLead, admin.RoleAnalyst) || mine {
			out = append(out, toGenProject(p))
		}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) CreateOfficialProject(c *gin.Context) {
	var body gen.CreateOfficialProjectJSONBody
	if !bind(c, &body) {
		return
	}
	var editors []uint64
	if body.EditorIds != nil {
		editors = ids(*body.EditorIds)
	}
	id, err := h.deps.Official.CreateProject(c.Request.Context(), currentAdmin(c).ID, body.School, body.Major, body.SubjectCode, body.SubjectName, editors)
	if err != nil {
		_ = c.Error(err)
		return
	}
	h.respondProject(c, id)
}

func (h *Handlers) respondProject(c *gin.Context, id uint64) {
	p, err := h.deps.Official.Project(c.Request.Context(), id)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenProject(p))
}

func (h *Handlers) GetOfficialProject(c *gin.Context, projectID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	h.respondProject(c, uint64(projectID))
}

func (h *Handlers) UpdateOfficialProject(c *gin.Context, projectID int64) {
	var body gen.UpdateOfficialProjectJSONBody
	if !bind(c, &body) {
		return
	}
	ms := make([]official.Material, len(body.Materials))
	for i, m := range body.Materials {
		ms[i] = official.Material{Name: m.Name, Basis: string(m.Basis), Note: deref(m.Note)}
	}
	if err := h.deps.Official.UpdateProject(c.Request.Context(), uint64(projectID), ids(body.EditorIds), ms, string(body.Stage)); err != nil {
		_ = c.Error(err)
		return
	}
	h.respondProject(c, uint64(projectID))
}

func (h *Handlers) ListOfficialItems(c *gin.Context, projectID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	its, err := h.deps.Official.Items(c.Request.Context(), uint64(projectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.OfficialItem, len(its))
	for i, it := range its {
		out[i] = gen.OfficialItem{EntityType: it.EntityType, Id: int64(it.ID), Title: it.Title, Status: it.Status, Payload: toMap(it.Payload)}
	}
	c.JSON(http.StatusOK, items(out))
}

func toGenPoints(ps []official.Point) []gen.OfficialPoint {
	out := make([]gen.OfficialPoint, len(ps))
	for i, p := range ps {
		out[i] = gen.OfficialPoint{Content: p.Content, Score: float32(p.Score)}
		if len(p.Keywords) > 0 {
			kw := p.Keywords
			out[i].Keywords = &kw
		}
	}
	return out
}

func (h *Handlers) ExtractOfficialKPs(c *gin.Context, projectID int64) {
	var body gen.ExtractOfficialKPsJSONBody
	if !bind(c, &body) || !h.canProject(c, projectID) {
		return
	}
	cs, err := h.deps.Official.KPCandidates(c.Request.Context(), uint64(projectID), body.Text)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.OfficialCandidate, len(cs))
	for i, x := range cs {
		out[i] = gen.OfficialCandidate{Section: x.Section, Chapter: x.Chapter, Name: x.Name, OriginalText: x.OriginalText, Rubric: toGenPoints(x.Rubric),
			Alignment: gen.OfficialCandidateAlignment(x.Alignment), ExistingId: optID(x.ExistingID), ExistingText: opt(x.ExistingText)}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) ExtractOfficialRubric(c *gin.Context) {
	var body gen.ExtractOfficialRubricJSONBody
	if !bind(c, &body) {
		return
	}
	score := 10.0
	if body.Score != nil {
		score = float64(*body.Score)
	}
	ps, err := h.deps.Official.RubricCandidates(c.Request.Context(), string(body.Qtype), body.Stem, body.Answer, score)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, items(toGenPoints(ps)))
}

func toGenDraft(d official.Draft) gen.OfficialDraft {
	return gen.OfficialDraft{Id: int64(d.ID), ProjectId: int64(d.ProjectID), EditorId: int64(d.EditorID), EntityType: d.EntityType, EntityId: optID(d.EntityID),
		ChangeType: d.ChangeType, Payload: toMap(d.Payload), Status: gen.OfficialDraftStatus(d.Status), RejectReason: opt(d.RejectReason), SubmittedAt: d.SubmittedAt, CreatedAt: d.CreatedAt}
}

func (h *Handlers) ListOfficialDrafts(c *gin.Context, projectID int64, params gen.ListOfficialDraftsParams) {
	if !h.canProject(c, projectID) {
		return
	}
	status := ""
	if params.Status != nil {
		status = string(*params.Status)
	}
	var editor uint64
	// 内容编辑只看自己的草稿；内容负责人可以看全部或只看自己的。
	if a := currentAdmin(c); !a.Has(admin.RoleContentLead) || (params.Mine != nil && *params.Mine) {
		editor = a.ID
	}
	ds, err := h.deps.Official.Drafts(c.Request.Context(), uint64(projectID), status, editor)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.OfficialDraft, len(ds))
	for i, d := range ds {
		out[i] = toGenDraft(d)
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) saveDraft(c *gin.Context, projectID, draftID int64) {
	var body gen.CreateOfficialDraftJSONBody
	if !bind(c, &body) || !h.canProject(c, projectID) {
		return
	}
	var entity uint64
	if body.EntityId != nil {
		entity = uint64(*body.EntityId)
	}
	d, err := h.deps.Official.SaveDraft(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), uint64(draftID), string(body.EntityType), string(body.ChangeType), entity, fromMap(body.Payload))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenDraft(d))
}

func (h *Handlers) CreateOfficialDraft(c *gin.Context, projectID int64) { h.saveDraft(c, projectID, 0) }

func (h *Handlers) UpdateOfficialDraft(c *gin.Context, projectID, draftID int64) {
	h.saveDraft(c, projectID, draftID)
}

func (h *Handlers) DeleteOfficialDraft(c *gin.Context, projectID, draftID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	if err := h.deps.Official.DeleteDraft(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), uint64(draftID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) SubmitOfficialDraft(c *gin.Context, projectID, draftID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	mode, err := h.deps.Official.Submit(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), uint64(draftID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.OfficialSubmitResult{Mode: gen.OfficialSubmitResultMode(mode)})
}

func (h *Handlers) ListOfficialReviews(c *gin.Context, params gen.ListOfficialReviewsParams) {
	var pid uint64
	if params.ProjectId != nil {
		pid = uint64(*params.ProjectId)
	}
	rs, err := h.deps.Official.Reviews(c.Request.Context(), pid)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.OfficialReview, len(rs))
	for i, r := range rs {
		out[i] = gen.OfficialReview{Id: int64(r.ID), DraftId: int64(r.DraftID), ProjectId: int64(r.ProjectID), EditorId: int64(r.EditorID), Mode: r.Mode, EntityType: r.EntityType,
			EntityId: optID(r.EntityID), ChangeType: r.ChangeType, Payload: toMap(r.Payload), CreatedAt: r.CreatedAt}
		if len(r.Before) > 0 {
			b := toMap(r.Before)
			out[i].Before = &b
		}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) DecideOfficialReview(c *gin.Context, reviewID int64) {
	var body gen.DecideOfficialReviewJSONBody
	if !bind(c, &body) {
		return
	}
	var edited json.RawMessage
	if body.EditedPayload != nil {
		edited = fromMap(*body.EditedPayload)
	}
	if err := h.deps.Official.Decide(c.Request.Context(), currentAdmin(c).ID, uint64(reviewID), string(body.Decision), deref(body.Reason), edited); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func toGenChanges(cs []official.Change) []gen.OfficialChange {
	out := make([]gen.OfficialChange, len(cs))
	for i, x := range cs {
		out[i] = gen.OfficialChange{DraftId: int64(x.DraftID), EntityType: x.EntityType, ChangeType: x.ChangeType, EntityId: int64(x.EntityID), Title: x.Title, RubricChanged: x.RubricChanged}
	}
	return out
}

func toGenVersion(v official.Version) gen.OfficialVersion {
	return gen.OfficialVersion{Id: int64(v.ID), Version: v.Version, Changes: toGenChanges(v.Changes), PublishedAt: v.PublishedAt, RolledBackAt: v.RolledBackAt}
}

func (h *Handlers) PreviewOfficialRelease(c *gin.Context, projectID int64) {
	p, err := h.deps.Official.ReleasePreview(c.Request.Context(), uint64(projectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.OfficialReleasePreview{Changes: toGenChanges(p.Changes), Checks: make([]gen.OfficialCheck, len(p.Checks)), NextVersion: p.NextVersion, Notice: p.Notice, Subscribers: p.Subscribers}
	for i, ch := range p.Checks {
		out.Checks[i] = gen.OfficialCheck{Name: ch.Name, Passed: ch.Passed, Detail: opt(ch.Detail)}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) ListOfficialReleases(c *gin.Context, projectID int64) {
	if !h.canProject(c, projectID) {
		return
	}
	vs, err := h.deps.Official.Versions(c.Request.Context(), uint64(projectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.OfficialVersion, len(vs))
	for i, v := range vs {
		out[i] = toGenVersion(v)
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) PublishOfficialRelease(c *gin.Context, projectID int64) {
	var body gen.PublishOfficialReleaseJSONBody
	if !bind(c, &body) {
		return
	}
	v, err := h.deps.Official.Publish(c.Request.Context(), currentAdmin(c).ID, uint64(projectID), deref(body.Version))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenVersion(v))
}

func (h *Handlers) RollbackOfficialRelease(c *gin.Context, projectID, versionID int64) {
	if err := h.deps.Official.Rollback(c.Request.Context(), uint64(projectID), uint64(versionID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
