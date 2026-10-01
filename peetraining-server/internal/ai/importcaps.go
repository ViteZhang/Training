package ai

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// 导入流水线用到的能力（dev-spec 第六节第 6–9 步，PRD v3 12.1）。

// Block 是规则切出来的一块原文（第 5 步），交给模型结构化。
type Block struct {
	ID      int     `json:"id"`
	Page    int     `json:"page"`
	Section string  `json:"section,omitempty"` // 所在题型标题，如「一、名词解释（每题 5 分）」
	QType   string  `json:"qtype,omitempty"`   // 规则从标题判断的题型，可能为空
	Score   float64 `json:"score,omitempty"`   // 标题里的「每题 N 分」
	Year    int     `json:"year,omitempty"`
	No      string  `json:"no,omitempty"`         // 规则识别的题号
	Answer  bool    `json:"is_answer,omitempty"`  // 位于「参考答案」部分
	Whole   bool    `json:"whole_page,omitempty"` // 规则切不开的整页
	Text    string  `json:"text"`
}

// StructureIn 是题目结构化的输入。
type StructureIn struct {
	Subject string  `json:"subject"`
	Blocks  []Block `json:"blocks"`
}

// Option 是选择题选项。
type Option struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// QItem 是结构化出的一条：题目，或只有答案（答案在单独文件或书后，第 7 步按年份 + 题号配对）。
type QItem struct {
	Block          int      `json:"block"`
	Kind           string   `json:"kind"`
	QType          string   `json:"qtype,omitempty"`
	Stem           string   `json:"stem,omitempty"`
	Options        []Option `json:"options,omitempty"`
	Answer         string   `json:"answer,omitempty"`
	Score          *float64 `json:"score,omitempty"`
	ExamYear       *int     `json:"exam_year,omitempty"`
	QuestionNo     string   `json:"question_no,omitempty"`
	Page           int      `json:"page"`
	Confidence     float64  `json:"confidence"`
	IsRecollection bool     `json:"is_recollection,omitempty"`
}

type StructureOut struct {
	Items []QItem `json:"items"`
}

// QTypes 是题型（与 questions.qtype 一致）。
var QTypes = []string{"single_choice", "multi_choice", "true_false", "fill_blank", "term", "short_answer", "discussion", "essay", "calculation", "other"}

// Subjective 判断是否主观题（按采分点批改）。
func Subjective(qtype string) bool {
	switch qtype {
	case "term", "short_answer", "discussion", "essay":
		return true
	}
	return false
}

var Structure = &Cap[StructureIn, StructureOut]{
	Def: Def{Name: "import_structure", Version: "v1", Tier: "strong", Temperature: 0.1, Schema: `{
		"type": "object", "required": ["items"],
		"properties": {"items": {"type": "array", "items": {
			"type": "object", "required": ["block", "kind", "page", "confidence"],
			"properties": {
				"block": {"type": "integer"},
				"kind": {"type": "string", "enum": ["question", "answer"]},
				"qtype": {"type": "string", "enum": ["single_choice","multi_choice","true_false","fill_blank","term","short_answer","discussion","essay","calculation","other"]},
				"stem": {"type": "string"},
				"options": {"type": "array", "items": {"type": "object", "required": ["key","text"], "properties": {"key": {"type": "string"}, "text": {"type": "string"}}}},
				"answer": {"type": "string"},
				"score": {"type": "number", "minimum": 0, "maximum": 300},
				"exam_year": {"type": "integer", "minimum": 1977, "maximum": 2100},
				"question_no": {"type": "string", "maxLength": 16},
				"page": {"type": "integer", "minimum": 1},
				"confidence": {"type": "number", "minimum": 0, "maximum": 1},
				"is_recollection": {"type": "boolean"}
			}}}}}`},
	Check: checkStructure,
	Mock:  mockStructure,
}

// checkStructure：引用的块必须存在；题目必须有题型与题干，题干要来自这块原文（防止编造）；答案条目要有题号与答案。
func checkStructure(in StructureIn, out *StructureOut) error {
	blocks := map[int]Block{}
	for _, b := range in.Blocks {
		blocks[b.ID] = b
	}
	for i, it := range out.Items {
		b, ok := blocks[it.Block]
		if !ok {
			return fmt.Errorf("第 %d 条引用了不存在的块 %d", i+1, it.Block)
		}
		switch it.Kind {
		case "question":
			if it.QType == "" || strings.TrimSpace(it.Stem) == "" {
				return fmt.Errorf("第 %d 条缺题型或题干", i+1)
			}
			if Overlap(it.Stem, b.Text) < 0.8 {
				return fmt.Errorf("第 %d 条题干不在原文里", i+1)
			}
		case "answer":
			if it.QuestionNo == "" || strings.TrimSpace(it.Answer) == "" {
				return fmt.Errorf("第 %d 条答案缺题号或内容", i+1)
			}
		}
	}
	return nil
}

