// Package score 是预估分、整卷报告与提分看板（T22，PRD 11.6、4.24、4.25、6.2）。
// 预估分只由服务端算：整卷批改完成、删除资料、改采分点重批后重算，结果写进 score_estimates（最新一条为当前值，按周取趋势）。
package score

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/params"
	"peetraining-server/internal/plan"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/rules"
)

// Service 计算与读取预估分、整卷报告与提分看板。
type Service struct {
	q       *dbq.Queries
	params  *params.Store
	profile *profile.Service
	bank    *bank.Service
	plan    *plan.Service
	now     func() time.Time
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB      *sql.DB
	Params  *params.Store
	Profile *profile.Service
	Bank    *bank.Service
	Plan    *plan.Service
	Now     func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{q: dbq.New(d.DB), params: d.Params, profile: d.Profile, bank: d.Bank, plan: d.Plan, now: now}
}

func dec(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func report(j dbtypes.NullJSON) practice.PaperReport {
	var r practice.PaperReport
	if len(j) > 0 {
		_ = json.Unmarshal(j, &r)
	}
	return r
}

// estimateDetails 是 score_estimates.details：算出这个分数的依据，便于考后回访校准（PRD 11.6）。
type estimateDetails struct {
	Measured float64           `json:"measured"`
	Model    float64           `json:"model"`
	Papers   []float64         `json:"papers"`
	QTypes   []estimateQTStats `json:"qtypes"`
	// 作文课（PRD 11.13）：计入的作文得分，与失分主项（得分率最低的维度）。
	Essays           []float64 `json:"essays,omitempty"`
	MainGapDimension string    `json:"main_gap_dimension,omitempty"`
}

type estimateQTStats struct {
	QType         string  `json:"qtype"`
	TotalInPaper  float64 `json:"total_in_paper"`
	RecentCount   int     `json:"recent_count"`
	LastPaperRate float64 `json:"last_paper_rate"`
}

// Recompute 按 PRD 11.6 重算一门课的预估分并保存。还没做完导入真题卷的课不算（首页显示「做完一套整卷后生成预估分」）。
// 可重复调用：每次都追加一条记录，最新一条为当前值。
func (s *Service) Recompute(ctx context.Context, userID, subjectID uint64, reason string) error {
	p, err := s.params.Rules(ctx)
	if err != nil {
		return err
	}
	sub, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return err
	}
	if sub.IsEssay {
		return s.recomputeEssay(ctx, userID, sub, reason, p)
	}
	sessions, err := s.q.ListGradedPaperSessions(ctx, dbq.ListGradedPaperSessionsParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return err
	}
	var in rules.EstimateInput
	var latest *dbq.ListGradedPaperSessionsRow
	for i, ss := range sessions {
		// AI 组卷成绩不计入（PRD 11.6）。
		if !ss.CountsForEstimate || !ss.Score.Valid {
			continue
		}
		in.Papers = append(in.Papers, rules.PaperScore{Score: dec(ss.Score.String)})
		if latest == nil {
			latest = &sessions[i]
		}
	}
	if latest == nil {
		return nil
	}
	in.FullScore = dec(latest.FullScore)

	// 各题型近期得分率，新的在前。
	rows, err := s.q.ListSubjectRecentRates(ctx, dbq.ListSubjectRecentRatesParams{OwnerUserID: userID, SubjectID: sql.NullInt64{Int64: int64(subjectID), Valid: true}})
	if err != nil {
		return err
	}
	recent := map[string][]float64{}
	for _, r := range rows {
		rate := 0.0
		switch {
		case r.Score.Valid && r.FullScore.Valid && dec(r.FullScore.String) > 0:
			rate = dec(r.Score.String) / dec(r.FullScore.String)
		case r.IsCorrect.Valid && r.IsCorrect.Bool:
			rate = 1
		}
		recent[string(r.Qtype)] = append(recent[string(r.Qtype)], rate)
	}

	// 题型结构取最近一套导入真题卷（缺题卷按比例换算到整卷满分）。
	rep := report(latest.Report)
	scale := 1.0
	if rep.ActualFull > 0 && rep.PaperFull > 0 {
		scale = rep.PaperFull / rep.ActualFull
	}
	det := estimateDetails{}
	for _, ps := range in.Papers {
		det.Papers = append(det.Papers, ps.Score)
	}
	for _, st := range rep.ByQType {
		if st.Full <= 0 {
			continue
		}
		rates := recent[st.QType]
		in.QTypes = append(in.QTypes, rules.QTypeStat{QType: rules.QType(st.QType), TotalInPaper: st.Full * scale, RecentRates: rates, LastPaperRate: st.Got / st.Full})
		used := min(len(rates), p.ScoreEstimate.QTypeRecentQuestions)
		if !rules.IsObjective(rules.QType(st.QType)) {
			in.RecentSubjectiveCount += used
		}
		det.QTypes = append(det.QTypes, estimateQTStats{QType: st.QType, TotalInPaper: round1(st.Full * scale), RecentCount: used, LastPaperRate: st.Got / st.Full})
	}
	est, ok := rules.EstimateScore(in, p.ScoreEstimate)
	if !ok {
		return nil
	}
	det.Measured, det.Model = round1(est.Measured), round1(est.Model)
	raw, _ := json.Marshal(det)
	return s.q.InsertScoreEstimate(ctx, dbq.InsertScoreEstimateParams{OwnerUserID: userID, SubjectID: subjectID, Low: uint16(est.Low), High: uint16(est.High),
		Mid: strconv.FormatFloat(est.Mid, 'f', 2, 64), BasisPapers: uint8(min(est.BasisPapers, 255)), BasisQuestions: uint16(min(est.BasisQuestions, 65535)),
		MainGapQtype: sql.NullString{String: string(est.MainGap), Valid: est.MainGap != ""}, Details: dbtypes.NullJSON(raw), TriggerReason: reason, ComputedAt: s.now().UTC()})
}

