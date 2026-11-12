package admin

import (
	"context"
	"fmt"
	"sort"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

// 7.20 激活漏斗（用户使用进度）第一期：只用业务表推算每个注册用户「用到了哪一步」，不依赖埋点，历史用户也能算。
// 阶梯按顺序：注册 → 完成引导 → 有题库 → 首次作答 → 看到批改 → 隔天再练 → 7 日后仍在练 → 付费。
// 「卡在哪一步」取用户达成的最后一级（个别用户可能跳级，比如没走完引导就开始练，按最远的一级算）。

// Stage 是使用阶梯上的一级。
type Stage string

const (
	StageRegistered   Stage = "registered"
	StageOnboarded    Stage = "onboarded"
	StageHasBank      Stage = "has_bank"
	StageFirstAnswer  Stage = "first_answer"
	StageFirstGrading Stage = "first_grading"
	StageReturnDay    Stage = "return_day"
	StageWeekLater    Stage = "week_later"
	StagePaid         Stage = "paid"
)

// StageDef 是一级的说明。
type StageDef struct {
	Key   Stage
	Label string
	Hint  string
}

// Stages 是使用阶梯，按先后顺序。
var Stages = []StageDef{
	{StageRegistered, "注册", "注册或首次登录"},
	{StageOnboarded, "完成引导", "走完新手引导（选专业课、设目标）"},
	{StageHasBank, "有题库", "添加了官方题库，或至少一份资料解析成功"},
	{StageFirstAnswer, "首次作答", "第一次提交答案"},
	{StageFirstGrading, "看到批改", "第一次拿到 AI 批改结果"},
	{StageReturnDay, "隔天再练", "首次作答之后，另一天（北京时间）又作答过"},
	{StageWeekLater, "7 日后仍在练", "注册满 7 天之后还作答过"},
	{StagePaid, "付费", "有过支付成功的订单"},
}

// ValidStage 判断阶段键是否存在。
func ValidStage(s string) bool {
	for _, d := range Stages {
		if string(d.Key) == s {
			return true
		}
	}
	return false
}

// Milestone 是一个用户在一级上的情况；At 为空且 Reached 为真表示达成了但没有记录时间（完成引导）。
type Milestone struct {
	Key     Stage
	Label   string
	Reached bool
	At      *time.Time
}

// Journey 是一个用户的使用进度。
type Journey struct {
	UserID       uint64
	PhoneMasked  string
	CreatedAt    time.Time
	LastActiveAt *time.Time // 最后一次登录、刷新令牌或作答，取最晚的
	LastAnswerAt *time.Time
	Current      Stage
	Milestones   []Milestone
}

// asTime 读出 sqlc 推断为 interface{} 的时间列（子查询 MIN / MAX）。
func asTime(v any) *time.Time {
	var t time.Time
	switch x := v.(type) {
	case time.Time:
		t = x
	case []byte:
		p, err := time.Parse("2006-01-02 15:04:05.999", string(x))
		if err != nil {
			return nil
		}
		t = p
	case string:
		p, err := time.Parse("2006-01-02 15:04:05.999", x)
		if err != nil {
			return nil
		}
		t = p
	default:
		return nil
	}
	t = t.UTC()
	return &t
}

func earliest(ts ...*time.Time) *time.Time {
	var out *time.Time
	for _, t := range ts {
		if t != nil && (out == nil || t.Before(*out)) {
			out = t
		}
	}
	return out
}

func toJourney(r dbq.AdminJourneysRow) Journey {
	created := r.CreatedAt.UTC()
	at := map[Stage]*time.Time{
		StageRegistered:   &created,
		StageHasBank:      earliest(asTime(r.FirstMaterialAt), asTime(r.FirstOfficialAt)),
		StageFirstAnswer:  asTime(r.FirstAnswerAt),
		StageFirstGrading: asTime(r.FirstGradingAt),
		StageReturnDay:    asTime(r.ReturnDayAt),
		StageWeekLater:    asTime(r.WeekLaterAt),
		StagePaid:         asTime(r.FirstPaidAt),
	}
	j := Journey{UserID: r.ID, PhoneMasked: MaskPhone(r.Phone), CreatedAt: created, LastAnswerAt: asTime(r.LastAnswerAt), Current: StageRegistered}
	j.LastActiveAt = nullTimePtr(r.LastActiveAt)
	if j.LastActiveAt != nil {
		v := j.LastActiveAt.UTC()
		j.LastActiveAt = &v
	}
	if j.LastAnswerAt != nil && (j.LastActiveAt == nil || j.LastAnswerAt.After(*j.LastActiveAt)) {
		j.LastActiveAt = j.LastAnswerAt
	}
	for _, d := range Stages {
		m := Milestone{Key: d.Key, Label: d.Label, At: at[d.Key]}
		m.Reached = m.At != nil || (d.Key == StageOnboarded && r.OnboardingStep == "done")
		if m.Reached {
			j.Current = d.Key
		}
		j.Milestones = append(j.Milestones, m)
	}
	return j
}

// cohortRange 把北京时间的注册日期范围 [from, to]（含 to 当天）换成 UTC 的 [from, to+1)；最多 366 天。
func cohortRange(from, to time.Time) (time.Time, time.Time, error) {
	f := dayStart(time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, shanghai))
	t := dayStart(time.Date(to.Year(), to.Month(), to.Day(), 12, 0, 0, 0, shanghai)).AddDate(0, 0, 1)
	if !t.After(f) {
		return f, t, apperr.New(apperr.BadRequest, "结束日期不能早于开始日期")
	}
	if t.Sub(f) > 366*24*time.Hour {
		return f, t, apperr.New(apperr.BadRequest, "注册日期范围最多一年")
	}
	return f, t, nil
}

