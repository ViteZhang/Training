package essay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/quota"
)

// Brief 是作文列表里的一篇（5.1 草稿、5.8 作文本）。
type Brief struct {
	ID                uint64
	Topic             string
	TopicSource       string
	DraftNo           int
	Status            string
	Score             *float64
	FullScore         float64
	CountsForEstimate bool
	WordCount         int
	CreatedAt         time.Time
	GradedAt          *time.Time
}

func briefOf(r dbq.ListSubjectEssaysRow) Brief {
	b := Brief{ID: r.ID, Topic: r.TopicText, TopicSource: string(r.TopicSource), DraftNo: int(r.DraftNo), Status: string(r.Status),
		Score: nullScore(r.Score.String, r.Score.Valid && r.Status == dbq.EssaysStatusGraded), FullScore: dec(r.FullScore.String),
		CountsForEstimate: r.CountsForEstimate, WordCount: int(r.WordCount), CreatedAt: r.CreatedAt}
	if r.GradedAt.Valid {
		t := r.GradedAt.Time
		b.GradedAt = &t
	}
	return b
}

// ExamTopic 是一道作文真题（5.1「真题题目」）：已写稿数、最高分、附了几篇用户导入的范文。
type ExamTopic struct {
	ID              uint64
	Stem            string
	ExamYear        int
	RequiredWords   int
	Drafts          int
	Best            *float64
	ModelEssayCount int
}

// AITopicBrief 是一道 AI 命题与写过几稿。
type AITopicBrief struct {
	AITopic
	Drafts int
}

// Home 是作文训练（5.1）；NoMaterial 时显示 5.1b 引导导入，仍可用 AI 命题或自拟按通用标准先写。
type Home struct {
	WeeklyGoal      int
	WeekDone        int
	AvgScore        *float64
	Rubric          RubricInfo
	NoMaterial      bool
	ExamTopics      []ExamTopic
	AITopics        []AITopicBrief
	Drafts          []Brief
	WeeklyRemaining *int
}

