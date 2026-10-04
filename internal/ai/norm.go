package ai

import (
	"errors"
	"fmt"
	"strings"
)

// NormElement 是题型结构的一个要素（如名词解释的「定义」「特征要点」「出处或例子」）。
type NormElement struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// NormIn 是答题规范批改的输入（PRD 12.1：题型结构要求 + 用户答案 → 各结构要素是否具备与建议）。
type NormIn struct {
	QType    string        `json:"qtype"`
	Stem     string        `json:"stem"`
	Elements []NormElement `json:"elements"`
	Answer   string        `json:"answer"`
}

// NormResult 是一个结构要素的判定。
type NormResult struct {
	Name       string `json:"name"`
	Present    bool   `json:"present"`
	Quote      string `json:"quote"`
	Suggestion string `json:"suggestion"`
}

type NormOut struct {
	Elements    []NormResult `json:"elements"`
	Suggestions []string     `json:"suggestions"`
}

var GradeNorm = &Cap[NormIn, NormOut]{
	Def: Def{Name: "grade_norm", Version: "v1", Tier: "cheap", Temperature: 0, Schema: `{
		"type": "object", "required": ["elements", "suggestions"],
		"properties": {
			"elements": {"type": "array", "items": {"type": "object", "required": ["name", "present"],
				"properties": {
					"name": {"type": "string"},
					"present": {"type": "boolean"},
					"quote": {"type": "string"},
					"suggestion": {"type": "string"}
				}}},
			"suggestions": {"type": "array", "maxItems": 3, "items": {"type": "string"}}
		}}`},
	// Check：每个要素都要判、按给定顺序；具备的要素引用必须是考生原话。
	Check: func(in NormIn, out *NormOut) error {
		if len(out.Elements) != len(in.Elements) {
			return errors.New("结构要素个数不对")
		}
		ans := compact(in.Answer)
		for i := range out.Elements {
			e := &out.Elements[i]
			if e.Name != in.Elements[i].Name {
				return fmt.Errorf("第 %d 个要素应为「%s」", i+1, in.Elements[i].Name)
			}
			if e.Present {
				if strings.TrimSpace(e.Quote) == "" || !strings.Contains(ans, compact(e.Quote)) {
					return fmt.Errorf("要素「%s」的引用不是考生原话", e.Name)
				}
			} else {
				e.Quote = ""
			}
		}
		return nil
	},
	Mock: mockNorm,
}

// normHints 是 mock 判断要素是否具备时找的提示词。
var normHints = map[string][]string{
	"定义":     {"是", "指"},
	"特征要点":   {"特征", "特点", "一是", "首先", "；"},
	"出处或例子":  {"如", "例如", "出自", "提出", "《"},
	"总述":     {"是", "指", "主要"},
	"分点作答":   {"一是", "首先", "第一", "1.", "（1）"},
	"小结":     {"总之", "综上", "因此"},
	"观点":     {"认为", "是", "观点"},
	"分论点与论证": {"首先", "其次", "一方面", "第一"},
	"结论":     {"总之", "综上", "因此", "所以"},
}

func mockNorm(in NormIn) (NormOut, error) {
	sentences := splitSentences(in.Answer)
	out := NormOut{Suggestions: []string{}}
	for _, el := range in.Elements {
		r := NormResult{Name: el.Name}
		for _, s := range sentences {
			for _, h := range normHints[el.Name] {
				if strings.Contains(s, h) {
					r.Present, r.Quote = true, s
					break
				}
			}
			if r.Present {
				break
			}
		}
		if !r.Present {
			r.Suggestion = "补上「" + el.Name + "」：" + el.Desc
			out.Suggestions = append(out.Suggestions, r.Suggestion)
		}
		out.Elements = append(out.Elements, r)
	}
	if len(out.Suggestions) > 3 {
		out.Suggestions = out.Suggestions[:3]
	}
	return out, nil
}
