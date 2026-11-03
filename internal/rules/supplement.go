package rules

import "time"

// 补充资料提示（PRD v3 增补 3.1e「补充资料提示」）：已添加官方题库的课，用户在某块错得多、官方题又少时，
// 提示导入本校老师的讲义或笔记。每次只显示一条；「以后再说」后同一提示 SnoozeDays 天内不再出现；注册当天不提示。

// 补充资料提示的触发原因。
const (
	SupplementSectionGap = "section_gap" // 板块缺口：近 14 天作答 ≥ 5、得分率 < 60%、官方题 < 5
	SupplementDispute    = "dispute"     // 批改异议原因为「和我们老师讲的不一样」
	SupplementWrongBook  = "wrong_book"  // 错题本达到 30 题
)

// SectionStat 是一个板块最近的作答情况。
type SectionStat struct {
	ID       uint64
	Name     string
	Attempts int
	ScoreSum float64 // 每题得分率之和（主观题按得分 / 满分，客观题对 1 错 0）
	Official int     // 板块下的官方题数
	Snoozed  bool    // 用户点过「以后再说」，还在 SnoozeDays 天内
}

// Rate 是板块得分率。
func (s SectionStat) Rate() float64 {
	if s.Attempts == 0 {
		return 0
	}
	return s.ScoreSum / float64(s.Attempts)
}

// SupplementInput 是判断要不要提示的输入。
type SupplementInput struct {
	RegisteredAt, Now time.Time
	Sections          []SectionStat
	TeacherDispute    bool
	DisputeSnoozed    bool
	WrongBook         int
	WrongBookSnoozed  bool
}

// SupplementPrompt 是要显示的一条提示；Reason 为空表示不提示。
type SupplementPrompt struct {
	Reason  string
	Section SectionStat
}

// PickSupplementPrompt 按板块缺口 → 异议 → 错题量的顺序挑一条提示；板块缺口里挑得分率最低的。
func PickSupplementPrompt(in SupplementInput, p SupplementPromptParams) SupplementPrompt {
	if DayOf(in.RegisteredAt) == DayOf(in.Now) {
		return SupplementPrompt{}
	}
	var best *SectionStat
	for i := range in.Sections {
		s := &in.Sections[i]
		if s.Snoozed || s.Attempts < p.MinAttempts || s.Rate() >= p.MaxScoreRate || s.Official >= p.MaxOfficial {
			continue
		}
		if best == nil || s.Rate() < best.Rate() {
			best = s
		}
	}
	if best != nil {
		return SupplementPrompt{Reason: SupplementSectionGap, Section: *best}
	}
	if in.TeacherDispute && !in.DisputeSnoozed {
		return SupplementPrompt{Reason: SupplementDispute}
	}
	if p.WrongBookCount > 0 && in.WrongBook >= p.WrongBookCount && !in.WrongBookSnoozed {
		return SupplementPrompt{Reason: SupplementWrongBook}
	}
	return SupplementPrompt{}
}
