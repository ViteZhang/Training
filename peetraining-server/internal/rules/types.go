package rules

// Stage 是备考阶段（PRD 11.4）。
type Stage string

const (
	Foundation Stage = "foundation" // 基础期
	Strengthen Stage = "strengthen" // 强化期
	Sprint     Stage = "sprint"     // 冲刺期
	Final      Stage = "final"      // 考前期
)

// SelfAssess 是三档自评。
type SelfAssess string

const (
	SelfUnknown  SelfAssess = "unknown"  // 不会
	SelfVague    SelfAssess = "vague"    // 模糊
	SelfMastered SelfAssess = "mastered" // 掌握
)

// Difficulty 是客观题难度；首个版本不识别，一律按中（D16）。
type Difficulty string

const (
	Easy   Difficulty = "easy"
	Medium Difficulty = "medium"
	Hard   Difficulty = "hard"
)

// ReciteResult 是背诵自评。
type ReciteResult string

const (
	ReciteRemembered ReciteResult = "remembered" // 记住了
	ReciteVague      ReciteResult = "vague"      // 模糊
	ReciteForgot     ReciteResult = "forgot"     // 没记住
)

// MasteryState 是掌握状态（PRD 11.2）。
type MasteryState string

const (
	StateUnlearned     MasteryState = "unlearned"     // 未学习
	StateLearning      MasteryState = "learning"      // 学习中
	StateConsolidating MasteryState = "consolidating" // 待巩固
	StateMastered      MasteryState = "mastered"      // 已掌握
)

// LossType 是失分类型（PRD 11.7）。
type LossType string

const (
	LossKnowledge LossType = "knowledge" // 知识没掌握
	LossNorm      LossType = "norm"      // 答题不规范
	LossTime      LossType = "time"      // 时间不够
)

// QType 是题型（与 questions.qtype 一致）。
type QType string

const (
	QTermExplain QType = "term"         // 名词解释
	QShortAnswer QType = "short_answer" // 简答
	QDiscussion  QType = "discussion"   // 论述
	QEssay       QType = "essay"        // 作文
)

// IsObjective 报告题型是否客观题（选择、判断、填空）。
func IsObjective(q QType) bool {
	switch q {
	case "single_choice", "multi_choice", "true_false", "fill_blank":
		return true
	}
	return false
}

// MinutesKey 返回用时参数表里的键：客观题统一为 objective。
func MinutesKey(q QType) string {
	if IsObjective(q) {
		return "objective"
	}
	return string(q)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