// Home 返回一门课的作文训练首页。
func (s *Service) Home(ctx context.Context, userID, subjectID uint64) (Home, error) {
	sub, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Home{}, err
	}
	h := Home{ExamTopics: []ExamTopic{}, AITopics: []AITopicBrief{}, Drafts: []Brief{}, WeeklyGoal: 2}
	if prof, err := s.q.GetStudyProfile(ctx, userID); err == nil {
		h.WeeklyGoal = int(prof.EssayWeeklyGoal)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Home{}, err
	}
	r, err := s.activeRubric(ctx, userID, subjectID)
	if err != nil {
		return Home{}, err
	}
	h.Rubric = RubricInfo{ID: r.ID, Name: r.Name, Source: r.Source, FullScore: r.FullScore, FileName: r.FileName, Page: r.Page}
	if h.WeeklyRemaining, err = s.quota.Remaining(ctx, userID, quota.EssayGrading); err != nil {
		return Home{}, err
	}

	rows, err := s.q.ListSubjectEssays(ctx, dbq.ListSubjectEssaysParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return Home{}, err
	}
	week := weekStart(s.now())
	drafts, best := map[uint64]int{}, map[uint64]float64{}
	aiDrafts := map[uint64]int{}
	sum, n := 0.0, 0
	for _, e := range rows {
		if e.TopicQuestionID.Valid {
			q := uint64(e.TopicQuestionID.Int64)
			drafts[q]++
			if e.Status == dbq.EssaysStatusGraded && dec(e.Score.String) > best[q] {
				best[q] = dec(e.Score.String)
			}
		}
		if e.AiTopicID.Valid {
			aiDrafts[uint64(e.AiTopicID.Int64)]++
		}
		switch e.Status {
		case dbq.EssaysStatusDraft, dbq.EssaysStatusFailed:
			h.Drafts = append(h.Drafts, briefOf(e))
		case dbq.EssaysStatusGraded:
			sum += dec(e.Score.String)
			n++
			if e.GradedAt.Valid && !e.GradedAt.Time.Before(week) {
				h.WeekDone++
			}
		}
	}
	if n > 0 {
		avg := round1(sum / float64(n))
		h.AvgScore = &avg
	}

	topics, err := s.q.ListEssayTopicsKB(ctx, dbq.ListEssayTopicsKBParams{BankID: sub.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return Home{}, err
	}
	for _, t := range topics {
		et := ExamTopic{ID: t.ID, Stem: t.Stem, ExamYear: int(t.ExamYear.Int16), RequiredWords: int(t.RequiredWords.Int16), Drafts: drafts[t.ID], ModelEssayCount: int(t.ModelEssayCount)}
		if b, ok := best[t.ID]; ok {
			et.Best = &b
		}
		h.ExamTopics = append(h.ExamTopics, et)
	}
	ais, err := s.q.ListEssayAITopics(ctx, dbq.ListEssayAITopicsParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return Home{}, err
	}
	for _, t := range ais {
		h.AITopics = append(h.AITopics, AITopicBrief{AITopic: AITopic{ID: t.ID, Topic: t.Topic, RequiredWords: int(t.RequiredWords.Int16), Note: t.Note.String, CreatedAt: t.CreatedAt},
			Drafts: aiDrafts[t.ID]})
	}
	models, err := s.q.ListModelEssaysKB(ctx, dbq.ListModelEssaysKBParams{BankID: sub.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return Home{}, err
	}
	h.NoMaterial = len(topics) == 0 && len(models) == 0 && r.Source == "generic"
	return h, nil
}

// TrendPoint 是作文本分数趋势的一点。
type TrendPoint struct {
	EssayID   uint64
	Date      time.Time
	Score     float64
	FullScore float64
}

// WeakDim 是最弱维度（失分主项，PRD 11.13）与对应的写作方法（「去学」→ 3.10）。
type WeakDim struct {
	Name        string
	AvgScore    float64
	AvgMax      float64
	MethodID    uint64
	MethodTitle string
}

// Notebook 是作文本（5.8）。
type Notebook struct {
	Count    int
	AvgScore *float64
	Best     *float64
	Trend    []TrendPoint
	Weakest  *WeakDim
	DimAvgs  []DimScore // 各维度平均分（6.2 作文课五维平均分）
	Items    []Brief
}

// trendSize 是作文本趋势看最近几篇。
const trendSize = 10

// Notebook 返回一门课的作文本。
func (s *Service) Notebook(ctx context.Context, userID, subjectID uint64) (Notebook, error) {
	sub, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Notebook{}, err
	}
	rows, err := s.q.ListSubjectEssays(ctx, dbq.ListSubjectEssaysParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return Notebook{}, err
	}
	nb := Notebook{Items: []Brief{}, Trend: []TrendPoint{}, DimAvgs: []DimScore{}}
	type agg struct{ score, max float64 }
	dims := map[string]*agg{}
	var order []string
	sum := 0.0
	for _, e := range rows {
		nb.Items = append(nb.Items, briefOf(e))
		if e.Status != dbq.EssaysStatusGraded || !e.Score.Valid {
			continue
		}
		sc := dec(e.Score.String)
		nb.Count++
		sum += sc
		if nb.Best == nil || sc > *nb.Best {
			b := sc
			nb.Best = &b
		}
		if len(nb.Trend) < trendSize {
			nb.Trend = append([]TrendPoint{{EssayID: e.ID, Date: e.GradedAt.Time, Score: sc, FullScore: dec(e.FullScore.String)}}, nb.Trend...)
		}
		var ds []dimScore
		_ = json.Unmarshal(e.DimensionScores, &ds)
		for _, d := range ds {
			a, ok := dims[d.Name]
			if !ok {
				a = &agg{}
				dims[d.Name] = a
				order = append(order, d.Name)
			}
			a.score += d.Score
			a.max += d.Max
		}
	}
	if nb.Count == 0 {
		return nb, nil
	}
	avg := round1(sum / float64(nb.Count))
	nb.AvgScore = &avg
	// 各维度的平均分按出现过的篇数平均；最弱维度看得分率（不同标准的分值不同）。
	counts := map[string]int{}
	for _, e := range rows {
		var ds []dimScore
		_ = json.Unmarshal(e.DimensionScores, &ds)
		for _, d := range ds {
			counts[d.Name]++
		}
	}
	weakRate := 2.0
	for _, name := range order {
		a, c := dims[name], float64(counts[name])
		nb.DimAvgs = append(nb.DimAvgs, DimScore{Name: name, Score: round1(a.score / c), Max: round1(a.max / c)})
		if a.max > 0 && a.score/a.max < weakRate {
			weakRate = a.score / a.max
			nb.Weakest = &WeakDim{Name: name, AvgScore: round1(a.score / c), AvgMax: round1(a.max / c)}
		}
	}
	if nb.Weakest != nil {
		methods, err := s.q.ListWritingMethods(ctx, dbq.ListWritingMethodsParams{BankID: sub.BankID, OwnerUserID: owner(userID)})
		if err != nil {
			return Notebook{}, err
		}
		for _, m := range methods {
			if m.Dimension.Valid && (strings.Contains(m.Dimension.String, nb.Weakest.Name) || strings.Contains(nb.Weakest.Name, m.Dimension.String)) {
				nb.Weakest.MethodID, nb.Weakest.MethodTitle = m.ID, m.Title
				break
			}
		}
	}
	return nb, nil
}

