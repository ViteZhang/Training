package rules

import (
	"math"
	"time"
)

// SectionCount 是试卷里某题型的题数。
type SectionCount struct {
	QType QType
	Count int
}

// SectionTime 是某题型的建议用时。
type SectionTime struct {
	QType   QType
	Minutes int
	// CumulativeMinutes 是从开考到这一题型结束的累计建议用时，到点提醒一次（4.21）。
	CumulativeMinutes int
}

// SuggestedTimes 计算各题型建议用时（PRD 11.9）：
// 题数 × 单题考场用时（名词解释 4、简答 15、论述 40 分钟），再按「总时长 − 15」等比例缩放并取整到 5 分钟。
// 考情分析、选择模式、时间报告使用同一套数字。totalMinutes ≤ 0 时取默认 180。
func SuggestedTimes(sections []SectionCount, totalMinutes int, p PaperTimeParams) []SectionTime {
	if totalMinutes <= 0 {
		totalMinutes = p.DefaultTotalMinutes
	}
	available := float64(totalMinutes - p.CheckMinutes)
	raw := make([]float64, len(sections))
	sum := 0.0
	for i, s := range sections {
		per, ok := p.PerQuestionMinutes[MinutesKey(s.QType)]
		if !ok {
			per = p.PerQuestionMinutes[string(QDiscussion)]
		}
		raw[i] = float64(s.Count) * per
		sum += raw[i]
	}
	out := make([]SectionTime, len(sections))
	cum := 0
	step := float64(p.RoundToMinutes)
	for i, s := range sections {
		m := 0
		if sum > 0 && available > 0 {
			m = int(math.Round(raw[i]/sum*available/step) * step)
		}
		cum += m
		out[i] = SectionTime{QType: s.QType, Minutes: m, CumulativeMinutes: cum}
	}
	return out
}

// Overtime 判断某题型实际用时是否超时：超过建议 20%（PRD 11.9）。
func Overtime(actualMinutes, suggestedMinutes float64, p PaperTimeParams) bool {
	return actualMinutes > suggestedMinutes*(1+p.OvertimeRatio)
}

// TimeStatus 是某题型实际用时与建议用时的对比（4.25）：超过建议 20% 为超时，少于建议 20% 为少用，其余为正常。
type TimeStatus string

const (
	TimeOK       TimeStatus = "ok"
	TimeOvertime TimeStatus = "overtime"
	TimeUnder    TimeStatus = "under"
)

// CompareTime 按 PRD 11.9 的超时比例标出超时与少用（少用用同一比例，见 open-questions D22）。
func CompareTime(actualMinutes, suggestedMinutes float64, p PaperTimeParams) TimeStatus {
	switch {
	case suggestedMinutes <= 0:
		return TimeOK
	case Overtime(actualMinutes, suggestedMinutes, p):
		return TimeOvertime
	case actualMinutes < suggestedMinutes*(1-p.OvertimeRatio):
		return TimeUnder
	default:
		return TimeOK
	}
}

// UnansweredQuestion 是一道未作答题。
type UnansweredQuestion struct {
	QType     QType
	FullScore float64
}

// TimeLoss 估计时间失分（PRD 11.9）：未作答题满分 × 该用户该题型平均得分率。
// 没有该题型得分率时按 0 计，不夸大时间失分。
func TimeLoss(unanswered []UnansweredQuestion, avgRate map[QType]float64) float64 {
	loss := 0.0
	for _, u := range unanswered {
		loss += u.FullScore * clamp(avgRate[u.QType], 0, 1)
	}
	return loss
}

// MockDeadline 返回模拟考试的服务端截止时间：开考时间 + 总时长。离开 App 继续计时，倒计时以它为准。
func MockDeadline(start time.Time, totalMinutes int) time.Time {
	return start.Add(time.Duration(totalMinutes) * time.Minute)
}

// CanResume 判断因系统原因中断后能否恢复（PRD 11.9）：10 分钟内、每场只能恢复一次。
// 恢复时补回中断时长（新截止时间 = 原截止时间 + 中断时长）。
func CanResume(interruptedAt, now time.Time, alreadyUsed bool, p PaperTimeParams) bool {
	if alreadyUsed || interruptedAt.IsZero() || now.Before(interruptedAt) {
		return false
	}
	return now.Sub(interruptedAt) <= time.Duration(p.ResumeWindowMinutes)*time.Minute
}

// ShouldWarnTimeLeft 判断是否醒目提示「剩 15 分钟」。
func ShouldWarnTimeLeft(deadline, now time.Time, p PaperTimeParams) bool {
	left := deadline.Sub(now)
	return left > 0 && left <= time.Duration(p.RemindLeftMinutes)*time.Minute
}