var (
	reInlineAnswer = regexp.MustCompile(`(?s)^(.*?)\s*(?:【答案】|【参考答案】|参考答案[:：]|答案[:：]|答[:：])\s*(.*)$`)
	reOption       = regexp.MustCompile(`(?m)^\s*([A-H])[.．、:：)]\s*(.+)$`)
	reLeadNo       = regexp.MustCompile(`^\s*(?:[（(]\s*)?\d{1,3}\s*[.．、)）]\s*`)
)

// mockStructure 按规则结构化（开发期与测试用）：块文本里「答案：」之后是答案，A. B. 开头的行是选项。
func mockStructure(in StructureIn) (StructureOut, error) {
	var out StructureOut
	for _, b := range in.Blocks {
		if b.Whole {
			continue // 规则切不开的整页只有真实模型能处理
		}
		text := reLeadNo.ReplaceAllString(strings.TrimSpace(b.Text), "")
		it := QItem{Block: b.ID, Page: b.Page, QuestionNo: b.No, Confidence: 0.9}
		if b.Year > 0 {
			y := b.Year
			it.ExamYear = &y
		}
		if b.Answer {
			it.Kind, it.Answer = "answer", text
			if it.QuestionNo == "" || it.Answer == "" {
				continue
			}
			out.Items = append(out.Items, it)
			continue
		}
		it.Kind = "question"
		if m := reInlineAnswer.FindStringSubmatch(text); m != nil {
			text, it.Answer = strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		}
		if opts := reOption.FindAllStringSubmatchIndex(text, -1); len(opts) >= 2 {
			for _, o := range opts {
				it.Options = append(it.Options, Option{Key: text[o[2]:o[3]], Text: strings.TrimSpace(text[o[4]:o[5]])})
			}
			text = strings.TrimSpace(text[:opts[0][0]])
		}
		it.Stem = text
		it.QType = b.QType
		if it.QType == "" {
			switch {
			case len(it.Options) > 0:
				it.QType = "single_choice"
			case len([]rune(it.Stem)) <= 12:
				it.QType = "term"
			default:
				it.QType = "short_answer"
			}
			it.Confidence = 0.6
		}
		if it.QType == "single_choice" && len([]rune(strings.ReplaceAll(it.Answer, " ", ""))) > 1 && reAllLetters.MatchString(it.Answer) {
			it.QType = "multi_choice"
		}
		if b.Score > 0 {
			s := b.Score
			it.Score = &s
		}
		if it.Stem == "" {
			continue
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}

var reAllLetters = regexp.MustCompile(`^[A-H\s,，、]+$`)

// RubricIn 是采分点提取的输入（第 8 步）：采分点来自参考答案原文。
type RubricIn struct {
	QType  string  `json:"qtype"`
	Stem   string  `json:"stem"`
	Answer string  `json:"answer"`
	Score  float64 `json:"score"`
}

// RubricPoint 是一个采分点。
type RubricPoint struct {
	Content  string   `json:"content"`
	Score    float64  `json:"score"`
	Keywords []string `json:"keywords"`
}

type RubricOut struct {
	Points []RubricPoint `json:"points"`
}

// rubricPoints 是采分点数组的 Schema，采分点提取与生成参考答案共用。
const rubricPoints = `{"type": "array", "minItems": 1, "maxItems": 20, "items": {
	"type": "object", "required": ["content", "score", "keywords"],
	"properties": {
		"content": {"type": "string", "minLength": 1},
		"score": {"type": "number", "minimum": 0},
		"keywords": {"type": "array", "items": {"type": "string", "minLength": 1}}
	}}}`

const rubricSchema = `{"type": "object", "required": ["points"], "properties": {"points": ` + rubricPoints + `}}`

var ExtractRubric = &Cap[RubricIn, RubricOut]{
	Def: Def{Name: "rubric_extract", Version: "v1", Tier: "strong", Temperature: 0.1, Schema: rubricSchema},
	// 采分关键词必须能在参考答案里逐字找到（背诵挖空、批改比对都靠它）。
	Check: func(in RubricIn, out *RubricOut) error {
		for i, p := range out.Points {
			for _, k := range p.Keywords {
				if !ContainsVerbatim(in.Answer, k) {
					return fmt.Errorf("第 %d 个采分点的关键词「%s」不在参考答案里", i+1, k)
				}
			}
		}
		return nil
	},
	Mock: func(in RubricIn) (RubricOut, error) { return RubricOut{Points: splitPoints(in.Answer, in.Score)}, nil },
}

var (
	reSentence = regexp.MustCompile(`[^。；;！？\n]+[。；;！？]?`)
	reEnum     = regexp.MustCompile(`^\s*(?:[（(]?\d{1,2}[)）.．、]|[①-⑳]|[一二三四五六七八九十]+[、.．])\s*`)
)

// splitPoints 按句拆采分点并平均分配分值（按 0.5 分取整，余数给最后一个），关键词取每句开头的短语。
func splitPoints(answer string, score float64) []RubricPoint {
	var parts []string
	for _, s := range reSentence.FindAllString(answer, -1) {
		s = strings.TrimSpace(reEnum.ReplaceAllString(strings.TrimSpace(s), ""))
		if len([]rune(s)) >= 2 {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		parts = []string{strings.TrimSpace(answer)}
	}
	if len(parts) > 8 {
		parts = parts[:8]
	}
	each := math.Floor(score/float64(len(parts))*2) / 2
	pts := make([]RubricPoint, len(parts))
	total := 0.0
	for i, p := range parts {
		kw := []rune(strings.TrimRight(p, "。；;！？，,"))
		if len(kw) > 6 {
			kw = kw[:6]
		}
		pts[i] = RubricPoint{Content: p, Score: each, Keywords: []string{string(kw)}}
		total += each
	}
	pts[len(pts)-1].Score += score - total
	return pts
}

// GenerateAnswerIn 是缺答案时生成参考答案与采分点的输入（1.7b，标「AI 生成」）。
type GenerateAnswerIn struct {
	Subject string  `json:"subject"`
	QType   string  `json:"qtype"`
	Stem    string  `json:"stem"`
	Score   float64 `json:"score"`
}

type GenerateAnswerOut struct {
	Answer string        `json:"answer"`
	Points []RubricPoint `json:"points"`
}

var GenerateAnswer = &Cap[GenerateAnswerIn, GenerateAnswerOut]{
	Def: Def{Name: "answer_generate", Version: "v1", Tier: "strong", Temperature: 0.3, Schema: `{
		"type": "object", "required": ["answer", "points"],
		"properties": {
			"answer": {"type": "string", "minLength": 1},
			"points": ` + rubricPoints + `}}`},
	Check: func(in GenerateAnswerIn, out *GenerateAnswerOut) error {
		if in.Score > 0 {
			sum := 0.0
			for _, p := range out.Points {
				sum += p.Score
			}
			if math.Abs(sum-in.Score) > 0.01 {
				return fmt.Errorf("采分点合计 %.1f 不等于题目分值 %.1f", sum, in.Score)
			}
		}
		return nil
	},
	Mock: func(in GenerateAnswerIn) (GenerateAnswerOut, error) {
		ans := "（AI 生成）" + in.Stem + "的要点：一是概念与内涵；二是特征与表现；三是意义与影响。"
		return GenerateAnswerOut{Answer: ans, Points: splitPoints(ans, in.Score)}, nil
	},
}

// KPIn 是参考资料拆知识点的输入（参考类资料，第 9 步）：按页给原文。
type KPIn struct {
	Subject string   `json:"subject"`
	Pages   []KPPage `json:"pages"`
}

type KPPage struct {
	No   int    `json:"no"`
	Text string `json:"text"`
}

// KPItem 是一个知识点：原文表述必须能在所在页逐字找到（CLAUDE.md 必须遵守第 8 条）。
type KPItem struct {
	Name         string        `json:"name"`
	OriginalText string        `json:"original_text"`
	Page         int           `json:"page"`
	Path         []string      `json:"path"`
	RubricPoints []RubricPoint `json:"rubric_points"`
}

type KPOut struct {
	Points []KPItem `json:"points"`
}

var ExtractKPs = &Cap[KPIn, KPOut]{
	Def: Def{Name: "kp_extract", Version: "v1", Tier: "strong", Temperature: 0.1, Schema: `{
		"type": "object", "required": ["points"],
		"properties": {"points": {"type": "array", "items": {
			"type": "object", "required": ["name", "original_text", "page", "path", "rubric_points"],
			"properties": {
				"name": {"type": "string", "minLength": 1, "maxLength": 128},
				"original_text": {"type": "string", "minLength": 1},
				"page": {"type": "integer", "minimum": 1},
				"path": {"type": "array", "minItems": 1, "maxItems": 2, "items": {"type": "string", "minLength": 1, "maxLength": 128}},
				"rubric_points": {"type": "array", "items": {"type": "object", "required": ["content"], "properties": {
					"content": {"type": "string", "minLength": 1}, "score": {"type": "number"}, "keywords": {"type": "array", "items": {"type": "string"}}}}}
			}}}}}`},
	Check: func(in KPIn, out *KPOut) error {
		pages := map[int]string{}
		for _, p := range in.Pages {
			pages[p.No] = p.Text
		}
		for i, k := range out.Points {
			text, ok := pages[k.Page]
			if !ok {
				return fmt.Errorf("第 %d 个知识点的页码 %d 不在输入里", i+1, k.Page)
			}
			if !ContainsVerbatim(text, k.OriginalText) {
				return fmt.Errorf("第 %d 个知识点「%s」的原文表述在第 %d 页找不到", i+1, k.Name, k.Page)
			}
		}
		return nil
	},
	Mock: mockKPs,
}

var (
	reChapter = regexp.MustCompile(`^\s*(第[一二三四五六七八九十百\d]+[章编部分讲]\s*\S.*)$`)
	reSection = regexp.MustCompile(`^\s*([一二三四五六七八九十]+[、.．]\s*\S.*)$`)
	reDefine  = regexp.MustCompile(`^\s*(?:[（(]?\d{1,2}[)）.．、]\s*)?([^：:，。]{2,20})[：:]\s*(\S.+)$`)
)

// mockKPs 按规则拆知识点：「第 N 章」与「一、」行作板块与章节，「名称：解释」的段落作知识点。
func mockKPs(in KPIn) (KPOut, error) {
	var out KPOut
	chapter, section := "未分章", ""
	for _, p := range in.Pages {
		for _, line := range strings.Split(p.Text, "\n") {
			switch {
			case reChapter.MatchString(line):
				chapter, section = strings.TrimSpace(line), ""
			case reSection.MatchString(line) && !strings.ContainsAny(line, "：:"):
				section = strings.TrimSpace(line)
			default:
				m := reDefine.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				path := []string{chapter}
				if section != "" {
					path = append(path, section)
				}
				orig := strings.TrimSpace(line)
				out.Points = append(out.Points, KPItem{
					Name: strings.TrimSpace(m[1]), OriginalText: orig, Page: p.No, Path: path,
					RubricPoints: splitPoints(strings.TrimSpace(m[2]), 0),
				})
			}
		}
	}
	return out, nil
}

// TagIn 是给题目打知识点标签的输入（第 9 步）：已有的知识点路径优先复用，题库成一棵树。
type TagIn struct {
	Subject   string     `json:"subject"`
	Existing  [][]string `json:"existing_paths"`
	Questions []TagQ     `json:"questions"`
}

type TagQ struct {
	ID     int    `json:"id"`
	QType  string `json:"qtype"`
	Stem   string `json:"stem"`
	Answer string `json:"answer,omitempty"`
}

// Tag 是一道题的知识点：路径为 板块 / 章节 / 知识点。
type Tag struct {
	ID   int      `json:"id"`
	Path []string `json:"path"`
}

type TagOut struct {
	Tags []Tag `json:"tags"`
}

var TagQuestions = &Cap[TagIn, TagOut]{
	Def: Def{Name: "kp_tag", Version: "v1", Tier: "cheap", Temperature: 0.1, Schema: `{
		"type": "object", "required": ["tags"],
		"properties": {"tags": {"type": "array", "items": {
			"type": "object", "required": ["id", "path"],
			"properties": {
				"id": {"type": "integer"},
				"path": {"type": "array", "minItems": 3, "maxItems": 3, "items": {"type": "string", "minLength": 1, "maxLength": 128}}
			}}}}}`},
	Check: func(in TagIn, out *TagOut) error {
		ids := map[int]bool{}
		for _, q := range in.Questions {
			ids[q.ID] = true
		}
		for _, t := range out.Tags {
			if !ids[t.ID] {
				return fmt.Errorf("标签引用了不存在的题 %d", t.ID)
			}
		}
		if len(out.Tags) == 0 && len(in.Questions) > 0 {
			return errors.New("没有给出任何标签")
		}
		return nil
	},
	Mock: func(in TagIn) (TagOut, error) {
		var out TagOut
		for _, q := range in.Questions {
			name := []rune(strings.TrimSpace(q.Stem))
			if len(name) > 20 {
				name = name[:20]
			}
			out.Tags = append(out.Tags, Tag{ID: q.ID, Path: []string{"待整理", "待整理", string(name)}})
		}
		return out, nil
	},
}
