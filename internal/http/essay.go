package http

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/essay"
	"peetraining-server/internal/gen"
)

// 模块 5 作文（T23）：5.1 作文训练、5.2 写作文、5.4 拍照上传（复用 /handwriting）、5.5 批改中、5.6 批改结果、5.7 范文详情、5.8 作文本、5.9 评分标准。

func optF32(v *float64) *float32 { return f32p(v) }

func sourceRef(materialID uint64, file string, page int) *gen.SourceRef {
	if materialID == 0 {
		return nil
	}
	r := gen.SourceRef{MaterialId: int64(materialID), FileName: file}
	if page > 0 {
		r.Page = ptr(page)
	}
	return &r
}

func modelStructure(raw json.RawMessage) *gen.ModelEssayStructure {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var st gen.ModelEssayStructure
	if json.Unmarshal(raw, &st) != nil {
		return nil
	}
	return &st
}

func toGenRubricInfo(r essay.RubricInfo) gen.EssayRubricInfo {
	return gen.EssayRubricInfo{Id: int64(r.ID), Name: r.Name, Source: gen.EssayRubricSource(r.Source), FullScore: float32(r.FullScore)}
}

func toGenDims(ds []essay.DimScore) []gen.EssayDimScore {
	out := make([]gen.EssayDimScore, len(ds))
	for i, d := range ds {
		out[i] = gen.EssayDimScore{Name: d.Name, Score: float32(d.Score), Max: float32(d.Max), Comment: optStr(d.Comment)}
	}
	return out
}

