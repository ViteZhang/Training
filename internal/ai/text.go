package ai

import (
	"strings"
	"unicode"
)

// Compact 去掉全部空白，用于「原文逐字一致」的比较：资料取文本时的换行、空格不算差异。
func Compact(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// ContainsVerbatim 判断 needle 是否能在 hay 里逐字找到（忽略空白）。
func ContainsVerbatim(hay, needle string) bool {
	n := Compact(needle)
	return n != "" && strings.Contains(Compact(hay), n)
}

// Overlap 返回 a 的二字片段在 b 里出现的比例，用来判断模型给的题干是不是来自这块原文（防止编造）。
func Overlap(a, b string) float64 {
	ra, rb := []rune(Compact(a)), Compact(b)
	if len(ra) < 2 {
		if strings.Contains(rb, string(ra)) {
			return 1
		}
		return 0
	}
	hit := 0
	for i := 0; i+1 < len(ra); i++ {
		if strings.Contains(rb, string(ra[i:i+2])) {
			hit++
		}
	}
	return float64(hit) / float64(len(ra)-1)
}
