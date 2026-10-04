package rules

// DefaultStage 按距专业课考试的天数给出默认阶段（PRD 11.4，D16 边界）：
// ≥ 150 天基础期、60–149 强化期、14–59 冲刺期、< 14 考前期。
func DefaultStage(daysToExam int, p StageParams) Stage {
	switch {
	case daysToExam >= p.FoundationMinDays:
		return Foundation
	case daysToExam >= p.StrengthenMinDays:
		return Strengthen
	case daysToExam >= p.SprintMinDays:
		return Sprint
	default:
		return Final
	}
}

// CoverageKP 是计算知识点覆盖率需要的事实。
type CoverageKP struct {
	State     MasteryState
	ExamCount int // 在用户真题中的出现次数
}

// Coverage 计算知识点覆盖率（PRD 11.4）：非「未学习」知识点的加权占比，权重 = 1 + 真题出现次数。
// 没有知识点时返回 0。
func Coverage(kps []CoverageKP) float64 {
	var total, covered float64
	for _, k := range kps {
		w := 1 + float64(k.ExamCount)
		total += w
		if k.State != StateUnlearned {
			covered += w
		}
	}
	if total == 0 {
		return 0
	}
	return covered / total
}

var stageOrder = map[Stage]int{Foundation: 0, Strengthen: 1, Sprint: 2, Final: 3}

// StageAdvice 是阶段检查的结果。
type StageAdvice struct {
	// Suggested 是系统建议的阶段（6.9 显示「系统建议和理由」）。
	Suggested Stage
	// Reason 是建议理由的类型：by_date 到期、early_sprint 准备度提前。
	Reason string
	// Prompt 为 true 时弹出 2.1e「进入新阶段提示」（同一目标阶段只弹一次，由调用方记录）。
	Prompt bool
	// LowCoverage 为 true 时冲刺期首页提示先补新知识点，计划保留 NewKPShare 的新知识点。
	LowCoverage bool
	NewKPShare  float64
}

// AdviseStage 检查是否该进入新阶段（PRD 11.4）。
//
// 阶段到点（按日期的默认阶段比当前阶段靠后）或满足修正条件（强化期、距考试 ≤ 90 天且覆盖率 ≥ 70%）时提示；
// 用户手动选择过阶段时只提示、不自动覆盖——是否切换由用户在 2.1e 决定，这里从不直接改阶段。
func AdviseStage(current Stage, daysToExam int, coverage float64, p StageParams) StageAdvice {
	byDate := DefaultStage(daysToExam, p)
	adv := StageAdvice{Suggested: current}
	switch {
	case stageOrder[byDate] > stageOrder[current]:
		adv.Suggested, adv.Reason, adv.Prompt = byDate, "by_date", true
	case current == Strengthen && daysToExam <= p.EarlySprintMaxDays && coverage >= p.EarlySprintCoverage:
		adv.Suggested, adv.Reason, adv.Prompt = Sprint, "early_sprint", true
	}
	effective := current
	if adv.Prompt {
		effective = adv.Suggested
	}
	if effective == Sprint && coverage < p.SprintLowCoverage {
		adv.LowCoverage = true
		adv.NewKPShare = p.SprintNewKPShare
	}
	return adv
}
