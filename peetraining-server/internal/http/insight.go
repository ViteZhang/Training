package http

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/bank"
	"peetraining-server/internal/gen"
)

// 3.8 考情分析、3.9 知识图谱、3.10 作文知识库（T14）。

// sized 把生成代码里的匿名结构体切片设为 n 个元素，再按下标填字段。
func sized[S ~[]E, E any](s *S, n int) { *s = make(S, n) }

func (h *Handlers) GetExamProfile(c *gin.Context, subjectID gen.SubjectId) {
	p, err := h.deps.Bank.ExamProfile(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.ExamProfile{Ready: p.Ready, PaperCount: p.PaperCount, MinPapers: p.MinPapers, Years: p.Years, ExcludedCount: p.Excluded, StyleTags: p.StyleTags}
	if out.Years == nil {
		out.Years = []int{}
	}
	if out.StyleTags == nil {
		out.StyleTags = []string{}
	}
	sized(&out.Basis, len(p.Basis))
	for i, b := range p.Basis {
		out.Basis[i].MaterialId, out.Basis[i].FileName = int64(b.MaterialID), b.FileName
	}
	sized(&out.Structure, len(p.Structure))
	for i, st := range p.Structure {
		x := &out.Structure[i]
		x.Qtype, x.Count, x.ScoreEach, x.Total = gen.QuestionType(st.QType), st.Count, float32(st.ScoreEach), float32(st.Total)
		if i < len(p.StructureTimes) {
			x.SuggestedMinutes = p.StructureTimes[i]
		}
	}
	sized(&out.Sections, len(p.Sections))
	for i, s := range p.Sections {
		x := &out.Sections[i]
		x.Id, x.Name, x.Share, x.Mastery, x.KpCount = int64(s.ID), s.Name, float32(s.Share), float32(s.Mastery), s.KPCount
	}
	sized(&out.MissingSections, len(p.Missing))
	for i, s := range p.Missing {
		x := &out.MissingSections[i]
		x.Id, x.Name, x.Share, x.Mastery, x.KpCount = int64(s.ID), s.Name, float32(s.Share), float32(s.Mastery), s.KPCount
	}
	sized(&out.HighFreq, len(p.HighFreq))
	for i, k := range p.HighFreq {
		x := &out.HighFreq[i]
		x.KpId, x.Name, x.Path, x.ExamCount, x.State = int64(k.KPID), k.Name, k.Path, k.ExamCount, gen.MasteryState(k.State)
		if x.Path == nil {
			x.Path = []string{}
		}
	}
	if p.Ready {
		out.StableYears, out.ChangedYears, out.HighFreqTotal = &p.StableYears, &p.ChangedYears, &p.HighFreqTotal
		out.TotalMinutes, out.CheckMinutes = &p.TotalMinutes, &p.CheckMinutes
	}
	c.JSON(http.StatusOK, out)
}

func toGenRelation(r bank.Relation) gen.KnowledgeRelation {
	return gen.KnowledgeRelation{Id: int64(r.ID), SourceId: int64(r.Source), TargetId: int64(r.Target), RelationType: gen.RelationType(r.Type), Origin: gen.Origin(r.Origin)}
}

func (h *Handlers) GetKnowledgeGraph(c *gin.Context, subjectID gen.SubjectId, p gen.GetKnowledgeGraphParams) {
	filter := "all"
	if p.Filter != nil {
		filter = string(*p.Filter)
	}
	var section *uint64
	if p.SectionId != nil {
		v := uint64(*p.SectionId)
		section = &v
	}
	g, err := h.deps.Bank.Graph(c.Request.Context(), currentUser(c), uint64(subjectID), filter, section)
	if err != nil {
		_ = c.Error(err)
		return
	}
	var out gen.KnowledgeGraph
	sized(&out.Sections, len(g.Sections))
	for i, s := range g.Sections {
		out.Sections[i].Id, out.Sections[i].Name = int64(s.ID), s.Name
	}
	sized(&out.Nodes, len(g.Nodes))
	for i, n := range g.Nodes {
		x := &out.Nodes[i]
		x.Id, x.Name, x.SectionId, x.State, x.M, x.ExamCount = int64(n.ID), n.Name, int64(n.SectionID), gen.MasteryState(n.State), float32(n.M), n.ExamCount
	}
	out.Edges = make([]gen.KnowledgeRelation, len(g.Edges))
	for i, e := range g.Edges {
		out.Edges[i] = toGenRelation(e)
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) CreateKnowledgeRelation(c *gin.Context, kpID gen.KpId) {
	var req gen.CreateKnowledgeRelationJSONBody
	if !bind(c, &req) {
		return
	}
	r, err := h.deps.Bank.CreateRelation(c.Request.Context(), currentUser(c), uint64(kpID), uint64(req.TargetId), string(req.RelationType))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, toGenRelation(r))
}

func (h *Handlers) DeleteKnowledgeRelation(c *gin.Context, id int64) {
	if err := h.deps.Bank.DeleteRelation(c.Request.Context(), currentUser(c), uint64(id)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func srcRef(materialID int64, valid bool, fileName string, page int32) *gen.SourceRef {
	if !valid {
		return nil
	}
	r := &gen.SourceRef{MaterialId: materialID, FileName: fileName}
	if page > 0 {
		p := int(page)
		r.Page = &p
	}
	return r
}

func (h *Handlers) GetEssayKnowledgeBase(c *gin.Context, subjectID gen.SubjectId) {
	kb, err := h.deps.Bank.EssayKnowledgeBase(c.Request.Context(), currentUser(c), uint64(subjectID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	var out gen.EssayKnowledgeBase
	sized(&out.Methods, len(kb.Methods))
	for i, m := range kb.Methods {
		x := &out.Methods[i]
		x.Id, x.Title, x.Content, x.Origin = int64(m.ID), m.Title, m.Content, gen.Origin(m.Origin)
		// writing_methods 的掌握状态与知识点同一套：unlearned / learning / consolidating / mastered。
		x.State = gen.MasteryState(m.MasteryState)
		if m.Dimension.Valid {
			x.Dimension = &m.Dimension.String
		}
		x.Source = srcRef(m.SourceMaterialID.Int64, m.SourceMaterialID.Valid, m.FileName.String, m.SourcePage.Int32)
	}
	sized(&out.Materials, len(kb.Materials))
	for i, m := range kb.Materials {
		x := &out.Materials[i]
		x.Id, x.Theme, x.Content, x.Origin, x.Favorite, x.ExamCount = int64(m.ID), m.Theme, m.Content, gen.Origin(m.Origin), m.Favorite, int(m.ExamCount)
		x.Source = srcRef(m.SourceMaterialID.Int64, m.SourceMaterialID.Valid, m.FileName.String, m.SourcePage.Int32)
	}
	sized(&out.ModelEssays, len(kb.Models))
	for i, m := range kb.Models {
		x := &out.ModelEssays[i]
		x.Id, x.Title = int64(m.ID), m.Title
		if m.TopicQuestionID.Valid {
			x.TopicQuestionId = &m.TopicQuestionID.Int64
		}
		if m.Topic.Valid {
			x.Topic = &m.Topic.String
		}
		if len(m.Structure) > 0 {
			_ = json.Unmarshal(m.Structure, &x.Structure)
		}
		x.Source = srcRef(m.SourceMaterialID.Int64, m.SourceMaterialID.Valid, m.FileName.String, m.SourcePage.Int32)
	}
	sized(&out.Topics, len(kb.Topics))
	for i, t := range kb.Topics {
		x := &out.Topics[i]
		x.Id, x.Stem, x.ModelEssayCount = int64(t.ID), t.Stem, int(t.ModelEssayCount)
		if t.ExamYear.Valid {
			y := int(t.ExamYear.Int16)
			x.ExamYear = &y
		}
		if t.RequiredWords.Valid {
			w := int(t.RequiredWords.Int16)
			x.RequiredWords = &w
		}
	}
	if r := kb.Rubric; r != nil {
		out.Rubric = &struct {
			Dimensions []struct {
				Description *string `json:"description,omitempty"`
				Name        string  `json:"name"`
				Score       float32 `json:"score"`
			} `json:"dimensions"`
			FullScore float32                            `json:"full_score"`
			Id        int64                              `json:"id"`
			Name      string                             `json:"name"`
			Source    gen.EssayKnowledgeBaseRubricSource `json:"source"`
			SourceRef *gen.SourceRef                     `json:"source_ref,omitempty"`
		}{FullScore: float32(r.FullScore), Id: int64(r.ID), Name: r.Name, Source: gen.EssayKnowledgeBaseRubricSource(r.Source)}
		_ = json.Unmarshal(r.Dimensions, &out.Rubric.Dimensions)
		if r.SourceRef != nil {
			out.Rubric.SourceRef = toSource(r.SourceRef)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) SetEssayMaterialFavorite(c *gin.Context, id int64) {
	var req gen.SetEssayMaterialFavoriteJSONBody
	if !bind(c, &req) {
		return
	}
	if err := h.deps.Bank.SetEssayMaterialFavorite(c.Request.Context(), currentUser(c), uint64(id), req.Favorite); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