func toGenEssay(v essay.View) gen.Essay {
	out := gen.Essay{Id: int64(v.ID), SubjectId: int64(v.SubjectID), TopicSource: gen.EssayTopicSource(v.TopicSource), Topic: v.Topic, DraftNo: v.DraftNo,
		Content: v.Content, PhotoKeys: v.PhotoKeys, WordCount: v.WordCount, Timed: v.Timed, DurationSeconds: v.DurationSeconds, TimeLimitMinutes: v.TimeLimitMinutes,
		Status: gen.EssayStatus(v.Status), FailReason: optStr(v.FailReason), Score: optF32(v.Score), PrevDelta: optF32(v.PrevDelta), ScoreBefore: optF32(v.ScoreBefore),
		CountsForEstimate: v.CountsForEstimate, Disputed: v.Disputed, CreatedAt: v.CreatedAt, SubmittedAt: v.SubmittedAt, GradedAt: v.GradedAt,
		WeakestDimension: optStr(v.Weakest), Thesis: optStr(v.Thesis)}
	if v.TopicQuestionID != 0 {
		out.TopicQuestionId = ptr(int64(v.TopicQuestionID))
	}
	if v.AITopicID != 0 {
		out.AiTopicId = ptr(int64(v.AITopicID))
	}
	if v.ParentID != 0 {
		out.ParentEssayId = ptr(int64(v.ParentID))
	}
	if v.RequiredWords > 0 {
		out.RequiredWords = ptr(v.RequiredWords)
	}
	if v.Rubric != nil {
		r := toGenRubricInfo(*v.Rubric)
		out.Rubric = &r
		out.FullScore = ptr(float32(v.FullScore))
	}
	if v.Status == "graded" {
		dims := toGenDims(v.Dimensions)
		out.Dimensions = &dims
		out.Highlights, out.Problems, out.Suggestions, out.Paragraphs = ptr(nonNil(v.Highlights)), ptr(nonNil(v.Problems)), ptr(nonNil(v.Suggestions)), ptr(nonNil(v.Paragraphs))
		ann := make([]gen.EssayAnnotation, len(v.Annotations))
		for i, a := range v.Annotations {
			ann[i] = gen.EssayAnnotation{Paragraph: a.Paragraph, Quote: a.Quote, Issue: a.Issue, Suggestion: a.Suggestion}
		}
		out.Annotations = &ann
		models := make([]gen.ModelEssayRef, len(v.ModelEssays))
		for i, m := range v.ModelEssays {
			models[i] = gen.ModelEssayRef{Id: int64(m.ID), Title: m.Title, Structure: modelStructure(m.Structure)}
		}
		out.ModelEssays = &models
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func toGenEssayBrief(b essay.Brief) gen.EssayBrief {
	out := gen.EssayBrief{Id: int64(b.ID), Topic: b.Topic, TopicSource: gen.EssayTopicSource(b.TopicSource), DraftNo: b.DraftNo, Status: gen.EssayStatus(b.Status),
		Score: optF32(b.Score), WordCount: b.WordCount, CountsForEstimate: b.CountsForEstimate, CreatedAt: b.CreatedAt, GradedAt: b.GradedAt}
	if b.FullScore > 0 {
		out.FullScore = ptr(float32(b.FullScore))
	}
	return out
}

func toGenAITopic(t essay.AITopic, drafts *int) gen.EssayAITopic {
	return gen.EssayAITopic{Id: int64(t.ID), Topic: t.Topic, RequiredWords: t.RequiredWords, Note: optStr(t.Note), CreatedAt: t.CreatedAt, Drafts: drafts}
}

func toGenRubric(r essay.Rubric) gen.EssayRubric {
	out := gen.EssayRubric{Id: int64(r.ID), Name: r.Name, Source: gen.EssayRubricSource(r.Source), Origin: gen.EssayRubricOrigin(r.Origin), FullScore: float32(r.FullScore),
		Dimensions: make([]gen.EssayDimension, len(r.Dimensions)), SourceRef: sourceRef(r.MaterialID, r.FileName, r.Page)}
	for i, d := range r.Dimensions {
		gd := gen.EssayDimension{Name: d.Name, Description: optStr(d.Description), Score: float32(d.Score)}
		if len(d.Bands) > 0 {
			bands := make([]struct {
				Description string `json:"description"`
				Range       string `json:"range"`
			}, len(d.Bands))
			for j, b := range d.Bands {
				bands[j].Description, bands[j].Range = b.Description, b.Range
			}
			gd.Bands = &bands
		}
		out.Dimensions[i] = gd
	}
	return out
}

func toGenRubrics(rs essay.Rubrics) gen.EssayRubrics {
	out := gen.EssayRubrics{ActiveId: int64(rs.ActiveID), Generic: toGenRubric(rs.Generic)}
	if rs.User != nil {
		u := toGenRubric(*rs.User)
		out.User = &u
	}
	return out
}

func (h *Handlers) GetEssayHome(c *gin.Context, subjectID gen.SubjectId) {
	hm, err := h.deps.Essay.Home(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.EssayHome{WeeklyGoal: hm.WeeklyGoal, WeekDone: hm.WeekDone, AvgScore: optF32(hm.AvgScore), Rubric: toGenRubricInfo(hm.Rubric), NoMaterial: hm.NoMaterial,
		ExamTopics: make([]gen.EssayExamTopic, len(hm.ExamTopics)), AiTopics: make([]gen.EssayAITopic, len(hm.AITopics)), Drafts: make([]gen.EssayBrief, len(hm.Drafts))}
	if hm.WeeklyRemaining != nil {
		out.WeeklyRemaining = nullable.NewNullableWithValue(*hm.WeeklyRemaining)
	} else {
		out.WeeklyRemaining = nullable.NewNullNullable[int]()
	}
	for i, t := range hm.ExamTopics {
		et := gen.EssayExamTopic{Id: int64(t.ID), Stem: t.Stem, Drafts: t.Drafts, Best: optF32(t.Best), ModelEssayCount: t.ModelEssayCount}
		if t.ExamYear > 0 {
			et.ExamYear = ptr(t.ExamYear)
		}
		if t.RequiredWords > 0 {
			et.RequiredWords = ptr(t.RequiredWords)
		}
		out.ExamTopics[i] = et
	}
	for i, t := range hm.AITopics {
		out.AiTopics[i] = toGenAITopic(t.AITopic, ptr(t.Drafts))
	}
	for i, b := range hm.Drafts {
		out.Drafts[i] = toGenEssayBrief(b)
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GenerateEssayTopic(c *gin.Context, subjectID gen.SubjectId) {
	t, err := h.deps.Essay.GenerateTopic(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenAITopic(t, ptr(0)))
}

func deref64(p *int64) uint64 {
	if p == nil {
		return 0
	}
	return uint64(*p)
}

func (h *Handlers) CreateEssay(c *gin.Context) {
	var body gen.CreateEssayJSONBody
	if !bind(c, &body) {
		return
	}
	in := essay.CreateInput{SubjectID: deref64(body.SubjectId), QuestionID: deref64(body.QuestionId), AITopicID: deref64(body.AiTopicId), ParentID: deref64(body.ParentEssayId),
		Key: body.IdempotencyKey}
	if body.TopicSource != nil {
		in.Source = string(*body.TopicSource)
	}
	if body.TopicText != nil {
		in.TopicText = *body.TopicText
	}
	if body.RequiredWords != nil {
		in.RequiredWords = *body.RequiredWords
	}
	v, err := h.deps.Essay.Create(c.Request.Context(), currentUser(c), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenEssay(v))
}

func (h *Handlers) GetEssay(c *gin.Context, essayID gen.EssayId) {
	v, err := h.deps.Essay.Get(c.Request.Context(), currentUser(c), uint64(essayID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenEssay(v))
}

func (h *Handlers) SaveEssayDraft(c *gin.Context, essayID gen.EssayId) {
	var body gen.SaveEssayDraftJSONBody
	if !bind(c, &body) {
		return
	}
	v, err := h.deps.Essay.SaveDraft(c.Request.Context(), currentUser(c), uint64(essayID), essay.DraftInput{Content: body.Content, DurationSeconds: body.DurationSeconds,
		Timed: body.Timed, PhotoKeys: body.PhotoKeys})
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenEssay(v))
}

func (h *Handlers) SubmitEssay(c *gin.Context, essayID gen.EssayId) {
	v, err := h.deps.Essay.Submit(c.Request.Context(), currentUser(c), uint64(essayID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenEssay(v))
}

func (h *Handlers) DisputeEssay(c *gin.Context, essayID gen.EssayId) {
	var body gen.DisputeEssayJSONBody
	if !bind(c, &body) {
		return
	}
	note := ""
	if body.Note != nil {
		note = *body.Note
	}
	v, err := h.deps.Essay.Dispute(c.Request.Context(), currentUser(c), uint64(essayID), string(body.Reason), note)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenEssay(v))
}

func (h *Handlers) GetEssayNotebook(c *gin.Context, subjectID gen.SubjectId) {
	nb, err := h.deps.Essay.Notebook(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.EssayNotebook{Count: nb.Count, AvgScore: optF32(nb.AvgScore), Best: optF32(nb.Best), DimAvgs: toGenDims(nb.DimAvgs),
		Trend: make([]gen.EssayTrendPoint, len(nb.Trend)), Items: make([]gen.EssayBrief, len(nb.Items))}
	for i, t := range nb.Trend {
		out.Trend[i] = gen.EssayTrendPoint{EssayId: int64(t.EssayID), Date: t.Date, Score: float32(t.Score), FullScore: float32(t.FullScore)}
	}
	for i, b := range nb.Items {
		out.Items[i] = toGenEssayBrief(b)
	}
	if w := nb.Weakest; w != nil {
		out.Weakest = &gen.EssayWeakest{Name: w.Name, AvgScore: float32(w.AvgScore), AvgMax: float32(w.AvgMax), MethodTitle: optStr(w.MethodTitle)}
		if w.MethodID != 0 {
			out.Weakest.MethodId = ptr(int64(w.MethodID))
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetEssayRubrics(c *gin.Context, subjectID gen.SubjectId) {
	rs, err := h.deps.Essay.Rubrics(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenRubrics(rs))
}

func (h *Handlers) SelectEssayRubric(c *gin.Context, subjectID gen.SubjectId) {
	var body gen.SelectEssayRubricJSONBody
	if !bind(c, &body) {
		return
	}
	rs, err := h.deps.Essay.SelectRubric(c.Request.Context(), currentUser(c), uint64(subjectID), string(body.Source))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenRubrics(rs))
}

func (h *Handlers) UpdateEssayRubric(c *gin.Context, subjectID gen.SubjectId) {
	var body gen.UpdateEssayRubricJSONBody
	if !bind(c, &body) {
		return
	}
	dims := make([]ai.EssayDim, len(body.Dimensions))
	for i, d := range body.Dimensions {
		dims[i] = ai.EssayDim{Name: d.Name, Score: float64(d.Score)}
		if d.Description != nil {
			dims[i].Description = *d.Description
		}
		if d.Bands != nil {
			for _, b := range *d.Bands {
				dims[i].Bands = append(dims[i].Bands, ai.EssayBand{Range: b.Range, Description: b.Description})
			}
		}
	}
	rs, err := h.deps.Essay.UpdateRubric(c.Request.Context(), currentUser(c), uint64(subjectID), body.Name, dims)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenRubrics(rs))
}

func (h *Handlers) GetModelEssay(c *gin.Context, modelEssayID int64) {
	m, err := h.deps.Essay.ModelEssay(c.Request.Context(), currentUser(c), uint64(modelEssayID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.ModelEssayDetail{Id: int64(m.ID), Title: m.Title, Topic: optStr(m.Topic), Content: m.Content, Structure: modelStructure(m.Structure),
		SourceRef: sourceRef(m.MaterialID, m.FileName, m.Page)})
}
