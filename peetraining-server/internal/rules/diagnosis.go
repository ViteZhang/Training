package rules

// LossInput 是一道题的失分归因输入（PRD 11.7）。
type LossInput struct {
	LostScore float64
	// MissedOrPartial 表示有采分点遗漏或部分命中。
	MissedOrPartial bool
	// StructureLacking 表示答题结构缺层次（答题规范批改给出）。
	StructureLacking bool
	// KPM 是该题主知识点的掌握分（批改前的值）。
	KPM float64
	// Timed 表示限时作答或模拟考试；「时间不够」只在这里判定。
	Timed      bool
	Unanswered bool
	// Position 是题目在试卷中的相对位置 0–1（按题序）；单题限时作答取 0。
	Position         float64
	TimeSpentSeconds float64
	SuggestedSeconds float64
}

// ClassifyLoss 把一道题的失分归入一类（PRD 11.7）。没有失分时 ok=false。
//
//   - 时间不够：只在限时作答和模拟考试中判定——题目未作答，或位于试卷后段且用时不足建议用时的 30%
//   - 知识没掌握：采分点遗漏或部分命中，且该知识点 M < 60
//   - 答题不规范：其余情况（知识点 M ≥ 60，知道但没写到位；或结构缺层次）
func ClassifyLoss(in LossInput, p LossDiagnosisParams) (LossType, bool) {
	if in.LostScore <= 0 {
		return "", false
	}
	if in.Timed {
		if in.Unanswered {
			return LossTime, true
		}
		if in.Position >= p.TailStartRatio && in.SuggestedSeconds > 0 && in.TimeSpentSeconds < p.TimeMinRatio*in.SuggestedSeconds {
			return LossTime, true
		}
	}
	if in.Unanswered || (in.MissedOrPartial && in.KPM < p.KnowledgeMaxM) {
		return LossKnowledge, true
	}
	return LossNorm, true
}

// AggregateLoss 按失分分值汇总三类（每次批改与整卷都这样归类，提分看板展示占比）。
func AggregateLoss(items []LossInput, p LossDiagnosisParams) map[LossType]float64 {
	out := map[LossType]float64{LossKnowledge: 0, LossNorm: 0, LossTime: 0}
	for _, it := range items {
		if t, ok := ClassifyLoss(it, p); ok {
			out[t] += it.LostScore
		}
	}
	return out
}
