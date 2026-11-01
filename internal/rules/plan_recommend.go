package rules

// RecommendTier 按「从这次购买生效起，到最近一次初试结束还剩多少天」推荐会员档位（PRD v3 13.2，11 月 1 日更新）。
// 阈值复用备考阶段（PRD 11.4）：
//   - 冲刺期、考前期（< 60 天）：月卡，一两张就能用到考完，没必要买到考试日或下一年
//   - 强化期（60–149 天）：冲刺卡，一次买到本次初试结束
//   - 基础期（≥ 150 天）：考季卡
//
// 初试日期没配置（hasExam 为 false）时返回空字符串，由调用方回落到后台配置的推荐档。
func RecommendTier(daysToExamEnd int, hasExam bool, p StageParams) string {
	if !hasExam {
		return ""
	}
	switch {
	case daysToExamEnd < p.StrengthenMinDays:
		return "monthly"
	case daysToExamEnd < p.FoundationMinDays:
		return "sprint"
	default:
		return "season"
	}
}
