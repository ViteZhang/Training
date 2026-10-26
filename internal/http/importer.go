package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/rules"
)

// 1.5–1.8 导入流程（T10）。

func (h *Handlers) CreateImportJob(c *gin.Context, _ gen.CreateImportJobParams) {
	var req gen.CreateImportJobRequest
	if !bind(c, &req) {
		return
	}
	ids := make([]uint64, len(req.MaterialIds))
	for i, id := range req.MaterialIds {
		ids[i] = uint64(id)
	}
	if req.Mode == gen.ImportModeAnswers {
		_ = c.Error(ErrBadRequest("补充答案文件请用 1.7c 的入口"))
		return
	}
	j, err := h.deps.Importer.CreateJob(c.Request.Context(), currentUser(c), uint64(req.SubjectId), string(req.Mode), ids)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toImportJob(j))
}

func (h *Handlers) ListImportJobs(c *gin.Context, p gen.ListImportJobsParams) {
	list, err := h.deps.Importer.List(c.Request.Context(), currentUser(c), p.Active != nil && *p.Active)
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.ImportJob, len(list))
	for i, j := range list {
		items[i] = toImportJob(j)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handlers) GetImportJob(c *gin.Context, id gen.JobId) {
	j, err := h.deps.Importer.Get(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportJob(j))
}

