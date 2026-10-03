package essay

import (
	"context"
	"encoding/json"
	"time"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

// DimScore 是一个维度的得分（5.6）。
type DimScore struct {
	Name    string
	Score   float64
	Max     float64
	Comment string
}

// ModelRef 是同题范文要点（5.6「范文对比」，只有用户自己导入的范文）。
type ModelRef struct {
	ID        uint64
	Title     string
	Structure json.RawMessage
}

// View 是一篇作文：写作页（5.2）、批改中（5.5）与批改结果（5.6）共用。
type View struct {
	ID, SubjectID     uint64
	TopicSource       string
	TopicQuestionID   uint64
	AITopicID         uint64
	Topic             string
	RequiredWords     int
	TimeLimitMinutes  int
	DraftNo           int
	ParentID          uint64
	Content           string
	PhotoKeys         []string
	WordCount         int
	Timed             bool
	DurationSeconds   int
	Status            string
	FailReason        string
	Score             *float64
	FullScore         float64
	PrevDelta         *float64
	Rubric            *RubricInfo
	Dimensions        []DimScore
	Weakest           string
	Thesis            string
	Highlights        []string
	Problems          []string
	Suggestions       []string
	Annotations       []Annotation
	Paragraphs        []string
	ModelEssays       []ModelRef
	CountsForEstimate bool
	Disputed          bool
	ScoreBefore       *float64
	CreatedAt         time.Time
	SubmittedAt       *time.Time
	GradedAt          *time.Time
}

// RubricInfo 是批改所用评分标准（快照）的说明，结果页可点开看 5.9。
type RubricInfo struct {
	ID        uint64
	Name      string
	Source    string
	FullScore float64
	FileName  string
	Page      int
}

// Annotation 是逐段批注。
type Annotation struct {
	Paragraph  int
	Quote      string
	Issue      string
	Suggestion string
}

func nullScore(v string, ok bool) *float64 {
	if !ok {
		return nil
	}
	f := dec(v)
	return &f
}

// Get 返回一篇作文；批改中的不显示分数。
func (s *Service) Get(ctx context.Context, userID, id uint64) (View, error) {
	e, err := s.essay(ctx, userID, id)
	if err != nil {
		return View{}, err
	}
	return s.view(ctx, userID, e)
}

func (s *Service) view(ctx context.Context, userID uint64, e dbq.Essay) (View, error) {
	p, err := s.params.Rules(ctx)
	if err != nil {
		return View{}, err
	}
	v := View{ID: e.ID, SubjectID: e.SubjectID, TopicSource: string(e.TopicSource), Topic: e.TopicText, RequiredWords: int(e.RequiredWords.Int16),
		DraftNo: int(e.DraftNo), Content: e.Content.String, WordCount: int(e.WordCount), Timed: e.Timed, DurationSeconds: int(e.DurationSeconds),
		Status: string(e.Status), FailReason: e.FailReason.String, CountsForEstimate: e.CountsForEstimate, Disputed: e.Disputed, CreatedAt: e.CreatedAt,
		PhotoKeys: []string{}}
	// 真题默认按考试时长倒计时（PRD 5.2）：取作文的单题考场用时（PRD 11.9 参数）。
	v.TimeLimitMinutes = int(p.PaperTime.PerQuestionMinutes[string(rules.QEssay)])
	if e.TopicQuestionID.Valid {
		v.TopicQuestionID = uint64(e.TopicQuestionID.Int64)
	}
	if e.AiTopicID.Valid {
		v.AITopicID = uint64(e.AiTopicID.Int64)
	}
	if e.ParentEssayID.Valid {
		v.ParentID = uint64(e.ParentEssayID.Int64)
	}
	if len(e.PhotoKeys) > 0 {
		_ = json.Unmarshal(e.PhotoKeys, &v.PhotoKeys)
	}
	if e.SubmittedAt.Valid {
		t := e.SubmittedAt.Time
		v.SubmittedAt = &t
	}
	var snap rubricSnapshot
	if len(e.RubricSnapshot) > 0 && json.Unmarshal(e.RubricSnapshot, &snap) == nil {
		v.Rubric = &RubricInfo{ID: snap.ID, Name: snap.Name, Source: snap.Source, FullScore: snap.FullScore, FileName: snap.FileName, Page: snap.Page}
		v.FullScore = snap.FullScore
	}
	if e.Status != dbq.EssaysStatusGraded {
		return v, nil
	}

	v.Score = nullScore(e.Score.String, e.Score.Valid)
	v.ScoreBefore = nullScore(e.ScoreBefore.String, e.ScoreBefore.Valid)
	if e.GradedAt.Valid {
		t := e.GradedAt.Time
		v.GradedAt = &t
	}
	var dims []dimScore
	_ = json.Unmarshal(e.DimensionScores, &dims)
	var ws []rules.DimScore
	for _, d := range dims {
		v.Dimensions = append(v.Dimensions, DimScore(d))
		ws = append(ws, rules.DimScore{Name: d.Name, Score: d.Score, Max: d.Max})
	}
	v.Weakest, _ = rules.WeakestDimension(ws)
	var rv review
	_ = json.Unmarshal(e.Review, &rv)
	v.Thesis, v.Highlights, v.Problems, v.Suggestions, v.Paragraphs = rv.Thesis, rv.Highlights, rv.Problems, rv.Suggestions, rv.Paragraphs
	for _, a := range rv.Annotations {
		v.Annotations = append(v.Annotations, Annotation(a))
	}

	// 较上篇：同一门课上一篇批改完成的作文。
	rows, err := s.q.ListSubjectEssays(ctx, dbq.ListSubjectEssaysParams{OwnerUserID: userID, SubjectID: e.SubjectID})
	if err != nil {
		return View{}, err
	}
	for _, r := range rows {
		if r.ID != e.ID && r.Status == dbq.EssaysStatusGraded && r.Score.Valid && r.GradedAt.Valid && e.GradedAt.Valid && r.GradedAt.Time.Before(e.GradedAt.Time) && v.Score != nil {
			d := round1(*v.Score - dec(r.Score.String))
			v.PrevDelta = &d
			break
		}
	}
	if v.TopicQuestionID != 0 {
		ms, err := s.q.ListTopicModelEssays(ctx, dbq.ListTopicModelEssaysParams{OwnerUserID: owner(userID), TopicQuestionID: owner(v.TopicQuestionID)})
		if err != nil {
			return View{}, err
		}
		for _, m := range ms {
			v.ModelEssays = append(v.ModelEssays, ModelRef{ID: m.ID, Title: m.Title, Structure: json.RawMessage(m.Structure)})
		}
	}
	return v, nil
}
