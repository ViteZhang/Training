package ai

import (
	"errors"
	"strings"
)

// VariantIn 是 AI 出变式题的输入（PRD 模块 4：题库不够时按用户资料里的知识点出题，标「AI 出题」与依据）。
type VariantIn struct {
	Subject      string   `json:"subject"`
	KPName       string   `json:"kp_name"`
	OriginalText string   `json:"original_text"`
	Points       []string `json:"points"`
	QType        string   `json:"qtype"`
	Example      string   `json:"example,omitempty"`
}

// VariantOption 是选择题选项。
type VariantOption struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

type VariantOut struct {
	Stem     string          `json:"stem"`
	Options  []VariantOption `json:"options,omitempty"`
	Answer   string          `json:"answer"`
	Analysis string          `json:"analysis"`
}

// IsChoice 报告题型是否带选项。
func IsChoice(qtype string) bool {
	return qtype == "single_choice" || qtype == "multi_choice" || qtype == "true_false"
}

var Variant = &Cap[VariantIn, VariantOut]{
	Def: Def{Name: "question_variant", Version: "v1", Tier: "cheap", Temperature: 0.7, Schema: `{
		"type": "object", "required": ["stem", "answer", "analysis"],
		"properties": {
			"stem": {"type": "string", "minLength": 4, "maxLength": 600},
			"options": {"type": "array", "maxItems": 6, "items": {"type": "object", "required": ["key", "text"],
				"properties": {"key": {"type": "string", "pattern": "^[A-F]$"}, "text": {"type": "string", "minLength": 1}}}},
			"answer": {"type": "string", "minLength": 1},
			"analysis": {"type": "string"}
		}}`},
	Check: func(in VariantIn, out *VariantOut) error {
		if IsChoice(in.QType) {
			if len(out.Options) < 2 {
				return errors.New("选择题至少两个选项")
			}
			keys := map[string]bool{}
			for _, o := range out.Options {
				keys[o.Key] = true
			}
			ans := strings.ReplaceAll(strings.ReplaceAll(out.Answer, ",", ""), " ", "")
			if ans == "" {
				return errors.New("没有答案")
			}
			for _, r := range ans {
				if !keys[string(r)] {
					return errors.New("答案不在选项里")
				}
			}
			if in.QType == "single_choice" && len([]rune(ans)) != 1 {
				return errors.New("单选题只能有一个答案")
			}
			out.Answer = ans
		} else {
			out.Options = nil
		}
		for _, w := range []string{"保证得分", "必考", "押题"} {
			if strings.Contains(out.Stem+out.Analysis, w) {
				return errors.New("出现承诺分数或押题的说法")
			}
		}
		return nil
	},
	Mock: func(in VariantIn) (VariantOut, error) {
		def := truncateRunes(in.OriginalText, 60)
		if def == "" && len(in.Points) > 0 {
			def = in.Points[0]
		}
		if def == "" {
			def = in.KPName + "的基本含义"
		}
		switch in.QType {
		case "single_choice", "multi_choice":
			return VariantOut{Stem: "下列关于「" + in.KPName + "」的表述，正确的是", Answer: "A",
				Options:  []VariantOption{{"A", def}, {"B", in.KPName + "只是一种修辞手法"}, {"C", in.KPName + "与作品内容无关"}, {"D", "以上都不对"}},
				Analysis: "依据资料原文：" + def}, nil
		case "true_false":
			return VariantOut{Stem: "判断：" + def, Answer: "A", Options: []VariantOption{{"A", "正确"}, {"B", "错误"}}, Analysis: "资料原文即如此表述。"}, nil
		case "short_answer":
			return VariantOut{Stem: "简述" + in.KPName + "。", Answer: def, Analysis: "按资料里的采分点作答。"}, nil
		case "discussion":
			return VariantOut{Stem: "试论" + in.KPName + "。", Answer: def, Analysis: "按资料里的采分点展开论述。"}, nil
		}
		return VariantOut{Stem: in.KPName, Answer: def, Analysis: "名词解释：定义、特征要点、出处或例子。"}, nil
	},
}