func (h *Handlers) RetryImportMaterial(c *gin.Context, jobID gen.JobId, materialID gen.MaterialId) {
	j, err := h.deps.Importer.Retry(c.Request.Context(), currentUser(c), uint64(jobID), uint64(materialID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportJob(j))
}

func (h *Handlers) RemoveImportMaterial(c *gin.Context, jobID gen.JobId, materialID gen.MaterialId) {
	j, err := h.deps.Importer.Remove(c.Request.Context(), currentUser(c), uint64(jobID), uint64(materialID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportJob(j))
}

func (h *Handlers) ListImportItems(c *gin.Context, jobID gen.JobId, p gen.ListImportItemsParams) {
	filter, limit, after := "all", 20, uint64(0)
	if p.Filter != nil {
		filter = string(*p.Filter)
	}
	if p.Limit != nil {
		limit = *p.Limit
	}
	if p.Cursor != nil {
		v, err := strconv.ParseUint(*p.Cursor, 10, 64)
		if err != nil {
			_ = c.Error(ErrBadRequest("cursor 不正确"))
			return
		}
		after = v
	}
	ctx := c.Request.Context()
	items, counts, err := h.deps.Importer.Items(ctx, currentUser(c), uint64(jobID), filter, after, limit+1)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.ImportItemPage{Counts: toCounts(counts), Items: []gen.ImportItem{}}
	if len(items) > limit {
		items = items[:limit]
		next := strconv.FormatUint(items[len(items)-1].ID, 10)
		out.NextCursor = &next
	}
	groups, err := h.deps.Importer.Groups(ctx, currentUser(c), uint64(jobID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out.Groups = toImportGroups(groups)
	names := h.fileNames(c)
	for _, it := range items {
		out.Items = append(out.Items, toImportItem(dbq.GetImportItemRow{
			ID: it.ID, JobID: it.JobID, MaterialID: it.MaterialID, ItemType: it.ItemType, Payload: it.Payload, Confidence: it.Confidence,
			Status: it.Status, NeedsReview: it.NeedsReview, ReviewReasons: it.ReviewReasons, DuplicateOfQuestionID: it.DuplicateOfQuestionID,
		}, names))
	}
	c.JSON(http.StatusOK, out)
}

// fileNames 按需查资料文件名（出处显示「文件 + 页码」），同一请求内缓存。
func (h *Handlers) fileNames(c *gin.Context) func(uint64) string {
	cache := map[uint64]string{}
	return func(id uint64) string {
		if n, ok := cache[id]; ok {
			return n
		}
		m, err := h.deps.Material.Get(c.Request.Context(), currentUser(c), id)
		if err == nil {
			cache[id] = m.FileName
		}
		return cache[id]
	}
}

func (h *Handlers) GetImportItem(c *gin.Context, id gen.ItemId) {
	it, err := h.deps.Importer.GetItem(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportItem(it, h.fileNames(c)))
}

func (h *Handlers) UpdateImportItem(c *gin.Context, id gen.ItemId) {
	var req gen.ImportItemPatch
	if !bind(c, &req) {
		return
	}
	var p importer.Patch
	if req.Status != nil {
		p.Status = string(*req.Status)
	}
	if req.Question != nil {
		p.Question = &importer.QuestionDraft{}
		if !recode(c, req.Question, p.Question) {
			return
		}
	}
	if req.KnowledgePoint != nil {
		p.KP = &importer.KPDraft{}
		if !recode(c, req.KnowledgePoint, p.KP) {
			return
		}
	}
	it, err := h.deps.Importer.UpdateItem(c.Request.Context(), currentUser(c), uint64(id), p)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportItem(it, h.fileNames(c)))
}

func (h *Handlers) GenerateImportItemAnswer(c *gin.Context, id gen.ItemId, _ gen.GenerateImportItemAnswerParams) {
	it, err := h.deps.Importer.GenerateAnswer(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toImportItem(it, h.fileNames(c)))
}

func (h *Handlers) ConfirmImportJob(c *gin.Context, id gen.JobId, _ gen.ConfirmImportJobParams) {
	var req gen.ConfirmImportJobJSONBody
	if c.Request.ContentLength > 0 && !bind(c, &req) {
		return
	}
	var ids []uint64
	if req.ItemIds != nil {
		for _, x := range *req.ItemIds {
			ids = append(ids, uint64(x))
		}
	}
	r, err := h.deps.Importer.Confirm(c.Request.Context(), currentUser(c), uint64(id), ids)
	if err != nil {
		_ = c.Error(err)
		return
	}
	without := make([]int64, len(r.SubjectsWithoutImport))
	for i, s := range r.SubjectsWithoutImport {
		without[i] = int64(s)
	}
	c.JSON(http.StatusOK, gen.ConfirmImportResult{QuestionCount: r.Questions, KpCount: r.KPs, NeedsReviewCount: r.NeedsReview,
		PaperCount: &r.Papers, PlanReady: r.PlanReady, SubjectsWithoutImport: &without})
}

// recode 在契约类型与服务端草稿之间转换（JSON 字段一致）。
func recode(c *gin.Context, from, to any) bool {
	b, err := json.Marshal(from)
	if err == nil {
		err = json.Unmarshal(b, to)
	}
	if err != nil {
		_ = c.Error(ErrBadRequest("内容格式不正确").Wrap(err))
		return false
	}
	return true
}

// 预计耗时（dev-spec 第六节时效目标）：文字类每 100 页 3 分钟，拍照每张 10 秒。
func etaSeconds(m dbq.ListImportJobMaterialsRow) int {
	if m.Status != dbq.ImportJobMaterialsStatusPending && m.Status != dbq.ImportJobMaterialsStatusRunning {
		return 0
	}
	if m.Format == dbq.MaterialsFormatImage {
		return 10
	}
	return max(int(m.BilledPages), int(m.ReservedPages), 1) * 18 / 10
}

func toImportJob(j importer.Job) gen.ImportJob {
	out := gen.ImportJob{Id: int64(j.ID), SubjectId: j.SubjectID.Int64, Mode: gen.ImportMode(j.Mode), Status: gen.ImportJobStatus(j.Status),
		ReservedPages: int(j.ReservedPages), Counts: toCounts(j.Counts), CreatedAt: j.CreatedAt, Materials: []gen.ImportJobMaterial{}}
	if j.DetectedEssay {
		out.DetectedEssay = &j.DetectedEssay
	}
	if j.TargetJobID.Valid {
		out.TargetJobId = &j.TargetJobID.Int64
	}
	billed := int(j.BilledPages)
	out.BilledPages = &billed
	if j.FinishedAt.Valid {
		t := j.FinishedAt.Time
		out.FinishedAt = &t
	}
	eta := 0
	for _, m := range j.Materials {
		gm := gen.ImportJobMaterial{MaterialId: int64(m.MaterialID), FileName: m.FileName, Step: gen.ImportStep(m.Step),
			Status: gen.ImportJobMaterialStatus(m.Status)}
		if m.FailReason.Valid {
			gm.FailReason = &m.FailReason.String
		}
		n := int(m.RecognizedCount)
		gm.RecognizedCount = &n
		if e := etaSeconds(m); e > 0 {
			gm.EtaSeconds = &e
			eta += e
		}
		out.Materials = append(out.Materials, gm)
	}
	if eta > 0 {
		out.EtaSeconds = &eta
	}
	return out
}

func toCounts(c dbq.CountImportItemsRow) gen.ImportCounts {
	essay := int(c.EssayItems)
	matched, missing := int(c.AnswerMatched), int(c.MissingAnswer)
	return gen.ImportCounts{Questions: int(c.Questions), KnowledgePoints: int(c.KnowledgePoints), NeedsReview: int(c.NeedsReview),
		Subjective: int(c.Subjective), Objective: int(c.Objective), Confirmed: int(c.Confirmed), EssayItems: &essay,
		AnswerMatched: &matched, MissingAnswer: &missing}
}

func toImportItem(it dbq.GetImportItemRow, fileName func(uint64) string) gen.ImportItem {
	out := gen.ImportItem{Id: int64(it.ID), JobId: int64(it.JobID), ItemType: gen.ImportItemType(it.ItemType), Status: gen.ImportItemStatus(it.Status),
		NeedsReview: it.NeedsReview, ReviewReasons: []gen.ReviewReason{}}
	var reasons []string
	_ = json.Unmarshal(it.ReviewReasons, &reasons)
	for _, r := range reasons {
		out.ReviewReasons = append(out.ReviewReasons, gen.ReviewReason(r))
	}
	if f, err := strconv.ParseFloat(it.Confidence.String, 32); err == nil && it.Confidence.Valid {
		f32 := float32(f)
		out.Confidence = &f32
	}
	if it.DuplicateOfQuestionID.Valid {
		out.DuplicateOfQuestionId = &it.DuplicateOfQuestionID.Int64
	}
	page := 0
	switch it.ItemType {
	case dbq.ImportItemsItemTypeQuestion:
		var d importer.QuestionDraft
		_ = json.Unmarshal(it.Payload, &d)
		page = d.SourcePage
		out.Question = &gen.QuestionDraft{}
		_ = json.Unmarshal(it.Payload, out.Question)
		// 答案档位与出处（PRD v3 11.16）：「来自你的资料 · 文件 + 页码」
		tier := gen.AnswerTier(rules.TierOf(d.Answer, d.AnswerOrigin, d.Source))
		out.Question.AnswerTier = &tier
		if d.Answer != "" && d.AnswerMaterialID != 0 && d.AnswerOrigin == "imported" {
			src := gen.SourceRef{MaterialId: int64(d.AnswerMaterialID), FileName: fileName(d.AnswerMaterialID)}
			if d.AnswerPage > 0 {
				p := d.AnswerPage
				src.Page = &p
			}
			out.Question.AnswerSource = &src
		}
	case dbq.ImportItemsItemTypeKnowledgePoint:
		var d importer.KPDraft
		_ = json.Unmarshal(it.Payload, &d)
		page = d.SourcePage
		out.KnowledgePoint = &gen.KnowledgePointDraft{}
		_ = json.Unmarshal(it.Payload, out.KnowledgePoint)
	default:
		m := map[string]any{}
		_ = json.Unmarshal(it.Payload, &m)
		if p, ok := m["page"].(float64); ok {
			page = int(p)
		}
		out.Essay = &m
	}
	if it.MaterialID.Valid {
		src := gen.SourceRef{MaterialId: it.MaterialID.Int64, FileName: fileName(uint64(it.MaterialID.Int64))}
		if page > 0 {
			src.Page = &page
		}
		out.Source = &src
	}
	return out
}

func (h *Handlers) DeleteImportItems(c *gin.Context, jobID gen.JobId) {
	var req gen.IdList
	if !bind(c, &req) {
		return
	}
	n, counts, err := h.deps.Importer.DeleteItems(c.Request.Context(), currentUser(c), uint64(jobID), uint64s(req.Ids))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.DeleteImportItemsResult{Deleted: n, Counts: toCounts(counts)})
}

func uint64s(in []int64) []uint64 {
	out := make([]uint64, len(in))
	for i, v := range in {
		out[i] = uint64(v)
	}
	return out
}

func (h *Handlers) KeepImportGroups(c *gin.Context, jobID gen.JobId) {
	var req gen.KeepImportGroupsJSONRequestBody
	if !bind(c, &req) {
		return
	}
	ctx, user := c.Request.Context(), currentUser(c)
	d, r, err := h.deps.Importer.KeepGroups(ctx, user, uint64(jobID), req.Labels)
	if err != nil {
		_ = c.Error(err)
		return
	}
	_, counts, err := h.deps.Importer.Items(ctx, user, uint64(jobID), "all", 0, 1)
	if err != nil {
		_ = c.Error(err)
		return
	}
	groups, err := h.deps.Importer.Groups(ctx, user, uint64(jobID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.KeepGroupsResult{Deleted: d, Restored: r, Counts: toCounts(counts), Groups: *toImportGroups(groups)})
}

func toImportGroups(in []importer.GroupSummary) *[]gen.ImportGroup {
	out := make([]gen.ImportGroup, len(in))
	for i, g := range in {
		out[i] = gen.ImportGroup{Label: g.Label, Total: g.Total, Kept: g.Kept, Matches: g.Matches}
		if g.Majors != "" {
			m := g.Majors
			out[i].Majors = &m
		}
	}
	return &out
}

// ---------- 1.7c 补充答案文件（PRD v3 1.7c，10 月 26 日更新） ----------

func (h *Handlers) CreateAnswerFileJob(c *gin.Context, subjectID gen.SubjectId) {
	var req gen.CreateAnswerFileJobJSONBody
	if !bind(c, &req) {
		return
	}
	var target uint64
	if req.TargetJobId != nil {
		target = uint64(*req.TargetJobId)
	}
	j, err := h.deps.Importer.CreateAnswerJob(c.Request.Context(), currentUser(c), uint64(subjectID), target, uint64s(req.MaterialIds))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toImportJob(j))
}

func toPairingTarget(t importer.PairingTarget) gen.PairingTarget {
	out := gen.PairingTarget{Kind: gen.PairingTargetKind(t.Kind), Id: int64(t.ID), Qtype: gen.QuestionType(t.QType), Stem: t.Stem, ExamYear: t.ExamYear}
	if t.QuestionNo != "" {
		out.QuestionNo = &t.QuestionNo
	}
	return out
}

func toPairingAnswer(a importer.PairingAnswer) gen.PairingAnswer {
	out := gen.PairingAnswer{Id: int64(a.ID), Text: a.Text, ExamYear: a.ExamYear,
		Source: gen.SourceRef{MaterialId: int64(a.Source.MaterialID), FileName: a.Source.FileName}}
	if a.Source.Page > 0 {
		p := a.Source.Page
		out.Source.Page = &p
	}
	if a.QuestionNo != "" {
		out.QuestionNo = &a.QuestionNo
	}
	return out
}

func (h *Handlers) ListAnswerPairings(c *gin.Context, jobID gen.JobId) {
	r, err := h.deps.Importer.Pairings(c.Request.Context(), currentUser(c), uint64(jobID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AnswerPairingResult{Job: toImportJob(r.Job), Pairings: []gen.AnswerPairing{}, StillMissing: []gen.PairingTarget{}, SpareAnswers: []gen.PairingAnswer{}}
	for _, p := range r.Pairings {
		gp := gen.AnswerPairing{Id: int64(p.ID), Target: toPairingTarget(p.Target), Method: gen.AnswerPairingMethod(p.Method), Status: gen.AnswerPairingStatus(p.Status)}
		if p.Answer != nil {
			a := toPairingAnswer(*p.Answer)
			gp.Answer = &a
		}
		out.Pairings = append(out.Pairings, gp)
	}
	for _, t := range r.StillMissing {
		out.StillMissing = append(out.StillMissing, toPairingTarget(t))
	}
	for _, a := range r.Spare {
		out.SpareAnswers = append(out.SpareAnswers, toPairingAnswer(a))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) UpdateAnswerPairing(c *gin.Context, jobID gen.JobId, pairingID int64) {
	var req gen.UpdateAnswerPairingJSONBody
	if !bind(c, &req) {
		return
	}
	var answer uint64
	if v, err := req.AnswerId.Get(); err == nil {
		answer = uint64(v)
	}
	if err := h.deps.Importer.UpdatePairing(c.Request.Context(), currentUser(c), uint64(jobID), uint64(pairingID), answer); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ConfirmAnswerPairings(c *gin.Context, jobID gen.JobId) {
	n, err := h.deps.Importer.ConfirmPairings(c.Request.Context(), currentUser(c), uint64(jobID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"applied": n})
}