func (s *Service) journeys(ctx context.Context, fromT, toT time.Time, userID uint64) ([]Journey, error) {
	rows, err := s.q.AdminJourneys(ctx, dbq.AdminJourneysParams{FromT: fromT, ToT: toT, UserID: int64(userID)})
	if err != nil {
		return nil, err
	}
	out := make([]Journey, len(rows))
	for i, r := range rows {
		out[i] = toJourney(r)
	}
	return out, nil
}

// FunnelStep 是漏斗的一级：Reached 达成过这一级的人数；Stuck 停在这一级（最远只到这里）的人数；
// MedianHours 达成者从注册到达成的用时中位数（小时），没有时间记录的一级为空。
type FunnelStep struct {
	Key         Stage
	Label       string
	Hint        string
	Reached     int
	Stuck       int
	MedianHours *float64
}

// Activation 是一批注册用户（按注册日期）的激活漏斗。
type Activation struct {
	From  string // 2026-11-01，北京时间
	To    string
	Total int
	Steps []FunnelStep
}

// Activation 统计注册日期在 [from, to]（北京时间，含两端）的用户各级的达成人数与停留人数。
func (s *Service) Activation(ctx context.Context, from, to time.Time) (Activation, error) {
	fromT, toT, err := cohortRange(from, to)
	if err != nil {
		return Activation{}, err
	}
	js, err := s.journeys(ctx, fromT, toT, 0)
	if err != nil {
		return Activation{}, err
	}
	out := Activation{From: fromT.In(shanghai).Format(time.DateOnly), To: toT.Add(-time.Second).In(shanghai).Format(time.DateOnly), Total: len(js)}
	hours := map[Stage][]float64{}
	reached := map[Stage]int{}
	stuck := map[Stage]int{}
	for _, j := range js {
		stuck[j.Current]++
		for _, m := range j.Milestones {
			if !m.Reached {
				continue
			}
			reached[m.Key]++
			if m.At != nil && m.Key != StageRegistered {
				h := m.At.Sub(j.CreatedAt).Hours()
				if h < 0 {
					h = 0
				}
				hours[m.Key] = append(hours[m.Key], h)
			}
		}
	}
	for _, d := range Stages {
		st := FunnelStep{Key: d.Key, Label: d.Label, Hint: d.Hint, Reached: reached[d.Key], Stuck: stuck[d.Key]}
		if hs := hours[d.Key]; len(hs) > 0 {
			sort.Float64s(hs)
			m := hs[len(hs)/2]
			if len(hs)%2 == 0 {
				m = (hs[len(hs)/2-1] + hs[len(hs)/2]) / 2
			}
			m = float64(int(m*10+0.5)) / 10
			st.MedianHours = &m
		}
		out.Steps = append(out.Steps, st)
	}
	return out, nil
}

// ActivationUsers 列出注册日期在 [from, to] 内、最远停在 stage 这一级的用户（按用户 ID 倒序，50 条一页，before 为上一页最后一个 ID）。
func (s *Service) ActivationUsers(ctx context.Context, from, to time.Time, stage string, before uint64) ([]Journey, error) {
	if !ValidStage(stage) {
		return nil, apperr.New(apperr.BadRequest, "未知阶段")
	}
	fromT, toT, err := cohortRange(from, to)
	if err != nil {
		return nil, err
	}
	js, err := s.journeys(ctx, fromT, toT, 0)
	if err != nil {
		return nil, err
	}
	out := []Journey{}
	for _, j := range js {
		if string(j.Current) != stage || (before != 0 && j.UserID >= before) {
			continue
		}
		out = append(out, j)
		if len(out) == 50 {
			break
		}
	}
	return out, nil
}

// DayCount 是某一天（北京时间）的作答次数。
type DayCount struct {
	Day     string
	Answers int
}

// UserJourney 是 7.2 用户详情里的「使用进度」：各级达成时间，加最近 30 天每天的作答次数（没作答的日子不列）。
type UserJourney struct {
	Journey
	ActiveDays30 int
	Answers30    int
	Days         []DayCount
}

// UserJourney 返回一个用户的使用进度；用户不存在（或在注销中）时 404。
func (s *Service) UserJourney(ctx context.Context, userID uint64) (UserJourney, error) {
	js, err := s.journeys(ctx, time.Unix(0, 0).UTC(), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), userID)
	if err != nil {
		return UserJourney{}, err
	}
	if len(js) == 0 {
		return UserJourney{}, apperr.NotFoundErr()
	}
	out := UserJourney{Journey: js[0], Days: []DayCount{}}
	since := dayStart(s.now()).AddDate(0, 0, -29)
	days, err := s.q.AdminUserDailyAnswers(ctx, dbq.AdminUserDailyAnswersParams{UserID: userID, Since: since})
	if err != nil {
		return UserJourney{}, err
	}
	for _, d := range days {
		day, err := asDay(d.Day)
		if err != nil {
			return UserJourney{}, err
		}
		out.Days = append(out.Days, DayCount{Day: day, Answers: int(d.Answers)})
		out.ActiveDays30++
		out.Answers30 += int(d.Answers)
	}
	return out, nil
}

func asDay(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case []byte:
		return string(x), nil
	case time.Time:
		return x.Format(time.DateOnly), nil
	}
	return "", fmt.Errorf("日期列类型 %T", v)
}
