package rules

import (
	"math"
	"sort"
)

// PlanGroup 是今日计划的四组（PRD 11.5）。
type PlanGroup string

const (
	GroupNew    PlanGroup = "new"    // 新知识点
	GroupReview PlanGroup = "review" // 到期复习
	GroupWeak   PlanGroup = "weak"   // 薄弱查漏 / 题型专项
	GroupRecite PlanGroup = "recite" // 背诵
)

// PlanGroups 是计划里各组的固定顺序。
var PlanGroups = []PlanGroup{GroupNew, GroupReview, GroupWeak, GroupRecite}

// MixFor 返回某阶段四组的时间占比（PRD 11.5）。
// 冲刺期覆盖率不足时保留 newShare 的新知识点（PRD 11.4），其余三组按原比例缩放。
func MixFor(stage Stage, lowCoverage bool, newShare float64, p map[Stage]PlanMix) PlanMix {
	mix := p[stage]
	if !lowCoverage || mix.New >= newShare {
		return mix
	}
	rest := mix.Review + mix.Weak + mix.Recite
	scale := (1 - newShare) / rest
	return PlanMix{New: newShare, Review: mix.Review * scale, Weak: mix.Weak * scale, Recite: mix.Recite * scale}
}

func (m PlanMix) share(g PlanGroup) float64 {
	switch g {
	case GroupNew:
		return m.New
	case GroupReview:
		return m.Review
	case GroupWeak:
		return m.Weak
	default:
		return m.Recite
	}
}

// KPWeight 计算知识点权重（PRD 11.5）：所在板块的真题分值占比 ×（1 + 0.5 × 该知识点的真题出现次数）。
// 没有真题时板块等权（1 / 板块数）、出现次数取 0。
func KPWeight(sectionShare float64, examCount int, hasExams bool, sectionCount int, p PlanParams) float64 {
	if !hasExams {
		if sectionCount <= 0 {
			return 1
		}
		return 1 / float64(sectionCount)
	}
	return sectionShare * (1 + p.KPExamBonus*float64(examCount))
}

// QTypeCoef 计算题型系数（PRD 11.5）：该题型在用户真题卷中的单题分值，按最大值归一；没有真题卷时为 1。
// 真题里没出现过的题型取已出现题型里最小的系数，既不排除它，也不让它压过真题题型。
func QTypeCoef(q QType, scoreEach map[QType]float64) float64 {
	if len(scoreEach) == 0 {
		return 1
	}
	maxV, minV := 0.0, math.Inf(1)
	for _, v := range scoreEach {
		maxV = math.Max(maxV, v)
		minV = math.Min(minV, v)
	}
	if maxV <= 0 {
		return 1
	}
	if v, ok := scoreEach[q]; ok {
		return v / maxV
	}
	return minV / maxV
}

// Gain 计算提分收益（PRD 11.5）：知识点权重 × (100 − M) / 100 × 题型系数。
func Gain(kpWeight, m, qtypeCoef float64) float64 {
	return kpWeight * (100 - clamp(m, 0, 100)) / 100 * qtypeCoef
}

// ItemMinutes 返回单题预计用时（PRD 11.5）：客观题 1、名词解释 3、简答 6、论述 12、背诵 1 / 条。
// 参数表里没有的题型按论述计，宁可高估也不让计划超时。
func ItemMinutes(q QType, recite bool, p PlanParams) float64 {
	if recite {
		return p.Minutes["recite"]
	}
	if v, ok := p.Minutes[MinutesKey(q)]; ok {
		return v
	}
	return p.Minutes[string(QDiscussion)]
}

// PlanCandidate 是某一组的一个候选（题目或背诵条目）。候选由调用方按组准备：
// 新知识点 = 未学习知识点的题；到期复习 = 复习日已到的题或错题；薄弱查漏 = M 低的知识点
// （强化期以后也包含未学习知识点，保证新用户首日计划不为空）；背诵 = 背诵复习日已到或未背过的知识点。
type PlanCandidate struct {
	Group      PlanGroup
	QuestionID int64 // 背诵条目为 0
	KPID       int64
	QType      QType
	Gain       float64
}