// essayDim 是 essays.dimension_scores 里的一个维度。
type essayDim struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
	Max   float64 `json:"max"`
}

// recomputeEssay 是作文课预估分（PRD 11.13）：最近 3 篇按用户评分细则批改、且以真题限时完成的作文得分平均，区间宽度按篇数同 11.6；
// 主要差在 = 这几篇里得分率最低的维度。
func (s *Service) recomputeEssay(ctx context.Context, userID uint64, sub subjectInfo, reason string, p rules.Params) error {
	rows, err := s.q.ListEstimateEssays(ctx, dbq.ListEstimateEssaysParams{OwnerUserID: userID, SubjectID: sub.ID})
	if err != nil {
		return err
	}
	scores := make([]float64, len(rows))
	for i, r := range rows {
		scores[i] = dec(r.Score.String)
	}
	est, ok := rules.EstimateEssay(scores, float64(sub.FullScore), p.ScoreEstimate)
	if !ok {
		return nil
	}
	agg := map[string]*rules.DimScore{}
	var order []string
	for _, r := range rows[:est.BasisPapers] {
		var ds []essayDim
		_ = json.Unmarshal(r.DimensionScores, &ds)
		for _, d := range ds {
			a, ok := agg[d.Name]
			if !ok {
				a = &rules.DimScore{Name: d.Name}
				agg[d.Name] = a
				order = append(order, d.Name)
			}
			a.Score += d.Score
			a.Max += d.Max
		}
	}
	dims := make([]rules.DimScore, 0, len(order))
	for _, n := range order {
		dims = append(dims, *agg[n])
	}
	weak, _ := rules.WeakestDimension(dims)
	det := estimateDetails{Measured: round1(est.Mid), Essays: scores[:est.BasisPapers], MainGapDimension: weak}
	raw, _ := json.Marshal(det)
	return s.q.InsertScoreEstimate(ctx, dbq.InsertScoreEstimateParams{OwnerUserID: userID, SubjectID: sub.ID, Low: uint16(est.Low), High: uint16(est.High),
		Mid: strconv.FormatFloat(est.Mid, 'f', 2, 64), BasisPapers: uint8(est.BasisPapers), Details: dbtypes.NullJSON(raw), TriggerReason: reason,
		ComputedAt: s.now().UTC()})
}

