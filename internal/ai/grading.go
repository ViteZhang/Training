package ai

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"
)

// GradePoint 是一个采分点（批改依据，来自用户资料或确认过的采分点）。
type GradePoint struct {
	Seq      int      `json:"seq"`
	Content  string   `json:"content"`
	Score    float64  `json:"score"`
	Keywords []string `json:"keywords,omitempty"`
}

// GradeIn 是主观题批改的输入（PRD 12.1：题目、采分点、参考答案、用户答案 → 逐采分点判定）。
type GradeIn struct {
	Subject   string       `json:"subject"`
	QType     string       `json:"qtype"`
	Stem      string       `json:"stem"`
	Reference string       `json:"reference"`
	Points    []GradePoint `json:"points"`
	Answer    string       `json:"answer"`
}

// GradeResult 是一个采分点的判定。
type GradeResult struct {
	Seq     int     `json:"seq"`
	Verdict string  `json:"verdict"` // hit / partial / miss
	Score   float64 `json:"score"`
	Quote   string  `json:"quote"`
	Reason  string  `json:"reason"`
}

type GradeOut struct {
	Points        []GradeResult `json:"points"`
	StructureOK   bool          `json:"structure_ok"`
	StructureNote string        `json:"structure_note"`
	Suggestions   []string      `json:"suggestions"`
}

// Total 是各采分点得分之和。
func (o GradeOut) Total() float64 {
	t := 0.0
	for _, p := range o.Points {
		t += p.Score
	}
	return math.Round(t*2) / 2
}

// compact 去掉空白，用于判断引用是否是考生原话（模型常改动空格与换行）。
func compact(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

var Grade = &Cap[GradeIn, GradeOut]{
	Def: Def{Name: "grade_subjective", Version: "v1", Tier: "strong", Temperature: 0, Schema: `{
		"type": "object", "required": ["points", "structure_ok", "suggestions"],
		"properties": {
			"points": {"type": "array", "items": {"type": "object", "required": ["seq", "verdict", "score"],
				"properties": {
					"seq": {"type": "integer"},
					"verdict": {"type": "string", "enum": ["hit", "partial", "miss"]},
					"score": {"type": "number", "minimum": 0},
					"quote": {"type": "string"},
					"reason": {"type": "string"}
				}}},
			"structure_ok": {"type": "boolean"},
			"structure_note": {"type": "string"},
			"suggestions": {"type": "array", "maxItems": 5, "items": {"type": "string"}}
		}}`},
	// Check 是批改的兜底校验（PRD 12.2）：每个采分点都要判、分值不超过采分点分值、命中满分 / 遗漏 0 分、
	// 引用必须是考生原话；不合格重试一次，再失败按「批改失败，未扣次数」处理。
	Check: func(in GradeIn, out *GradeOut) error {
		want := map[int]GradePoint{}
		for _, p := range in.Points {
			want[p.Seq] = p
		}
		seen := map[int]bool{}
		ans := compact(in.Answer)
		for i := range out.Points {
			r := &out.Points[i]
			p, ok := want[r.Seq]
			if !ok || seen[r.Seq] {
				return fmt.Errorf("采分点序号 %d 不对", r.Seq)
			}
			seen[r.Seq] = true
			switch r.Verdict {
			case "hit":
				r.Score = p.Score
			case "miss":
				r.Score, r.Quote = 0, ""
			default:
				if r.Score > p.Score {
					return fmt.Errorf("采分点 %d 得分超过分值", r.Seq)
				}
				if r.Score >= p.Score {
					r.Score = p.Score / 2
				}
			}
			if r.Verdict != "miss" {
				if strings.TrimSpace(r.Quote) == "" || !strings.Contains(ans, compact(r.Quote)) {
					return fmt.Errorf("采分点 %d 的引用不是考生原话", r.Seq)
				}
			}
		}
		if len(seen) != len(want) {
			return errors.New("有采分点没有判定")
		}
		sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].Seq < out.Points[j].Seq })
		for _, w := range []string{"保证得分", "一定能考", "稳拿"} {
			for _, s := range out.Suggestions {
				if strings.Contains(s, w) {
					return errors.New("建议里有承诺分数的说法")
				}
			}
		}
		return nil
	},
	Mock: mockGrade,
}

// mockGrade 按关键词覆盖判定（本地与测试用，确定性）：采分点的关键词（没有时取内容里的两字词）
// 在答案里出现 ≥ 60% 为命中，≥ 30% 为部分命中；引用取答案里覆盖最多的一句。
func mockGrade(in GradeIn) (GradeOut, error) {
	sentences := splitSentences(in.Answer)
	out := GradeOut{StructureOK: len([]rune(in.Answer)) >= 30, Suggestions: []string{}}
	for _, p := range in.Points {
		keys := p.Keywords
		if len(keys) == 0 {
			keys = bigrams(p.Content)
		}
		hit, best, bestN := 0, "", 0
		for _, k := range keys {
			if strings.Contains(in.Answer, k) {
				hit++
			}
		}
		for _, s := range sentences {
			n := 0
			for _, k := range keys {
				if strings.Contains(s, k) {
					n++
				}
			}
			if n > bestN {
				best, bestN = s, n
			}
		}
		r := GradeResult{Seq: p.Seq, Verdict: "miss", Reason: "没有写到「" + truncateRunes(p.Content, 20) + "」"}
		ratio := 0.0
		if len(keys) > 0 {
			ratio = float64(hit) / float64(len(keys))
		}
		switch {
		case ratio >= 0.6 && best != "":
			r.Verdict, r.Score, r.Quote, r.Reason = "hit", p.Score, best, ""
		case ratio >= 0.3 && best != "":
			r.Verdict, r.Score, r.Quote, r.Reason = "partial", p.Score/2, best, "写到了一部分，还缺关键表述"
		}
		if r.Verdict != "hit" {
			out.Suggestions = append(out.Suggestions, "补上「"+truncateRunes(p.Content, 30)+"」")
		}
		out.Points = append(out.Points, r)
	}
	if !out.StructureOK {
		out.StructureNote = "答案太短，先下定义，再分点写特征和意义"
	}
	if len(out.Suggestions) > 3 {
		out.Suggestions = out.Suggestions[:3]
	}
	return out, nil
}

func splitSentences(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return strings.ContainsRune("。！？；\n", r) }) {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// bigrams 取内容里的中文两字词（去掉标点和常见虚词），作为没有关键词时的比对单位。
func bigrams(s string) []string {
	var rs []rune
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.IsLetter(r) || unicode.IsDigit(r) {
			rs = append(rs, r)
		} else {
			rs = append(rs, ' ')
		}
	}
	seen := map[string]bool{}
	var out []string
	for i := 0; i+1 < len(rs); i += 2 {
		if rs[i] == ' ' || rs[i+1] == ' ' {
			continue
		}
		w := string(rs[i : i+2])
		if !seen[w] && !strings.ContainsAny(w, "的了是和与在") {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
