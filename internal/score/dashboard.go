package score

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"peetraining-server/internal/bank"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/plan"
	"peetraining-server/internal/rules"
)

// 提分看板的统计窗口。
const (
	trendWeeks    = 8  // 预估分趋势看近 8 周
	lossDays      = 30 // 失分归因看近 30 天
	recentPapers  = 5  // 最近整卷成绩
	weekdayMonday = 1
)

// WeekPoint 是某周最后一次的预估分（6.2 趋势，按周）。
type WeekPoint struct {
	WeekStart rules.Day
	Low, High int
	Mid       float64
}

// RecentPaper 是一套做完的整卷（6.2 最近整卷与模拟考试成绩）。
type RecentPaper struct {
	SessionID         uint64
	Title             string
	Kind, Mode        string
	Score, FullScore  float64
	CountsForEstimate bool
	GradedAt          time.Time
}

// Dashboard 是提分看板（6.2）。
type Dashboard struct {
	Card          Card
	Trend         []WeekPoint
	LossPoints    map[string]float64
	LossShares    map[string]float64
	SectionsReady bool
	Sections      []bank.SectionShare
	FalseMastery  []plan.KP
	RecentPapers  []RecentPaper
	// EssayDims 是作文课各维度的平均分（6.2「作文课显示五维平均分」），按批改完成的全部作文平均。
	EssayDims []rules.DimScore
}

func weekStart(d rules.Day) rules.Day {
	wd := int(d.Time().Weekday())
	return d.AddDays(-((wd - weekdayMonday + 7) % 7))
}

// Dashboard 出一门课的提分看板：预估分卡与按周趋势、近 30 天失分归因、各板块掌握度 × 真题分值占比、「以为会了」、最近整卷成绩；
// 作文课另有各维度平均分。
func (s *Service) Dashboard(ctx context.Context, userID, subjectID uint64) (Dashboard, error) {
	sub, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Dashboard{}, err
	}
	out := Dashboard{LossPoints: map[string]float64{}, LossShares: map[string]float64{}, Trend: []WeekPoint{}, RecentPapers: []RecentPaper{}, EssayDims: []rules.DimScore{}}
	if out.Card, err = s.card(ctx, userID, sub); err != nil {
		return Dashboard{}, err
	}

	today := rules.DayOf(s.now())
	first := weekStart(today).AddDays(-7 * (trendWeeks - 1))
	ests, err := s.q.ListScoreEstimates(ctx, dbq.ListScoreEstimatesParams{OwnerUserID: userID, SubjectID: subjectID, ComputedAt: first.Time().UTC()})
	if err != nil {
		return Dashboard{}, err
	}
	// 新的在前：每周取第一条（最后一次）。
	seen := map[rules.Day]bool{}
	for _, e := range ests {
		w := weekStart(rules.DayOf(e.ComputedAt))
		if seen[w] {
			continue
		}
		seen[w] = true
		out.Trend = append([]WeekPoint{{WeekStart: w, Low: int(e.Low), High: int(e.High), Mid: round1(dec(e.Mid))}}, out.Trend...)
	}

	losses, err := s.q.ListSubjectGradingLoss(ctx, dbq.ListSubjectGradingLossParams{OwnerUserID: userID, SubjectID: sql.NullInt64{Int64: int64(subjectID), Valid: true},
		Since: today.AddDays(-(lossDays - 1)).Time().UTC()})
	if err != nil {
		return Dashboard{}, err
	}
	total := 0.0
	for _, l := range losses {
		var m map[string]struct {
			Points float64 `json:"points"`
		}
		if json.Unmarshal(l, &m) != nil {
			continue
		}
		for k, v := range m {
			out.LossPoints[k] += v.Points
			total += v.Points
		}
	}
	for k, v := range out.LossPoints {
		out.LossPoints[k] = round1(v)
		if total > 0 {
			out.LossShares[k] = v / total
		}
	}

	if out.Sections, out.SectionsReady, err = s.bank.Sections(ctx, userID, subjectID); err != nil {
		return Dashboard{}, err
	}
	if out.FalseMastery, err = s.plan.FalseMasteryKPs(ctx, userID, subjectID); err != nil {
		return Dashboard{}, err
	}

	if sub.IsEssay {
		if out.EssayDims, err = s.essayDims(ctx, userID, subjectID); err != nil {
			return Dashboard{}, err
		}
	}

	sessions, err := s.q.ListGradedPaperSessions(ctx, dbq.ListGradedPaperSessionsParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return Dashboard{}, err
	}
	for _, ss := range sessions[:min(len(sessions), recentPapers)] {
		out.RecentPapers = append(out.RecentPapers, RecentPaper{SessionID: ss.ID, Title: ss.PaperTitle, Kind: string(ss.PaperKind), Mode: string(ss.Mode),
			Score: dec(ss.Score.String), FullScore: dec(ss.FullScore), CountsForEstimate: ss.CountsForEstimate, GradedAt: ss.GradedAt.Time})
	}
	return out, nil
}

// essayDims 是作文课各维度的平均得分与平均分值（按出现过该维度的作文篇数平均）。
func (s *Service) essayDims(ctx context.Context, userID, subjectID uint64) ([]rules.DimScore, error) {
	rows, err := s.q.ListSubjectEssays(ctx, dbq.ListSubjectEssaysParams{OwnerUserID: userID, SubjectID: subjectID})
	if err != nil {
		return nil, err
	}
	agg := map[string]*rules.DimScore{}
	counts := map[string]int{}
	var order []string
	for _, r := range rows {
		if r.Status != dbq.EssaysStatusGraded {
			continue
		}
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
			counts[d.Name]++
		}
	}
	out := []rules.DimScore{}
	for _, n := range order {
		a, c := agg[n], float64(counts[n])
		out = append(out, rules.DimScore{Name: n, Score: round1(a.Score / c), Max: round1(a.Max / c)})
	}
	return out, nil
}