// RecomputeForQuestion 重算一道题所在课的预估分（改采分点重批后）。
func (s *Service) RecomputeForQuestion(ctx context.Context, userID, questionID uint64, reason string) error {
	sid, err := s.q.SubjectOfQuestion(ctx, dbq.SubjectOfQuestionParams{ID: questionID, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !sid.Valid) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Recompute(ctx, userID, uint64(sid.Int64), reason)
}

// Card 是一门课的预估分卡（2.1 第一张卡、6.2 顶部）。
type Card struct {
	SubjectID uint64
	Name      string
	IsEssay   bool
	FullScore int
	Target    *int
	Ready     bool
	Low, High int
	Gap       *int // 目标分 − 预估上限，达到目标时为 0
	MainGap   string
	// MainGapDimension 是作文课的失分主项（得分率最低的维度，PRD 11.13）；作文课的 BasisPapers 是依据的作文篇数。
	MainGapDimension string
	BasisPapers      int
	BasisQuestions   int
	TodayChange      *int // 今天的变化（中值，四舍五入）；今天没变时为空
	ComputedAt       time.Time
}

type subjectInfo struct {
	ID        uint64
	Name      string
	IsEssay   bool
	FullScore uint16
	Target    sql.NullInt16
}

func (s *Service) card(ctx context.Context, userID uint64, sub subjectInfo) (Card, error) {
	c := Card{SubjectID: sub.ID, Name: sub.Name, IsEssay: sub.IsEssay, FullScore: int(sub.FullScore)}
	if sub.Target.Valid && sub.Target.Int16 > 0 {
		t := int(sub.Target.Int16)
		c.Target = &t
	}
	e, err := s.q.LatestScoreEstimate(ctx, dbq.LatestScoreEstimateParams{OwnerUserID: userID, SubjectID: sub.ID})
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Ready, c.Low, c.High = true, int(e.Low), int(e.High)
	c.MainGap, c.BasisPapers, c.BasisQuestions, c.ComputedAt = e.MainGapQtype.String, int(e.BasisPapers), int(e.BasisQuestions), e.ComputedAt
	var det estimateDetails
	if len(e.Details) > 0 && json.Unmarshal(e.Details, &det) == nil {
		c.MainGapDimension = det.MainGapDimension
	}
	if c.Target != nil {
		g := max(*c.Target-c.High, 0)
		c.Gap = &g
	}
	start := rules.DayOf(s.now()).Time().UTC()
	if !e.ComputedAt.Before(start) {
		prev, err := s.q.LastScoreEstimateBefore(ctx, dbq.LastScoreEstimateBeforeParams{OwnerUserID: userID, SubjectID: sub.ID, ComputedAt: start})
		if err == nil {
			if d := int(math.Round(dec(e.Mid) - dec(prev.Mid))); d != 0 {
				c.TodayChange = &d
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return c, err
		}
	}
	return c, nil
}

// Cards 是首页预估分卡：每门专业课一张。
func (s *Service) Cards(ctx context.Context, userID uint64) ([]Card, error) {
	subs, err := s.profile.Subjects(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []Card{}
	for _, sub := range subs.Items {
		c, err := s.card(ctx, userID, subjectInfo{ID: sub.ID, Name: sub.Name, IsEssay: sub.IsEssay, FullScore: sub.FullScore, Target: sub.TargetScore})
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) subject(ctx context.Context, userID, subjectID uint64) (subjectInfo, error) {
	sub, err := s.q.GetSubject(ctx, dbq.GetSubjectParams{ID: subjectID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return subjectInfo{}, apperr.NotFoundErr()
	}
	if err != nil {
		return subjectInfo{}, err
	}
	return subjectInfo{ID: sub.ID, Name: sub.Name, IsEssay: sub.IsEssay, FullScore: sub.FullScore, Target: sub.TargetScore}, nil
}
