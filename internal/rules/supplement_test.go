package rules

import (
	"testing"
	"time"
)

func TestPickSupplementPrompt(t *testing.T) {
	p := DefaultParams().OfficialFirst.Supplement
	now := time.Date(2026, 11, 3, 4, 0, 0, 0, time.UTC)
	reg := now.AddDate(0, 0, -3)
	// 增补的例子：作答 ≥ 5、得分率 < 60%、官方题 < 5。
	gap := SectionStat{ID: 1, Name: "近代哲学", Attempts: 5, ScoreSum: 2.5, Official: 4}
	cases := []struct {
		name string
		in   SupplementInput
		want string
	}{
		{"板块缺口", SupplementInput{RegisteredAt: reg, Now: now, Sections: []SectionStat{gap}}, SupplementSectionGap},
		{"注册当天不提示", SupplementInput{RegisteredAt: now.Add(-time.Hour), Now: now, Sections: []SectionStat{gap}}, ""},
		{"作答不到 5 题", SupplementInput{RegisteredAt: reg, Now: now, Sections: []SectionStat{{Attempts: 4, ScoreSum: 0, Official: 0}}}, ""},
		{"得分率正好 60% 不提示", SupplementInput{RegisteredAt: reg, Now: now, Sections: []SectionStat{{Attempts: 5, ScoreSum: 3, Official: 0}}}, ""},
		{"官方题正好 5 道不提示", SupplementInput{RegisteredAt: reg, Now: now, Sections: []SectionStat{{Attempts: 5, ScoreSum: 0, Official: 5}}}, ""},
		{"以后再说的板块不提示，轮到异议", SupplementInput{RegisteredAt: reg, Now: now, Sections: []SectionStat{{Attempts: 5, ScoreSum: 0, Official: 0, Snoozed: true}}, TeacherDispute: true}, SupplementDispute},
		{"错题本 30 题", SupplementInput{RegisteredAt: reg, Now: now, WrongBook: 30}, SupplementWrongBook},
		{"错题本 29 题", SupplementInput{RegisteredAt: reg, Now: now, WrongBook: 29}, ""},
		{"错题本提示被以后再说", SupplementInput{RegisteredAt: reg, Now: now, WrongBook: 40, WrongBookSnoozed: true}, ""},
	}
	for _, c := range cases {
		if got := PickSupplementPrompt(c.in, p); got.Reason != c.want {
			t.Errorf("%s：得到 %q，期望 %q", c.name, got.Reason, c.want)
		}
	}
	// 每次只显示一条：两个板块都有缺口时挑得分率最低的。
	worse := SectionStat{ID: 2, Name: "古代哲学", Attempts: 6, ScoreSum: 1.2, Official: 1}
	got := PickSupplementPrompt(SupplementInput{RegisteredAt: reg, Now: now, Sections: []SectionStat{gap, worse}, WrongBook: 50}, p)
	if got.Section.ID != 2 {
		t.Errorf("挑得分率最低的板块：%+v", got)
	}
}
