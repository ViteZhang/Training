package practice

import (
	"sort"
	"strings"
	"unicode"
)

// Option 是选择题选项。
type Option struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

var trueWords = map[string]bool{"对": true, "正确": true, "√": true, "T": true, "TRUE": true, "是": true, "✓": true}
var falseWords = map[string]bool{"错": true, "错误": true, "×": true, "F": true, "FALSE": true, "否": true, "✗": true}

// truthOf 把判断题的答案或选项文字归一为 T / F；认不出返回空。
func truthOf(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch {
	case trueWords[s]:
		return "T"
	case falseWords[s]:
		return "F"
	}
	return ""
}

// keysOf 把「A、C」「AC」「A,C」这样的答案拆成排好序的选项字母。
func keysOf(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range strings.ToUpper(s) {
		if r >= 'A' && r <= 'H' && !seen[string(r)] {
			seen[string(r)] = true
			out = append(out, string(r))
		}
	}
	sort.Strings(out)
	return out
}

// normText 去掉空白与标点、统一大小写，用于填空题比对。
func normText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Judge 判客观题（选中即判分，PRD 4.3）。返回是否答对与是否能判（答案缺失时不能判）。
//
// 单选、多选比较选项字母集合；判断题既接受选项字母（按选项文字归一为对 / 错），也接受「对 / 错」；
// 填空题去掉空白和标点后比较，多个空用「；」或「|」分隔，逐空比较。
func Judge(qtype string, options []Option, answer string, selected []string, text string) (correct, ok bool) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return false, false
	}
	switch qtype {
	case "single_choice", "multi_choice":
		want := keysOf(answer)
		got := keysOf(strings.Join(selected, ""))
		if len(want) == 0 {
			return false, false
		}
		return strings.Join(want, "") == strings.Join(got, ""), true
	case "true_false":
		textOf := map[string]string{}
		for _, o := range options {
			textOf[strings.ToUpper(o.Key)] = o.Text
		}
		norm := func(v string) string {
			if t := truthOf(v); t != "" {
				return t
			}
			return truthOf(textOf[strings.ToUpper(strings.TrimSpace(v))])
		}
		want := norm(answer)
		if want == "" || len(selected) != 1 {
			return false, want != ""
		}
		return norm(selected[0]) == want, true
	case "fill_blank":
		split := func(s string) []string {
			f := strings.FieldsFunc(s, func(r rune) bool { return r == '；' || r == ';' || r == '|' })
			for i := range f {
				f[i] = normText(f[i])
			}
			return f
		}
		want, got := split(answer), split(text)
		if len(want) == 0 {
			return false, false
		}
		if len(got) != len(want) {
			return false, true
		}
		for i := range want {
			if want[i] != got[i] {
				return false, true
			}
		}
		return true, true
	}
	return false, false
}
