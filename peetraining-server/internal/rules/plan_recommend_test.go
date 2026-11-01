package rules

import "testing"

func TestRecommendTier(t *testing.T) {
	cases := []struct {
		days    int
		hasExam bool
		want    string
	}{
		{0, false, ""},
		{20, true, "monthly"}, // 不到一个月
		{49, true, "monthly"}, // 11 月 1 日距 12 月 20 日初试结束
		{59, true, "monthly"}, // 冲刺期最后一天
		{60, true, "sprint"},  // 强化期起点
		{149, true, "sprint"}, // 强化期终点
		{150, true, "season"}, // 基础期
		{400, true, "season"}, // 初试刚结束，最近一次是下一年
	}
	for _, c := range cases {
		if got := RecommendTier(c.days, c.hasExam, P.Stage); got != c.want {
			t.Errorf("RecommendTier(%d, %v) = %q, want %q", c.days, c.hasExam, got, c.want)
		}
	}
}