// Rubrics 是评分标准（5.9）：你的资料（从用户资料识别，可编辑）与通用标准，可切换。
type Rubrics struct {
	ActiveID uint64
	User     *Rubric
	Generic  Rubric
}

// Rubrics 返回一门课的两种评分标准与当前用的那个。
func (s *Service) Rubrics(ctx context.Context, userID, subjectID uint64) (Rubrics, error) {
	if _, err := s.subject(ctx, userID, subjectID); err != nil {
		return Rubrics{}, err
	}
	active, err := s.activeRubric(ctx, userID, subjectID)
	if err != nil {
		return Rubrics{}, err
	}
	out := Rubrics{ActiveID: active.ID}
	g, err := s.q.GetGenericEssayRubric(ctx)
	if err != nil {
		return Rubrics{}, err
	}
	out.Generic = rubricOf(g.ID, g.Name, string(g.Source), string(g.Origin), g.FullScore, g.Dimensions, g.SourceMaterialID, sql.NullString{}, g.SourcePage)
	users, err := s.q.ListUserEssayRubrics(ctx, dbq.ListUserEssayRubricsParams{OwnerUserID: owner(userID), SubjectID: sql.NullInt64{Int64: int64(subjectID), Valid: true}})
	if err != nil {
		return Rubrics{}, err
	}
	if len(users) > 0 {
		u := users[0]
		for _, r := range users {
			if r.ID == active.ID {
				u = r
			}
		}
		ur := rubricOf(u.ID, u.Name, string(u.Source), string(u.Origin), u.FullScore, u.Dimensions, u.SourceMaterialID, u.FileName, u.SourcePage)
		out.User = &ur
	}
	return out, nil
}

