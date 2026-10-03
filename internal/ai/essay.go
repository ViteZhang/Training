package ai

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// EssayBand 是评分维度的一档。
type EssayBand struct {
	Range       string `json:"range"`
	Description string `json:"description"`
}

// EssayDim 是一个评分维度（用户资料里的评分细则，或通用五维度，PRD 11.13）。
type EssayDim struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Score       float64     `json:"score"`
	Bands       []EssayBand `json:"bands,omitempty"`
}

// EssayGradeIn 是作文批改的输入（PRD 12.1：题目、全文、评分标准 → 分维度得分、总评、逐段批注）。
type EssayGradeIn struct {
	Subject       string     `json:"subject"`
	Topic         string     `json:"topic"`
	RequiredWords int        `json:"required_words"`
	Dimensions    []EssayDim `json:"dimensions"`
	// Paragraphs 是按段拆开的正文，批注用段号（从 1 开始）定位。
	Paragraphs []string `json:"paragraphs"`
}

// EssayDimScore 是一个维度的得分与一句评语。
type EssayDimScore struct {
	Name    string  `json:"name"`
	Score   float64 `json:"score"`
	Comment string  `json:"comment"`
}

// EssayAnnotation 是逐段批注：在第几段的哪句话有什么问题，怎么改。
type EssayAnnotation struct {
	Paragraph  int    `json:"paragraph"`
	Quote      string `json:"quote"`
	Issue      string `json:"issue"`
	Suggestion string `json:"suggestion"`
}

type EssayGradeOut struct {
	Thesis      string            `json:"thesis"`
	Dimensions  []EssayDimScore   `json:"dimensions"`
	Highlights  []string          `json:"highlights"`
	Problems    []string          `json:"problems"`
	Suggestions []string          `json:"suggestions"`
	Annotations []EssayAnnotation `json:"annotations"`
}

// Total 是各维度得分之和，取到 0.5 分。
func (o EssayGradeOut) Total() float64 {
	t := 0.0
	for _, d := range o.Dimensions {
		t += d.Score
	}
	return math.Round(t*2) / 2
}

var promiseWords = []string{"保证得分", "一定能考", "稳拿", "必定高分"}

var EssayGrade = &Cap[EssayGradeIn, EssayGradeOut]{
	Def: Def{Name: "essay_grade", Version: "v1", Tier: "strong", Temperature: 0, Schema: `{
		"type": "object", "required": ["thesis", "dimensions", "highlights", "problems", "suggestions", "annotations"],
		"properties": {
			"thesis": {"type": "string"},
			"dimensions": {"type": "array", "items": {"type": "object", "required": ["name", "score", "comment"],
				"properties": {"name": {"type": "string"}, "score": {"type": "number", "minimum": 0}, "comment": {"type": "string"}}}},
			"highlights": {"type": "array", "maxItems": 5, "items": {"type": "string"}},
			"problems": {"type": "array", "maxItems": 5, "items": {"type": "string"}},
			"suggestions": {"type": "array", "minItems": 1, "maxItems": 5, "items": {"type": "string"}},
			"annotations": {"type": "array", "maxItems": 12, "items": {"type": "object", "required": ["paragraph", "quote", "issue", "suggestion"],
				"properties": {"paragraph": {"type": "integer", "minimum": 1}, "quote": {"type": "string"}, "issue": {"type": "string"}, "suggestion": {"type": "string"}}}}
		}}`},
	// Check 是作文批改的兜底校验：每个维度都要打分、不超过该维度分值；批注的段号存在、引用是该段原话；不承诺分数。
	// 不合格重试一次，再失败按「批改失败，未扣次数」处理。
	Check: func(in EssayGradeIn, out *EssayGradeOut) error {
		want := map[string]float64{}
		order := map[string]int{}
		for i, d := range in.Dimensions {
			want[d.Name], order[d.Name] = d.Score, i
		}
		got := make([]EssayDimScore, len(in.Dimensions))
		seen := map[string]bool{}
		for _, d := range out.Dimensions {
			max, ok := want[d.Name]
			if !ok || seen[d.Name] {
				return fmt.Errorf("评分维度「%s」不在评分标准里", d.Name)
			}
			if d.Score < 0 || d.Score > max {
				return fmt.Errorf("维度「%s」得分超过分值", d.Name)
			}
			seen[d.Name] = true
			d.Score = math.Round(d.Score*2) / 2
			got[order[d.Name]] = d
		}
		if len(seen) != len(want) {
			return errors.New("有评分维度没有打分")
		}
		out.Dimensions = got
		for i := range out.Annotations {
			a := &out.Annotations[i]
			if a.Paragraph < 1 || a.Paragraph > len(in.Paragraphs) {
				return fmt.Errorf("批注段号 %d 不存在", a.Paragraph)
			}
			if strings.TrimSpace(a.Quote) == "" || !strings.Contains(compact(in.Paragraphs[a.Paragraph-1]), compact(a.Quote)) {
				return fmt.Errorf("第 %d 段批注的引用不是原文", a.Paragraph)
			}
		}
		for _, list := range [][]string{out.Highlights, out.Problems, out.Suggestions} {
			for _, s := range list {
				for _, w := range promiseWords {
					if strings.Contains(s, w) {
						return errors.New("总评里有承诺分数的说法")
					}
				}
			}
		}
		return nil
	},
	Mock: mockEssayGrade,
}

