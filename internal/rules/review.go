package rules

// ReviewOutcome 是一次复习结果的分档（PRD 11.3）。
type ReviewOutcome int

const (
	// ReviewFail：答错 / 看答案 / 主观题得分率 < 50% / 背诵没记住 → 1 天后，间隔序列重置。
	ReviewFail ReviewOutcome = iota + 1
	// ReviewVague：模糊 / 主观题得分率 50–79% → 2 天后，序列不前进。
	ReviewVague
	// ReviewPass：答对 / 记住了 → 首次 3 天后，之后按 7 → 15 → 30 天前进。
	ReviewPass
)

// SubjectiveOutcome 把主观题得分率映射为复习分档。
func SubjectiveOutcome(rate float64, p ReviewParams) ReviewOutcome {
	switch {
	case rate < p.SubjectiveLowRate:
		return ReviewFail
	case rate < p.SubjectiveHighRate:
		return ReviewVague
	default:
		return ReviewPass
	}
}

// ReciteOutcome 把背诵自评映射为复习分档。
func ReciteOutcome(r ReciteResult) ReviewOutcome {
	switch r {
	case ReciteRemembered:
		return ReviewPass
	case ReciteVague:
		return ReviewVague
	default:
		return ReviewFail
	}
}

// NextReview 计算下次复习日与新的间隔档位（PRD 11.3）。
//
// step 是已连续通过的次数：0 表示还没通过过，通过后间隔取 Steps[step]（超过末档保持 30 天）。
func NextReview(outcome ReviewOutcome, step int, today Day, p ReviewParams) (Day, int) {
	switch outcome {
	case ReviewFail:
		return today.AddDays(p.ResetDays), 0
	case ReviewVague:
		return today.AddDays(p.VagueDays), step
	default:
		i := step
		if i >= len(p.Steps) {
			i = len(p.Steps) - 1
		}
		return today.AddDays(p.Steps[i]), step + 1
	}
}
