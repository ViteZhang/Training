package ai

import (
	"errors"
	"strings"
)

// ExplainIn 是知识点 AI 解读的输入（3.4，中档模型，首次生成后缓存）。
type ExplainIn struct {
	Subject      string   `json:"subject"`
	Name         string   `json:"name"`
	Path         []string `json:"path"`
	OriginalText string   `json:"original_text"`
	Points       []string `json:"points"`
}

type ExplainOut struct {
	Explanation string `json:"explanation"`
}

var Explain = &Cap[ExplainIn, ExplainOut]{
	Def: Def{Name: "kp_explain", Version: "v1", Tier: "cheap", Temperature: 0.4, Schema: `{
		"type": "object", "required": ["explanation"],
		"properties": {"explanation": {"type": "string", "minLength": 20, "maxLength": 1500}}}`},
	Check: func(in ExplainIn, out *ExplainOut) error {
		// 对外文案不承诺分数（CLAUDE.md 必须遵守第 11 条）。
		for _, w := range []string{"保证得分", "一定能考", "必考", "稳拿"} {
			if strings.Contains(out.Explanation, w) {
				return errors.New("解读里有承诺分数的说法")
			}
		}
		return nil
	},
	Mock: func(in ExplainIn) (ExplainOut, error) {
		var b strings.Builder
		b.WriteString("「" + in.Name + "」")
		if len(in.Path) > 0 {
			b.WriteString("属于" + strings.Join(in.Path, " / ") + "。")
		}
		if in.OriginalText != "" {
			b.WriteString("你的资料里是这样表述的：" + truncateRunes(in.OriginalText, 80) + "。")
		}
		if len(in.Points) > 0 {
			b.WriteString("答题时注意写全这几个要点：" + strings.Join(in.Points, "；") + "。")
		} else {
			b.WriteString("答题时先下定义，再讲特征和意义。")
		}
		return ExplainOut{Explanation: b.String()}, nil
	},
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "……"
	}
	return s
}
