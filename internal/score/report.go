package score

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/rules"
)

// qtypeNames 是报告文案里的题型名。
var qtypeNames = map[string]string{"term": "名词解释", "short_answer": "简答", "discussion": "论述", "essay": "作文", "single_choice": "单选",
	"multi_choice": "多选", "true_false": "判断", "fill_blank": "填空", "calculation": "计算", "other": "其他"}

func qtypeName(q string) string {
	if n, ok := qtypeNames[q]; ok {
		return n
	}
	return q
}

// QTypeScore 是整卷里一种题型的得分（4.24）。
type QTypeScore struct {
	QType     string
	Got, Full float64
}

// SectionTime 是一种题型的建议与实际用时（4.25）。
type SectionTime struct {
	QType            string
	Count, Answered  int
	SuggestedMinutes int
	ActualMinutes    int
	Status           rules.TimeStatus
	DiffMinutes      int // 实际 − 建议
	Unfinished       bool
}

// TrendPoint 是近几次整卷的未答题数（4.25）。
type TrendPoint struct {
	SessionID  uint64
	Date       time.Time
	Unanswered int
}

// TimeReport 是时间分析报告（4.25，只有模拟考试有）。
type TimeReport struct {
	TotalMinutes          int
	UsedMinutes           int
	UsedFull              bool
	Unanswered            int
	TimeLoss              float64
	Sections              []SectionTime
	CheckSuggestedMinutes int
	CheckActualMinutes    int
	CheckStatus           rules.TimeStatus
	Conclusion            string
	Advice                []string
	Trend                 []TrendPoint
}

// PaperReport 是整卷报告（4.24）。
type PaperReport struct {
	SessionID         uint64
	PaperID           uint64
	SubjectID         uint64
	Title             string
	Kind, Mode        string
	Score, FullScore  float64
	CountsForEstimate bool
	GradedAt          time.Time
	PrevDelta         *float64
	Target            *int
	Gap               *float64
	ByQType           []QTypeScore
	Loss              map[string]float64
	LossTotal         float64
	WeakestQType      string
	Time              *TimeReport
}

// trendSize 是时间报告里看近几次整卷。
const trendSize = 5