// SelectRubric 切换评分标准（5.9）：之后新写的作文按它批改，已批改的分数不变。
func (s *Service) SelectRubric(ctx context.Context, userID, subjectID uint64, source string) (Rubrics, error) {
	rs, err := s.Rubrics(ctx, userID, subjectID)
	if err != nil {
		return Rubrics{}, err
	}
	sid := sql.NullInt64{Int64: int64(subjectID), Valid: true}
	switch source {
	case "generic":
		err = s.q.DeactivateEssayRubrics(ctx, dbq.DeactivateEssayRubricsParams{OwnerUserID: owner(userID), SubjectID: sid})
	case "user_material":
		if rs.User == nil {
			return Rubrics{}, apperr.New(apperr.Conflict, "还没有从你的资料里识别出评分细则，先导入评分细则")
		}
		err = s.withTx(ctx, func(q *dbq.Queries) error {
			if err := q.DeactivateEssayRubrics(ctx, dbq.DeactivateEssayRubricsParams{OwnerUserID: owner(userID), SubjectID: sid}); err != nil {
				return err
			}
			_, err := q.ActivateEssayRubric(ctx, dbq.ActivateEssayRubricParams{ID: rs.User.ID, OwnerUserID: owner(userID)})
			return err
		})
	default:
		return Rubrics{}, apperr.New(apperr.BadRequest, "请选择评分标准")
	}
	if err != nil {
		return Rubrics{}, err
	}
	return s.Rubrics(ctx, userID, subjectID)
}

// UpdateRubric 编辑「你的资料」评分标准（维度、说明、分值与分档）；通用标准不能改。满分 = 各维度分值之和。
// 只影响之后的批改：已批改的作文存了标准快照，分数不变。
func (s *Service) UpdateRubric(ctx context.Context, userID, subjectID uint64, name string, dims []ai.EssayDim) (Rubrics, error) {
	rs, err := s.Rubrics(ctx, userID, subjectID)
	if err != nil {
		return Rubrics{}, err
	}
	if rs.User == nil {
		return Rubrics{}, apperr.New(apperr.Conflict, "通用标准不能修改；导入评分细则后可以编辑")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return Rubrics{}, apperr.New(apperr.BadRequest, "标准名称 1–64 字")
	}
	if len(dims) == 0 || len(dims) > 10 {
		return Rubrics{}, apperr.New(apperr.BadRequest, "评分维度 1–10 个")
	}
	full, seen := 0.0, map[string]bool{}
	for i := range dims {
		d := &dims[i]
		d.Name = strings.TrimSpace(d.Name)
		if d.Name == "" || len([]rune(d.Name)) > 16 || seen[d.Name] {
			return Rubrics{}, apperr.New(apperr.BadRequest, "维度名称不能为空、不超过 16 字、不能重复")
		}
		if d.Score <= 0 || d.Score > 150 {
			return Rubrics{}, apperr.New(apperr.BadRequest, "维度分值在 0–150 之间")
		}
		seen[d.Name] = true
		full += d.Score
	}
	raw, _ := json.Marshal(dims)
	if _, err := s.q.UpdateEssayRubric(ctx, dbq.UpdateEssayRubricParams{Name: name, FullScore: fmtScore(full), Dimensions: raw, ID: rs.User.ID, OwnerUserID: owner(userID)}); err != nil {
		return Rubrics{}, err
	}
	return s.Rubrics(ctx, userID, subjectID)
}

// ModelEssay 是范文详情（5.7）：只展示用户自己导入的范文与 AI 结构拆解。
type ModelEssay struct {
	ID         uint64
	Title      string
	Topic      string
	Content    string
	Structure  json.RawMessage
	MaterialID uint64
	FileName   string
	Page       int
}

func (s *Service) ModelEssay(ctx context.Context, userID, id uint64) (ModelEssay, error) {
	m, err := s.q.GetModelEssay(ctx, dbq.GetModelEssayParams{ID: id, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return ModelEssay{}, apperr.NotFoundErr()
	}
	if err != nil {
		return ModelEssay{}, err
	}
	return ModelEssay{ID: m.ID, Title: m.Title, Topic: m.Topic.String, Content: m.Content, Structure: json.RawMessage(m.Structure), FileName: m.FileName.String,
		Page: int(m.SourcePage.Int32), MaterialID: uint64(m.SourceMaterialID.Int64)}, nil
}