// SubjectCandidates 是一门专业课的候选。
type SubjectCandidates struct {
	SubjectID  int64
	Candidates []PlanCandidate
}

// PlanInput 是生成今日计划的输入。
type PlanInput struct {
	BudgetMinutes float64
	Mix           PlanMix
	Subjects      []SubjectCandidates
}

// PlanItem 是计划里的一项。
type PlanItem struct {
	SubjectID  int64
	Group      PlanGroup
	QuestionID int64
	KPID       int64
	QType      QType
	Minutes    float64
}

// Plan 是生成结果。Shortfall 是各专业课各组没填满的分钟数：开启了「AI 出变式题」的用户由调用方按知识点补题，
// 否则有多少出多少。
type Plan struct {
	Items        []PlanItem
	TotalMinutes float64
	Shortfall    map[int64]map[PlanGroup]float64
}

// BuildPlan 生成今日计划（PRD 11.5）。
//
//   - 按每日学习时长分配预算，多门专业课平均分配；某门课还没有题目时，它的份额给其他课
//   - 每门课内按阶段占比分到四组，组内按提分收益从高到低选，放不下的跳过、继续看更小的
//   - 同一知识点当日只出现一次（背诵与做题分开算，背诵是独立的复习）
//   - 输出顺序：先做题（客观题在前、主观题在后），再背诵
func BuildPlan(in PlanInput, p PlanParams) Plan {
	plan := Plan{Shortfall: map[int64]map[PlanGroup]float64{}}
	var active []SubjectCandidates
	for _, s := range in.Subjects {
		if hasQuestions(s.Candidates) {
			active = append(active, s)
		}
	}
	if len(active) == 0 {
		return plan
	}
	perSubject := in.BudgetMinutes / float64(len(active))

	var practice, recite []PlanItem
	for _, s := range active {
		usedKP := map[int64]bool{}
		usedRecite := map[int64]bool{}
		usedQ := map[int64]bool{}
		for _, g := range PlanGroups {
			budget := perSubject * in.Mix.share(g)
			cands := filterGroup(s.Candidates, g)
			remaining := budget
			for _, c := range cands {
				isRecite := g == GroupRecite
				mins := ItemMinutes(c.QType, isRecite, p)
				if mins > remaining+1e-9 {
					continue
				}
				if isRecite {
					if usedRecite[c.KPID] {
						continue
					}
					usedRecite[c.KPID] = true
				} else {
					if usedKP[c.KPID] || usedQ[c.QuestionID] {
						continue
					}
					usedKP[c.KPID] = true
					usedQ[c.QuestionID] = true
				}
				item := PlanItem{SubjectID: s.SubjectID, Group: g, QuestionID: c.QuestionID, KPID: c.KPID, QType: c.QType, Minutes: mins}
				if isRecite {
					recite = append(recite, item)
				} else {
					practice = append(practice, item)
				}
				remaining -= mins
				plan.TotalMinutes += mins
			}
			if remaining >= 1 {
				if plan.Shortfall[s.SubjectID] == nil {
					plan.Shortfall[s.SubjectID] = map[PlanGroup]float64{}
				}
				plan.Shortfall[s.SubjectID][g] = remaining
			}
		}
	}
	sort.SliceStable(practice, func(i, j int) bool {
		return IsObjective(practice[i].QType) && !IsObjective(practice[j].QType)
	})
	plan.Items = append(practice, recite...)
	return plan
}

func hasQuestions(cs []PlanCandidate) bool {
	for _, c := range cs {
		if c.Group != GroupRecite {
			return true
		}
	}
	return false
}

func filterGroup(cs []PlanCandidate, g PlanGroup) []PlanCandidate {
	var out []PlanCandidate
	for _, c := range cs {
		if c.Group == g {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Gain != out[j].Gain {
			return out[i].Gain > out[j].Gain
		}
		return out[i].QuestionID < out[j].QuestionID
	})
	return out
}
