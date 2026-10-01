package rules

// WrongReason 是收录进错题本的原因。
type WrongReason string

const (
	WrongWrong    WrongReason = "wrong"    // 客观题答错
	WrongPartial  WrongReason = "partial"  // 主观题得分率 < 100%
	WrongRevealed WrongReason = "revealed" // 点了「看答案」
)

// WrongBookAdd 判断一次作答是否收录进错题本（PRD 11.8）：客观题答错、主观题得分率 < 100%、点「看答案」。
func WrongBookAdd(objective, correct bool, scoreRate float64, revealed bool) (WrongReason, bool) {
	switch {
	case revealed:
		return WrongRevealed, true
	case objective && !correct:
		return WrongWrong, true
	case !objective && scoreRate < 1:
		return WrongPartial, true
	}
	return "", false
}

// WrongBookProgress 更新错题本里一道题的「连续答对日期」，并判断是否自动移出（PRD 11.8）：
// 同一题在 2 个不同日期连续答对（主观题得分率 ≥ 80% 视为答对）自动移出，计入「已消灭」；答错一次清零。
func WrongBookProgress(correctDays []Day, correct bool, today Day, p WrongBookParams) ([]Day, bool) {
	if !correct {
		return []Day{}, false
	}
	out := append([]Day{}, correctDays...)
	if len(out) == 0 || out[len(out)-1] != today {
		out = append(out, today)
	}
	return out, len(out) >= p.RemoveAfterCorrectDays
}
