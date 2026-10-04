package rules

import "sort"

// MasteryEventKind 是会改变掌握分的事件（PRD 11.1）。
type MasteryEventKind int

const (
	EventSelfAssess       MasteryEventKind = iota + 1 // 作答前自评
	EventObjectiveCorrect                             // 客观题答对
	EventObjectiveWrong                               // 客观题答错
	EventSubjective                                   // 主观题批改
	EventRevealAnswer                                 // 点「看答案 / 看参考答案」
	EventRecite                                       // 背诵自评
)

// MasteryEvent 是一次事件。不同事件只用到对应的字段。
type MasteryEvent struct {
	Kind       MasteryEventKind
	SelfAssess SelfAssess   // EventSelfAssess
	Difficulty Difficulty   // EventObjectiveCorrect；为空按中
	ScoreRate  float64      // EventSubjective：得分率 0–1
	Recite     ReciteResult // EventRecite
	// Secondary 为 true 表示题目关联的非主知识点，变化量减半（PRD 11.1 补充）。
	Secondary bool
}

// ApplyMastery 返回事件之后的掌握分 M（PRD 11.1），结果截断在 0–100。
//
// answered 表示该知识点此前已有作答验证记录：此时自评只记录、不改 M（D16）；
// 没有作答记录时，自评直接把 M 设为对应值（不会 0、模糊 30、掌握 50）。
func ApplyMastery(m float64, ev MasteryEvent, answered bool, p MasteryParams) float64 {
	var delta float64
	switch ev.Kind {
	case EventSelfAssess:
		if answered {
			return m
		}
		return clamp(p.SelfAssess[ev.SelfAssess], 0, 100)
	case EventObjectiveCorrect:
		d := ev.Difficulty
		if d == "" {
			d = Medium
		}
		delta = p.ObjectiveCorrect[d]
	case EventObjectiveWrong:
		delta = p.ObjectiveWrong
	case EventSubjective:
		// +25 × 得分率 − 10：满分 +15，零分 −10。
		delta = p.SubjectiveSlope*clamp(ev.ScoreRate, 0, 1) + p.SubjectiveOffset
	case EventRevealAnswer:
		delta = p.RevealAnswer
	case EventRecite:
		delta = p.Recite[ev.Recite]
	}
	if ev.Secondary {
		delta *= p.SecondaryKPFactor
	}
	return clamp(m+delta, 0, 100)
}

// ApplyOverdue 计算逾期未复习的衰减（PRD 11.1：超过下次复习日后每天 −3）。
//
// 从 max(下次复习日, 上次已衰减到的日期) 算到 today，返回新的 M 与新的「已衰减到」日期；
// 每天只衰减一次，重复调用不会重复扣。nextReview 为 0 值表示没有排期，不衰减。
func ApplyOverdue(m float64, nextReview, decayedTo, today Day, p MasteryParams) (float64, Day) {
	if nextReview == 0 {
		return m, decayedTo
	}
	from := nextReview
	if decayedTo > from {
		from = decayedTo
	}
	days := int(today - from)
	if days <= 0 {
		return m, decayedTo
	}
	return clamp(m+float64(days)*p.OverdueDaily, 0, 100), today
}

// MasteryFacts 是判定掌握状态需要的事实。
type MasteryFacts struct {
	M        float64
	Viewed   bool // 有浏览记录
	Assessed bool // 有自评记录
	Answered bool // 有作答验证记录
	// CorrectDays 是答对的日期（主观题得分率 ≥ 80% 视为答对），可重复、无序。
	CorrectDays []Day
}

// StateOf 判定掌握状态（PRD 11.2）。
//
// 已掌握：M ≥ 80，且近 30 天内在 2 个不同日期答对过；
// 待巩固：有作答记录但未达已掌握；学习中：有浏览或自评但从未作答；未学习：都没有。
func StateOf(f MasteryFacts, today Day, p MasteryStateParams) MasteryState {
	if !f.Answered {
		if f.Viewed || f.Assessed {
			return StateLearning
		}
		return StateUnlearned
	}
	if f.M >= p.MasteredMinM && distinctDaysWithin(f.CorrectDays, today, p.MasteredWindowDays) >= p.MasteredCorrectDays {
		return StateMastered
	}
	return StateConsolidating
}

// distinctDaysWithin 统计 [today-window+1, today] 内不同日期的个数。
func distinctDaysWithin(days []Day, today Day, window int) int {
	seen := map[Day]bool{}
	for _, d := range days {
		if d <= today && int(today-d) < window {
			seen[d] = true
		}
	}
	return len(seen)
}

// TrimCorrectDays 去重、只保留窗口内的答对日期并升序排列，供存回 kp_mastery.correct_dates。
func TrimCorrectDays(days []Day, today Day, window int) []Day {
	seen := map[Day]bool{}
	out := []Day{}
	for _, d := range days {
		if d <= today && int(today-d) < window && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// IsCorrect 判断一次作答是否算「答对」：客观题看对错，主观题得分率 ≥ 阈值（PRD 11.2、11.8 都是 80%）。
func IsCorrect(objective bool, correct bool, scoreRate, threshold float64) bool {
	if objective {
		return correct
	}
	return scoreRate >= threshold
}

// FalseMastery 判定「以为会了」（PRD 11.2）：最近一次自评为「掌握」，
// 且近 7 天该知识点作答 ≥ 2 次、正确率 < 50%。recent 为近 7 天每次作答是否答对。
func FalseMastery(lastSelfAssess SelfAssess, recent []bool, p MasteryStateParams) bool {
	if lastSelfAssess != SelfMastered || len(recent) < p.FalseMasteryMinAttempts {
		return false
	}
	correct := 0
	for _, ok := range recent {
		if ok {
			correct++
		}
	}
	return float64(correct)/float64(len(recent)) < p.FalseMasteryMaxRate
}

// SectionMastery 返回板块掌握度（板块内知识点 M 的平均值）与是否「短板」（低于 40）。
// 板块没有知识点时返回 0、不标短板。
func SectionMastery(ms []float64, p MasteryStateParams) (avg float64, weak bool) {
	if len(ms) == 0 {
		return 0, false
	}
	sum := 0.0
	for _, m := range ms {
		sum += m
	}
	avg = sum / float64(len(ms))
	return avg, avg < p.WeakSectionAvg
}
