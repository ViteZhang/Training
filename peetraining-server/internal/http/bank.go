package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/gen"
)

// 模块 3 题库：3.1 知识点树、3.1b 题目、3.2 搜索、3.3 题目详情、3.4–3.6 知识点卡片与编辑、3.7 原文查看（T13）。

func (h *Handlers) GetBankOverview(c *gin.Context, subjectID gen.SubjectId) {
	o, err := h.deps.Bank.Overview(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.BankOverview{SubjectId: int64(o.SubjectID), BankId: int64(o.BankID), QuestionCount: o.Questions, KpCount: o.KPs,
		NeedsReviewCount: o.NeedsReview, MaterialCount: o.Materials, PaperCount: &o.Papers, IsEssay: &o.IsEssay}
	out.MasteryDistribution.Unlearned, out.MasteryDistribution.Learning = o.Unlearned, o.Learning
	out.MasteryDistribution.Consolidating, out.MasteryDistribution.Mastered = o.Consolidating, o.Mastered
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetKnowledgeTree(c *gin.Context, subjectID gen.SubjectId, p gen.GetKnowledgeTreeParams) {
	filter := "all"
	if p.Filter != nil {
		filter = string(*p.Filter)
	}
	roots, review, err := h.deps.Bank.Tree(c.Request.Context(), currentUser(c), uint64(subjectID), filter)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sections": toNodes(roots), "needs_review_count": review})
}

func toNodes(ns []*bank.Node) []gen.KnowledgeNode {
	out := make([]gen.KnowledgeNode, 0, len(ns))
	for _, n := range ns {
		g := gen.KnowledgeNode{Id: int64(n.ID), Level: gen.KnowledgeLevel(n.Level), Name: n.Name, Children: toNodes(n.Children)}
		if n.Level == "point" {
			st := gen.MasteryState(n.State)
			g.State, g.ExamCount, g.NeedsReview = &st, &n.ExamCount, &n.NeedsReview
		} else {
			g.KpCount, g.ConsolidatingCount = &n.KPCount, &n.Consolidating
		}
		if n.Level == "section" {
			avg := float32(n.AvgMastery)
			g.AvgMastery, g.IsWeak = &avg, &n.IsWeak
		}
		if n.Official {
			g.Official = &n.Official
		}
		out = append(out, g)
	}
	return out
}

func (h *Handlers) CreateKnowledgeNode(c *gin.Context, subjectID gen.SubjectId) {
	var req gen.KnowledgeNodeInput
	if !bind(c, &req) {
		return
	}
	in := bank.NodeInput{Level: string(req.Level), Name: req.Name}
	if req.OriginalText != nil {
		in.OriginalText = *req.OriginalText
	}
	if req.ParentId.IsSpecified() && !req.ParentId.IsNull() {
		p := uint64(req.ParentId.MustGet())
		in.ParentID = &p
	}
	d, err := h.deps.Bank.CreateNode(c.Request.Context(), currentUser(c), uint64(subjectID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toKPDetail(d))
}

func (h *Handlers) GetKnowledgePoint(c *gin.Context, id gen.KpId) {
	d, err := h.deps.Bank.KnowledgePoint(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toKPDetail(d))
}

func (h *Handlers) UpdateKnowledgePoint(c *gin.Context, id gen.KpId) {
	var req gen.KnowledgePointPatch
	if !bind(c, &req) {
		return
	}
	p := bank.KPPatch{Name: req.Name, OriginalText: req.OriginalText, NeedsReview: req.NeedsReview}
	if req.ParentId != nil {
		v := uint64(*req.ParentId)
		p.ParentID = &v
	}
	if req.RubricPoints != nil {
		p.RubricSet, p.Rubric = true, toRubricInput(*req.RubricPoints)
	}
	d, err := h.deps.Bank.UpdateKnowledgePoint(c.Request.Context(), currentUser(c), uint64(id), p)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toKPDetail(d))
}

func (h *Handlers) DeleteKnowledgePoint(c *gin.Context, id gen.KpId) {
	if err := h.deps.Bank.DeleteKnowledgePoint(c.Request.Context(), currentUser(c), uint64(id)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) MergeKnowledgePoint(c *gin.Context, id gen.KpId) {
	var req gen.MergeKnowledgePointJSONBody
	if !bind(c, &req) {
		return
	}
	d, err := h.deps.Bank.Merge(c.Request.Context(), currentUser(c), uint64(id), uint64(req.TargetId))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toKPDetail(d))
}

func (h *Handlers) SplitKnowledgePoint(c *gin.Context, id gen.KpId) {
	var req gen.SplitKnowledgePointJSONBody
	if !bind(c, &req) {
		return
	}
	parts := make([]bank.SplitPart, len(req.Parts))
	for i, p := range req.Parts {
		parts[i].Name = p.Name
		if p.OriginalText != nil {
			parts[i].OriginalText = *p.OriginalText
		}
	}
	ds, err := h.deps.Bank.Split(c.Request.Context(), currentUser(c), uint64(id), parts)
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.KnowledgePointDetail, len(ds))
	for i, d := range ds {
		items[i] = toKPDetail(d)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *Handlers) RegenerateExplanation(c *gin.Context, id gen.KpId) {
	d, err := h.deps.Bank.RegenerateExplanation(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toKPDetail(d))
}

func (h *Handlers) SelfAssessKnowledgePoint(c *gin.Context, id gen.KpId) {
	var req gen.SelfAssessKnowledgePointJSONBody
	if !bind(c, &req) {
		return
	}
	m, err := h.deps.Bank.SelfAssess(c.Request.Context(), currentUser(c), uint64(id), string(req.Level))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toMastery(m))
}

func toMastery(m bank.Mastery) gen.Mastery {
	g := gen.Mastery{M: float32(m.M), State: gen.MasteryState(m.State)}
	if m.NextReviewOn != nil {
		g.NextReviewOn = &openapi_types.Date{Time: m.NextReviewOn.Date()}
	}
	if m.LastSelfAssess != "" {
		l := gen.SelfAssessLevel(m.LastSelfAssess)
		g.LastSelfAssess = &l
	}
	return g
}

func toRubricPoints(rs []bank.RubricPoint) []gen.RubricPoint {
	out := make([]gen.RubricPoint, len(rs))
	for i, r := range rs {
		out[i] = gen.RubricPoint{Id: int64(r.ID), Seq: r.Seq, Content: r.Content, Origin: gen.Origin(r.Origin)}
		if r.Score != nil {
			s := float32(*r.Score)
			out[i].Score = &s
		}
		if len(r.Keywords) > 0 {
			kw := r.Keywords
			out[i].Keywords = &kw
		}
	}
	return out
}

func toRubricInput(rs []gen.RubricPointInput) []bank.RubricInput {
	out := make([]bank.RubricInput, len(rs))
	for i, r := range rs {
		out[i].Content = r.Content
		if r.Score != nil {
			v := float64(*r.Score)
			out[i].Score = &v
		}
		if r.Keywords != nil {
			out[i].Keywords = *r.Keywords
		}
	}
	return out
}

func toSource(s *bank.Source) *gen.SourceRef {
	if s == nil {
		return nil
	}
	g := &gen.SourceRef{MaterialId: int64(s.MaterialID), FileName: s.FileName}
	if s.Page > 0 {
		p := s.Page
		g.Page = &p
	}
	return g
}

func toKPDetail(d bank.KPDetail) gen.KnowledgePointDetail {
	out := gen.KnowledgePointDetail{Id: int64(d.ID), Level: gen.KnowledgeLevel(d.Level), Name: d.Name, Path: d.Path, Origin: gen.Origin(d.Origin),
		ExamCount: d.ExamCount, Mastery: toMastery(d.Mastery), RubricPoints: toRubricPoints(d.Rubric), RelatedQuestions: []gen.QuestionBrief{},
		NeedsReview: d.NeedsReview, Source: toSource(d.Source)}
	if out.Path == nil {
		out.Path = []string{}
	}
	if d.ParentID != nil {
		p := int64(*d.ParentID)
		out.ParentId = &p
	}
	if d.OriginalText != "" {
		out.OriginalText = &d.OriginalText
	}
	if d.AIExplanation != "" {
		out.AiExplanation = &d.AIExplanation
	}
	if d.Official {
		out.Official = &d.Official
	}
	for _, q := range d.Related {
		out.RelatedQuestions = append(out.RelatedQuestions, gen.QuestionBrief{Id: int64(q.ID), Qtype: gen.QuestionType(q.QType), Stem: q.Stem,
			Source: gen.QuestionSource(q.Source), ExamYear: q.ExamYear})
	}
	return out
}

func (h *Handlers) ListQuestions(c *gin.Context, subjectID gen.SubjectId, p gen.ListQuestionsParams) {
	f := bank.QuestionFilter{ExamYear: p.ExamYear}
	if p.Qtype != nil {
		f.QType = string(*p.Qtype)
	}
	if p.Source != nil {
		f.Source = string(*p.Source)
	}
	if p.KpId != nil {
		v := uint64(*p.KpId)
		f.KPID = &v
	}
	if p.Status != nil {
		f.Status = string(*p.Status)
	}
	if p.Sort != nil {
		f.Sort = string(*p.Sort)
	}
	if p.Limit != nil {
		f.Limit = *p.Limit
	}
	if p.Cursor != nil {
		n, err := strconv.Atoi(*p.Cursor)
		if err != nil || n < 0 {
			_ = c.Error(ErrBadRequest("cursor 不正确"))
			return
		}
		f.After = n
	}
	items, facets, next, err := h.deps.Bank.Questions(c.Request.Context(), currentUser(c), uint64(subjectID), f)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.QuestionPage{Items: make([]gen.QuestionSummary, len(items))}
	out.Facets.ByQtype, out.Facets.BySource, out.Facets.ExamYears = facets.ByQType, facets.BySource, facets.Years
	if out.Facets.ExamYears == nil {
		out.Facets.ExamYears = []int{}
	}
	for i, q := range items {
		g := gen.QuestionSummary{Id: int64(q.ID), Qtype: gen.QuestionType(q.QType), Stem: q.Stem, Source: gen.QuestionSource(q.Source),
			ExamYear: q.ExamYear, StatusTag: gen.QuestionStatusTag(q.StatusTag), AttemptCount: q.AttemptCount, Path: q.Path,
			Score: f32(q.Score), LastScore: f32(q.LastScore)}
		if g.Path == nil {
			g.Path = []string{}
		}
		if q.GeneratedFromKP != "" {
			g.GeneratedFromKp = &q.GeneratedFromKP
		}
		out.Items[i] = g
	}
	if next > 0 {
		s := strconv.Itoa(next)
		out.NextCursor = &s
	}
	c.JSON(http.StatusOK, out)
}

func f32(p *float64) *float32 {
	if p == nil {
		return nil
	}
	v := float32(*p)
	return &v
}

func f64(p *float32) *float64 {
	if p == nil {
		return nil
	}
	v := float64(*p)
	return &v
}

func toOptions(p *[]gen.ChoiceOption) *[]ai.Option {
	if p == nil {
		return nil
	}
	out := make([]ai.Option, len(*p))
	for i, o := range *p {
		out[i] = ai.Option{Key: o.Key, Text: o.Text}
	}
	return &out
}

func toKPIDs(p *[]int64) *[]uint64 {
	if p == nil {
		return nil
	}
	out := make([]uint64, len(*p))
	for i, v := range *p {
		out[i] = uint64(v)
	}
	return &out
}

func (h *Handlers) CreateQuestion(c *gin.Context, subjectID gen.SubjectId) {
	var req gen.QuestionInput
	if !bind(c, &req) {
		return
	}
	qtype := string(req.Qtype)
	in := bank.QuestionInput{QType: &qtype, Stem: &req.Stem, Options: toOptions(req.Options), Answer: req.Answer, Analysis: req.Analysis,
		Score: f64(req.Score), ExamYear: req.ExamYear, KPIDs: toKPIDs(req.KpIds)}
	if req.Source != nil {
		s := string(*req.Source)
		in.Source = &s
	}
	if req.RubricPoints != nil {
		r := toRubricInput(*req.RubricPoints)
		in.Rubric = &r
	}
	d, err := h.deps.Bank.CreateQuestion(c.Request.Context(), currentUser(c), uint64(subjectID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toQuestionDetail(d))
}

func (h *Handlers) GetQuestion(c *gin.Context, id gen.QuestionId) {
	d, err := h.deps.Bank.Question(c.Request.Context(), currentUser(c), uint64(id))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toQuestionDetail(d))
}

func (h *Handlers) UpdateQuestion(c *gin.Context, id gen.QuestionId) {
	var req gen.QuestionPatch
	if !bind(c, &req) {
		return
	}
	in := bank.QuestionInput{Stem: req.Stem, Options: toOptions(req.Options), Answer: req.Answer, Analysis: req.Analysis,
		Score: f64(req.Score), ScoreSet: req.Score != nil, KPIDs: toKPIDs(req.KpIds), NeedsReview: req.NeedsReview}
	if req.Qtype != nil {
		t := string(*req.Qtype)
		in.QType = &t
	}
	if req.ExamYear.IsSpecified() {
		in.ExamYearSet = true
		if !req.ExamYear.IsNull() {
			y := req.ExamYear.MustGet()
			in.ExamYear = &y
		}
	}
	if req.RubricPoints != nil {
		r := toRubricInput(*req.RubricPoints)
		in.Rubric = &r
	}
	d, err := h.deps.Bank.UpdateQuestion(c.Request.Context(), currentUser(c), uint64(id), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toQuestionDetail(d))
}

func (h *Handlers) DeleteQuestion(c *gin.Context, id gen.QuestionId) {
	if err := h.deps.Bank.DeleteQuestion(c.Request.Context(), currentUser(c), uint64(id)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func toQuestionDetail(d bank.QuestionDetail) gen.QuestionDetail {
	out := gen.QuestionDetail{Id: int64(d.ID), Qtype: gen.QuestionType(d.QType), Stem: d.Stem, Source: gen.QuestionSource(d.Source),
		ExamYear: d.ExamYear, Score: f32(d.Score), OriginTags: d.OriginTags, RubricPoints: toRubricPoints(d.Rubric), RubricVersion: d.RubricVersion,
		Attempts: []gen.AttemptHistory{}, InWrongBook: d.InWrongBook, NeedsReview: d.NeedsReview, SourceRef: toSource(d.SourceRef)}
	if d.Answer != "" {
		out.Answer = &d.Answer
	}
	if d.AnswerOrigin != "" {
		o := gen.Origin(d.AnswerOrigin)
		out.AnswerOrigin = &o
	}
	if d.Analysis != "" {
		out.Analysis = &d.Analysis
	}
	if len(d.Options) > 0 {
		opts := make([]gen.ChoiceOption, len(d.Options))
		for i, o := range d.Options {
			opts[i] = gen.ChoiceOption{Key: o.Key, Text: o.Text}
		}
		out.Options = &opts
	}
	if d.GeneratedFromKP != nil {
		v := int64(*d.GeneratedFromKP)
		out.GeneratedFromKpId = &v
	}
	if len(d.ReviewReasons) > 0 {
		rs := make([]gen.ReviewReason, len(d.ReviewReasons))
		for i, r := range d.ReviewReasons {
			rs[i] = gen.ReviewReason(r)
		}
		out.ReviewReasons = &rs
	}
	for _, k := range d.KPs {
		out.KnowledgePoints = append(out.KnowledgePoints, struct {
			Id        int64  `json:"id"`
			IsPrimary bool   `json:"is_primary"`
			Name      string `json:"name"`
		}{int64(k.ID), k.IsPrimary, k.Name})
	}
	if out.KnowledgePoints == nil {
		out.KnowledgePoints = []struct {
			Id        int64  `json:"id"`
			IsPrimary bool   `json:"is_primary"`
			Name      string `json:"name"`
		}{}
	}
	for _, a := range d.Attempts {
		g := gen.AttemptHistory{AttemptId: int64(a.ID), AnsweredAt: a.AnsweredAt, Score: f32(a.Score), FullScore: f32(a.FullScore), IsCorrect: a.IsCorrect}
		if len(a.Missed) > 0 {
			m := a.Missed
			g.MissedPoints = &m
		}
		if len(a.LossTypes) > 0 {
			lt := make([]gen.AttemptHistoryLossTypes, len(a.LossTypes))
			for i, t := range a.LossTypes {
				lt[i] = gen.AttemptHistoryLossTypes(t)
			}
			g.LossTypes = &lt
		}
		out.Attempts = append(out.Attempts, g)
	}
	return out
}

func (h *Handlers) GetMaterialPage(c *gin.Context, materialID gen.MaterialId, pageNo int, p gen.GetMaterialPageParams) {
	hl := ""
	if p.Highlight != nil {
		hl = *p.Highlight
	}
	pg, err := h.deps.Bank.MaterialPage(c.Request.Context(), currentUser(c), uint64(materialID), pageNo, hl)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.MaterialPage{MaterialId: int64(pg.MaterialID), FileName: pg.FileName, PageNo: pg.PageNo, PageCount: pg.PageCount, Text: pg.Text,
		Highlights: toRanges(pg.Highlights), LowConfidence: toRanges(pg.LowConfidence)}
	out.KnowledgePoints = []struct {
		Id   int64  `json:"id"`
		Name string `json:"name"`
	}{}
	for _, k := range pg.KPs {
		out.KnowledgePoints = append(out.KnowledgePoints, struct {
			Id   int64  `json:"id"`
			Name string `json:"name"`
		}{int64(k.ID), k.Name})
	}
	c.JSON(http.StatusOK, out)
}

func toRanges(rs []bank.Range) []gen.TextRange {
	out := make([]gen.TextRange, len(rs))
	for i, r := range rs {
		out[i] = gen.TextRange{Start: r.Start, End: r.End}
	}
	return out
}

func toHit(h bank.Hit) gen.SearchHit {
	return gen.SearchHit{Text: h.Text, Highlights: toRanges(h.Highlights)}
}

func (h *Handlers) SearchBank(c *gin.Context, subjectID gen.SubjectId, p gen.SearchBankParams) {
	r, err := h.deps.Bank.Search(c.Request.Context(), currentUser(c), uint64(subjectID), p.Q)
	if err != nil {
		_ = c.Error(err)
		return
	}
	var out gen.SearchResult
	out.KnowledgePoints = make([]struct {
		Hit  gen.SearchHit `json:"hit"`
		Id   int64         `json:"id"`
		Name string        `json:"name"`
		Path *[]string     `json:"path,omitempty"`
	}, len(r.KPs))
	for i, k := range r.KPs {
		path := k.Path
		out.KnowledgePoints[i].Hit, out.KnowledgePoints[i].Id, out.KnowledgePoints[i].Name, out.KnowledgePoints[i].Path = toHit(k.Hit), int64(k.ID), k.Name, &path
	}
	out.Questions = make([]struct {
		Hit   gen.SearchHit    `json:"hit"`
		Id    int64            `json:"id"`
		Qtype gen.QuestionType `json:"qtype"`
	}, len(r.Questions))
	for i, q := range r.Questions {
		out.Questions[i].Hit, out.Questions[i].Id, out.Questions[i].Qtype = toHit(q.Hit), int64(q.ID), gen.QuestionType(q.QType)
	}
	out.MaterialPages = make([]struct {
		FileName   string        `json:"file_name"`
		Hit        gen.SearchHit `json:"hit"`
		MaterialId int64         `json:"material_id"`
		PageNo     int           `json:"page_no"`
	}, len(r.Pages))
	for i, pg := range r.Pages {
		out.MaterialPages[i].FileName, out.MaterialPages[i].Hit, out.MaterialPages[i].MaterialId, out.MaterialPages[i].PageNo = pg.FileName, toHit(pg.Hit), int64(pg.MaterialID), pg.PageNo
	}
	c.JSON(http.StatusOK, out)
}