// mockEssayGrade 按字数完成度与分段情况打分（本地与测试用，确定性）：每个维度给分值的 40%–90%；
// 批注取最短一段的第一句。
func mockEssayGrade(in EssayGradeIn) (EssayGradeOut, error) {
	chars := 0
	var paras []string
	for _, p := range in.Paragraphs {
		if strings.TrimSpace(p) != "" {
			paras = append(paras, p)
			chars += utf8.RuneCountInString(compact(p))
		}
	}
	req := in.RequiredWords
	if req <= 0 {
		req = 800
	}
	ratio := math.Min(float64(chars)/float64(req), 1)
	structure := 0.0
	if len(paras) >= 4 {
		structure = 0.1
	}
	out := EssayGradeOut{Highlights: []string{}, Problems: []string{}, Annotations: []EssayAnnotation{}}
	for _, d := range in.Dimensions {
		f := 0.4 + 0.4*ratio + structure
		out.Dimensions = append(out.Dimensions, EssayDimScore{Name: d.Name, Score: math.Round(d.Score*f*2) / 2, Comment: d.Name + "基本达到要求"})
	}
	if len(paras) > 0 {
		out.Thesis = firstSentence(paras[0])
	}
	if len(paras) >= 4 {
		out.Highlights = append(out.Highlights, "结构完整，分段清楚")
	} else {
		out.Problems = append(out.Problems, "分段偏少，论证层次不够清楚")
	}
	if ratio < 1 {
		out.Problems = append(out.Problems, fmt.Sprintf("字数不足（%d / %d）", chars, req))
	}
	out.Suggestions = []string{"每个分论点配一个具体论据，结尾回扣题目"}
	short := -1
	for i, p := range in.Paragraphs {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if short < 0 || utf8.RuneCountInString(p) < utf8.RuneCountInString(in.Paragraphs[short]) {
			short = i
		}
	}
	if short >= 0 {
		out.Annotations = append(out.Annotations, EssayAnnotation{Paragraph: short + 1, Quote: firstSentence(in.Paragraphs[short]),
			Issue: "这一段展开不够", Suggestion: "补一个具体论据，再说明它如何支撑分论点"})
	}
	return out, nil
}

func firstSentence(p string) string {
	p = strings.TrimSpace(p)
	if i := strings.IndexAny(p, "。！？!?"); i >= 0 {
		_, size := utf8.DecodeRuneInString(p[i:])
		return p[:i+size]
	}
	return p
}

// EssayPromptIn 是 AI 命题的输入：用户导入的作文真题（看命题方式），以及最近出过的题（不重复）。
type EssayPromptIn struct {
	Subject    string   `json:"subject"`
	PastTopics []string `json:"past_topics"`
	Avoid      []string `json:"avoid"`
}

type EssayPromptOut struct {
	Topic         string `json:"topic"`
	RequiredWords int    `json:"required_words"`
	Note          string `json:"note"`
}

var EssayPrompt = &Cap[EssayPromptIn, EssayPromptOut]{
	Def: Def{Name: "essay_topic", Version: "v1", Tier: "strong", Temperature: 0.8, Schema: `{
		"type": "object", "required": ["topic", "required_words"],
		"properties": {
			"topic": {"type": "string", "minLength": 4},
			"required_words": {"type": "integer", "minimum": 300, "maximum": 3000},
			"note": {"type": "string"}
		}}`},
	Check: func(in EssayPromptIn, out *EssayPromptOut) error {
		t := compact(out.Topic)
		for _, p := range append(append([]string{}, in.PastTopics...), in.Avoid...) {
			if compact(p) == t {
				return errors.New("和已有的题目重复")
			}
		}
		return nil
	},
	Mock: mockEssayPrompt,
}

var mockTopics = []string{"以「守正与创新」为题，写一篇议论文", "围绕「慢下来」写一篇文章，题目自拟", "以「传统的力量」为话题，写一篇议论文", "读材料「一棵树要长高，先要扎根」，自选角度写一篇文章"}

func mockEssayPrompt(in EssayPromptIn) (EssayPromptOut, error) {
	words := 800
	for i := range mockTopics {
		t := mockTopics[(i+len(in.Avoid))%len(mockTopics)]
		dup := false
		for _, p := range append(append([]string{}, in.PastTopics...), in.Avoid...) {
			if compact(p) == compact(t) {
				dup = true
			}
		}
		if !dup {
			return EssayPromptOut{Topic: t, RequiredWords: words, Note: "参照你导入的真题命题方式"}, nil
		}
	}
	return EssayPromptOut{}, errors.New("出不了新题")
}