// PaperReport 出整卷报告：总分、较上次、与目标差距、题型得分、失分归因三类；模拟考试另有时间分析（PRD 4.24、4.25、11.9）。
// 还在批改时返回 409。
func (s *Service) PaperReport(ctx context.Context, userID, sessionID uint64) (PaperReport, error) {
	row, err := s.q.GetPaperSession(ctx, dbq.GetPaperSessionParams{ID: sessionID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return PaperReport{}, apperr.NotFoundErr()
	}
	if err != nil {
		return PaperReport{}, err
	}
	if row.Status != dbq.PaperSessionsStatusGraded {
		return PaperReport{}, apperr.New(apperr.Conflict, "这套卷还在批改，大约需要 2 分钟")
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return PaperReport{}, err
	}
	sub, err := s.subject(ctx, userID, row.SubjectID)
	if err != nil {
		return PaperReport{}, err
	}
	rep := report(row.Report)
	out := PaperReport{SessionID: row.ID, SubjectID: row.SubjectID, Title: row.PaperTitle, Kind: string(row.PaperKind), Mode: string(row.Mode),
		Score: dec(row.Score.String), FullScore: dec(row.FullScore), CountsForEstimate: row.CountsForEstimate, GradedAt: row.GradedAt.Time, Loss: map[string]float64{}}
	if row.PaperID.Valid {
		out.PaperID = uint64(row.PaperID.Int64)
	}
	if sub.Target.Valid && sub.Target.Int16 > 0 {
		t := int(sub.Target.Int16)
		out.Target = &t
		g := round1(math.Max(float64(t)-out.Score, 0))
		out.Gap = &g
	}
	// 较上次：同一门课上一套批改完成的整卷。
	sessions, err := s.q.ListGradedPaperSessions(ctx, dbq.ListGradedPaperSessionsParams{OwnerUserID: userID, SubjectID: row.SubjectID})
	if err != nil {
		return PaperReport{}, err
	}
	idx := -1
	for i, ss := range sessions {
		if ss.ID == row.ID {
			idx = i
			break
		}
	}
	if idx >= 0 && idx+1 < len(sessions) && sessions[idx+1].Score.Valid {
		d := round1(out.Score - dec(sessions[idx+1].Score.String))
		out.PrevDelta = &d
	}

	worst := -1.0
	for _, st := range rep.ByQType {
		out.ByQType = append(out.ByQType, QTypeScore{QType: st.QType, Got: round1(st.Got), Full: round1(st.Full)})
		if l := st.Full - st.Got; l > worst {
			out.WeakestQType, worst = st.QType, l
		}
	}
	for _, k := range []string{"knowledge", "norm", "time"} {
		out.Loss[k] = round1(rep.Loss[k])
		out.LossTotal += rep.Loss[k]
	}
	out.LossTotal = round1(out.LossTotal)

	if row.Mode == dbq.PaperSessionsModeMock {
		var mocks []dbq.ListGradedPaperSessionsRow
		if idx >= 0 {
			for _, ss := range sessions[idx:] {
				if ss.Mode == dbq.PaperSessionsModeMock {
					mocks = append(mocks, ss)
				}
			}
		}
		out.Time = timeReport(rep, mocks, p.PaperTime)
	}
	return out, nil
}

// timeReport 按 PRD 11.9 出时间分析：建议用时与报告里存的同一套数字；mocks 是这一场及更早的模拟考试，新的在前。
func timeReport(rep practice.PaperReport, mocks []dbq.ListGradedPaperSessionsRow, pt rules.PaperTimeParams) *TimeReport {
	total := rep.TotalMinutes
	if total <= 0 {
		total = pt.DefaultTotalMinutes
	}
	used := rep.ElapsedSeconds
	if used <= 0 {
		used = rep.UsedSeconds
	}
	t := &TimeReport{TotalMinutes: total, UsedMinutes: int(math.Round(float64(used) / 60)), Unanswered: rep.Unanswered, TimeLoss: round1(rep.TimeLoss),
		CheckSuggestedMinutes: pt.CheckMinutes}
	// 交卷时剩不到 1 分钟算用满时间。
	t.UsedFull = used >= total*60-60
	itemSeconds := 0
	var over, unfinished []string
	for _, st := range rep.ByQType {
		itemSeconds += st.TimeSeconds
		actual := int(math.Round(float64(st.TimeSeconds) / 60))
		sec := SectionTime{QType: st.QType, Count: st.Count, Answered: st.Answered, SuggestedMinutes: st.SuggestedMinutes, ActualMinutes: actual,
			Status: rules.CompareTime(float64(st.TimeSeconds)/60, float64(st.SuggestedMinutes), pt), DiffMinutes: actual - st.SuggestedMinutes,
			Unfinished: st.Answered < st.Count}
		t.Sections = append(t.Sections, sec)
		if sec.Status == rules.TimeOvertime {
			over = append(over, fmt.Sprintf("%s超时 %d 分钟", qtypeName(st.QType), sec.DiffMinutes))
		}
		if sec.Unfinished {
			unfinished = append(unfinished, fmt.Sprintf("%s有 %d 题没写", qtypeName(st.QType), st.Count-st.Answered))
		}
	}
	// 检查时间 = 交卷前没有停在任何一道题上的时间（见 open-questions D22）。
	t.CheckActualMinutes = int(math.Round(float64(max(used-itemSeconds, 0)) / 60))
	t.CheckStatus = rules.CompareTime(float64(t.CheckActualMinutes), float64(pt.CheckMinutes), pt)

	parts := append(over, unfinished...)
	switch {
	case len(parts) > 0:
		t.Conclusion = strings.Join(parts, "，")
	case t.CheckStatus == rules.TimeUnder:
		t.Conclusion = "各题型用时都在建议范围内，但没有留出检查时间"
	default:
		t.Conclusion = "各题型用时都在建议范围内"
	}
	t.Advice = advice(t.Sections, t.CheckStatus, pt.CheckMinutes)

	// 近几次模拟考试的未答题数，旧的在前。
	for i := min(len(mocks), trendSize) - 1; i >= 0; i-- {
		m := mocks[i]
		t.Trend = append(t.Trend, TrendPoint{SessionID: m.ID, Date: m.GradedAt.Time, Unanswered: report(m.Report).Unanswered})
	}
	return t
}

// advice 是「下次这样分配」：每题控制在建议用时内；超时的题型到点就换；没写完的题型先写分值高的；留出检查时间。
func advice(secs []SectionTime, check rules.TimeStatus, checkMinutes int) []string {
	var out []string
	sorted := append([]SectionTime(nil), secs...)
	// 先说问题最大的题型：没写完 > 超时 > 其他。
	rank := func(s SectionTime) int {
		switch {
		case s.Unfinished:
			return 0
		case s.Status == rules.TimeOvertime:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return rank(sorted[i]) < rank(sorted[j]) })
	for _, s := range sorted {
		if s.Count <= 0 || s.SuggestedMinutes <= 0 {
			continue
		}
		name := qtypeName(s.QType)
		per := max(int(math.Round(float64(s.SuggestedMinutes)/float64(s.Count))), 1)
		switch {
		case s.Unfinished:
			out = append(out, fmt.Sprintf("%s留足 %d 分钟，先写分值高的题，每题先列要点再展开", name, s.SuggestedMinutes))
		case s.Status == rules.TimeOvertime:
			out = append(out, fmt.Sprintf("%s每题控制在 %d 分钟，到建议用时就进入下一题型", name, per))
		default:
			out = append(out, fmt.Sprintf("%s每题约 %d 分钟", name, per))
		}
	}
	if check == rules.TimeUnder {
		out = append(out, fmt.Sprintf("最后留 %d 分钟检查", checkMinutes))
	}
	return out
}
