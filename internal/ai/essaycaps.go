package ai

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ClassifyIn 是资料类型判断的输入（PRD 11.12）：取前几页文字即可。
type ClassifyIn struct {
	FileName string   `json:"file_name"`
	Pages    []KPPage `json:"pages"`
}

// ClassifyOut：category 是 question 题目类 / reference 参考类 / essay 作文类；sub_type 是细分类型。
type ClassifyOut struct {
	Category string `json:"category"`
	SubType  string `json:"sub_type"`
}

// SubTypes 是各类资料的细分类型（materials.sub_type）。
var SubTypes = map[string][]string{
	"question":  {"真题汇编", "习题集"},
	"reference": {"参考书", "讲义", "笔记"},
	"essay":     {"作文真题", "范文", "写作笔记", "评分细则"},
}

var Classify = &Cap[ClassifyIn, ClassifyOut]{
	Def: Def{Name: "material_classify", Version: "v1", Tier: "cheap", Temperature: 0, Schema: `{
		"type": "object", "required": ["category", "sub_type"],
		"properties": {
			"category": {"type": "string", "enum": ["question", "reference", "essay"]},
			"sub_type": {"type": "string"}
		}}`},
	Check: func(_ ClassifyIn, out *ClassifyOut) error {
		for _, s := range SubTypes[out.Category] {
			if s == out.SubType {
				return nil
			}
		}
		return fmt.Errorf("细分类型「%s」不属于 %s", out.SubType, out.Category)
	},
	Mock: mockClassify,
}

var (
	reEssayHint = regexp.MustCompile(`作文|范文|评分细则|评分标准|写作|素材`)
	reExamHint  = regexp.MustCompile(`真题|试题|试卷|考研`)
	reQNo       = regexp.MustCompile(`(?m)^\s*\d{1,3}\s*[.．、]`)
)

// mockClassify 按关键词判断：作文相关词多 → 作文类；题号多 → 题目类；其余参考类。
func mockClassify(in ClassifyIn) (ClassifyOut, error) {
	var b strings.Builder
	b.WriteString(in.FileName)
	for _, p := range in.Pages {
		b.WriteString("\n")
		b.WriteString(p.Text)
	}
	text := b.String()
	essay := len(reEssayHint.FindAllString(text, -1))
	qnos := len(reQNo.FindAllString(text, -1))
	switch {
	case essay >= 2 && essay*2 >= qnos:
		switch {
		case strings.Contains(text, "评分细则") || strings.Contains(text, "评分标准"):
			return ClassifyOut{"essay", "评分细则"}, nil
		case strings.Contains(text, "范文"):
			return ClassifyOut{"essay", "范文"}, nil
		case reExamHint.MatchString(text):
			return ClassifyOut{"essay", "作文真题"}, nil
		}
		return ClassifyOut{"essay", "写作笔记"}, nil
	case qnos >= 2:
		if reExamHint.MatchString(text) {
			return ClassifyOut{"question", "真题汇编"}, nil
		}
		return ClassifyOut{"question", "习题集"}, nil
	}
	return ClassifyOut{"reference", "讲义"}, nil
}

// EssayIn 是作文资料整理的输入（T11）：按页给原文。
type EssayIn struct {
	Subject string   `json:"subject"`
	Pages   []KPPage `json:"pages"`
}

// EssayTopic 是作文真题题目。
type EssayTopic struct {
	Title         string `json:"title"`
	Year          *int   `json:"year,omitempty"`
	RequiredWords *int   `json:"required_words,omitempty"`
	Page          int    `json:"page"`
}

// RubricBand 是一个分档。
type RubricBand struct {
	Range       string `json:"range"`
	Description string `json:"description"`
}

// RubricDimension 是评分维度。
type RubricDimension struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Score       float64      `json:"score"`
	Bands       []RubricBand `json:"bands"`
}

// EssayRubric 是评分细则。
type EssayRubric struct {
	Name       string            `json:"name"`
	FullScore  float64           `json:"full_score"`
	Dimensions []RubricDimension `json:"dimensions"`
	Page       int               `json:"page"`
}

// WritingMethod 是写作方法（由 AI 从笔记、范文归纳，标出处页码）。
type WritingMethod struct {
	Title     string `json:"title"`
	Content   string `json:"content"`
	Dimension string `json:"dimension,omitempty"`
	Page      int    `json:"page"`
}

// EssayMaterial 是素材；AISupplement 为 true 的是 AI 补充的（显示「AI 补充」）。
type EssayMaterial struct {
	Theme        string `json:"theme"`
	Content      string `json:"content"`
	Page         int    `json:"page"`
	AISupplement bool   `json:"ai_supplement,omitempty"`
}

// EssayStructure 是范文结构拆解（5.7）。
type EssayStructure struct {
	Opening   string   `json:"opening"`
	Points    []string `json:"points"`
	Elevation string   `json:"elevation"`
	Ending    string   `json:"ending"`
}

// ModelEssay 是范文：正文必须是原文逐字摘录。
type ModelEssay struct {
	Title     string         `json:"title"`
	Topic     string         `json:"topic,omitempty"`
	Content   string         `json:"content"`
	Page      int            `json:"page"`
	Structure EssayStructure `json:"structure"`
}

type EssayOut struct {
	Topics    []EssayTopic    `json:"topics"`
	Rubric    *EssayRubric    `json:"rubric,omitempty"`
	Methods   []WritingMethod `json:"methods"`
	Materials []EssayMaterial `json:"materials"`
	Models    []ModelEssay    `json:"model_essays"`
}

var OrganizeEssay = &Cap[EssayIn, EssayOut]{
	Def: Def{Name: "essay_organize", Version: "v1", Tier: "strong", Temperature: 0.2, Schema: `{
		"type": "object", "required": ["topics", "methods", "materials", "model_essays"],
		"properties": {
			"topics": {"type": "array", "items": {"type": "object", "required": ["title", "page"], "properties": {
				"title": {"type": "string", "minLength": 1}, "year": {"type": "integer", "minimum": 1977, "maximum": 2100},
				"required_words": {"type": "integer", "minimum": 100, "maximum": 5000}, "page": {"type": "integer", "minimum": 1}}}},
			"rubric": {"type": "object", "required": ["name", "full_score", "dimensions", "page"], "properties": {
				"name": {"type": "string", "minLength": 1}, "full_score": {"type": "number", "minimum": 1, "maximum": 300},
				"page": {"type": "integer", "minimum": 1},
				"dimensions": {"type": "array", "minItems": 1, "maxItems": 10, "items": {"type": "object", "required": ["name", "description", "score", "bands"], "properties": {
					"name": {"type": "string", "minLength": 1}, "description": {"type": "string"}, "score": {"type": "number", "minimum": 0},
					"bands": {"type": "array", "items": {"type": "object", "required": ["range", "description"], "properties": {"range": {"type": "string"}, "description": {"type": "string"}}}}}}}}},
			"methods": {"type": "array", "items": {"type": "object", "required": ["title", "content", "page"], "properties": {
				"title": {"type": "string", "minLength": 1}, "content": {"type": "string", "minLength": 1}, "dimension": {"type": "string"}, "page": {"type": "integer", "minimum": 1}}}},
			"materials": {"type": "array", "items": {"type": "object", "required": ["theme", "content", "page"], "properties": {
				"theme": {"type": "string", "minLength": 1}, "content": {"type": "string", "minLength": 1}, "page": {"type": "integer", "minimum": 0}, "ai_supplement": {"type": "boolean"}}}},
			"model_essays": {"type": "array", "items": {"type": "object", "required": ["title", "content", "page", "structure"], "properties": {
				"title": {"type": "string", "minLength": 1}, "topic": {"type": "string"}, "content": {"type": "string", "minLength": 1}, "page": {"type": "integer", "minimum": 1},
				"structure": {"type": "object", "required": ["opening", "points", "elevation", "ending"], "properties": {
					"opening": {"type": "string"}, "points": {"type": "array", "items": {"type": "string"}}, "elevation": {"type": "string"}, "ending": {"type": "string"}}}}}}
		}}`},
	Check: checkEssay,
	Mock:  mockEssay,
}

// checkEssay：题目与范文正文必须能在所在页逐字找到；评分细则各维度分值合计等于满分；非 AI 补充的素材必须来自原文。
func checkEssay(in EssayIn, out *EssayOut) error {
	pages := map[int]string{}
	for _, p := range in.Pages {
		pages[p.No] = p.Text
	}
	for i, t := range out.Topics {
		if !ContainsVerbatim(pages[t.Page], t.Title) {
			return fmt.Errorf("第 %d 个作文题在第 %d 页找不到", i+1, t.Page)
		}
	}
	for i, m := range out.Models {
		if !ContainsVerbatim(pages[m.Page], m.Content) {
			return fmt.Errorf("第 %d 篇范文正文在第 %d 页找不到", i+1, m.Page)
		}
	}
	for i, m := range out.Materials {
		if !m.AISupplement && Overlap(m.Content, pages[m.Page]) < 0.8 {
			return fmt.Errorf("第 %d 条素材不在原文里，应标为 AI 补充", i+1)
		}
	}
	if r := out.Rubric; r != nil {
		sum := 0.0
		for _, d := range r.Dimensions {
			sum += d.Score
		}
		if sum-r.FullScore > 0.01 || r.FullScore-sum > 0.01 {
			return fmt.Errorf("评分维度合计 %.1f 不等于满分 %.1f", sum, r.FullScore)
		}
	}
	return nil
}

var (
	reTopic     = regexp.MustCompile(`^\s*(?:((?:19|20)\d{2})\s*年)?.*?(?:作文题|作文题目|题目)[:：]\s*(.+)$`)
	reWords     = regexp.MustCompile(`不少于\s*(\d{3,4})\s*字|(\d{3,4})\s*字(?:左右|以上)`)
	reDimension = regexp.MustCompile(`^\s*([^（(：:\s]{2,10})\s*[（(]\s*(\d+(?:\.\d+)?)\s*分\s*[)）]\s*[:：]?\s*(.*)$`)
	reBand      = regexp.MustCompile(`^\s*[-·•]\s*(\d+\s*[-—~～]\s*\d+\s*分)\s*[:：]\s*(.+)$`)
	reMethod    = regexp.MustCompile(`^\s*(?:方法|技巧)\s*[:：]\s*([^，。,]{2,30})[，。,]\s*(.+)$`)
	reMaterial  = regexp.MustCompile(`^\s*素材\s*[【\[]\s*([^】\]]{1,20})\s*[】\]]\s*[:：]?\s*(.+)$`)
	reModel     = regexp.MustCompile(`^\s*范文\s*[《<]\s*(.+?)\s*[》>]\s*$`)
)

// mockEssay 按行规则整理（开发期与测试用）：
//
//	「2024 年作文题：……（不少于 800 字）」→ 作文题
//	「评分细则」之后「立意（30 分）：说明」→ 维度，「- 25-30 分：……」→ 分档
//	「方法：标题，内容」→ 写作方法；「素材【主题】内容」→ 素材；「范文《标题》」之后的段落 → 范文
func mockEssay(in EssayIn) (EssayOut, error) {
	var out EssayOut
	var cur *ModelEssay
	var body []string
	flushModel := func() {
		if cur != nil && len(body) > 0 {
			cur.Content = strings.Join(body, "\n")
			cur.Structure = EssayStructure{Opening: body[0], Ending: body[len(body)-1]}
			if len(body) > 2 {
				cur.Structure.Points = body[1 : len(body)-1]
			}
			out.Models = append(out.Models, *cur)
		}
		cur, body = nil, nil
	}
	for _, p := range in.Pages {
		for _, line := range strings.Split(p.Text, "\n") {
			t := strings.TrimSpace(line)
			if t == "" {
				continue
			}
			if m := reModel.FindStringSubmatch(t); m != nil {
				flushModel()
				cur = &ModelEssay{Title: m[1], Page: p.No}
				continue
			}
			if m := reTopic.FindStringSubmatch(t); m != nil && !strings.HasPrefix(t, "范文") {
				flushModel()
				tp := EssayTopic{Title: strings.TrimSpace(m[2]), Page: p.No}
				if m[1] != "" {
					y, _ := strconv.Atoi(m[1])
					tp.Year = &y
				}
				if w := reWords.FindStringSubmatch(t); w != nil {
					n, _ := strconv.Atoi(w[1] + w[2])
					tp.RequiredWords = &n
				}
				out.Topics = append(out.Topics, tp)
				continue
			}
			if strings.Contains(t, "评分细则") || strings.Contains(t, "评分标准") {
				flushModel()
				out.Rubric = &EssayRubric{Name: t, Page: p.No}
				continue
			}
			if m := reDimension.FindStringSubmatch(t); m != nil && out.Rubric != nil && cur == nil {
				s, _ := strconv.ParseFloat(m[2], 64)
				out.Rubric.Dimensions = append(out.Rubric.Dimensions, RubricDimension{Name: m[1], Score: s, Description: m[3], Bands: []RubricBand{}})
				out.Rubric.FullScore += s
				continue
			}
			if m := reBand.FindStringSubmatch(t); m != nil && out.Rubric != nil && len(out.Rubric.Dimensions) > 0 {
				d := &out.Rubric.Dimensions[len(out.Rubric.Dimensions)-1]
				d.Bands = append(d.Bands, RubricBand{Range: m[1], Description: m[2]})
				continue
			}
			if m := reMethod.FindStringSubmatch(t); m != nil {
				flushModel()
				out.Methods = append(out.Methods, WritingMethod{Title: m[1], Content: m[2], Page: p.No})
				continue
			}
			if m := reMaterial.FindStringSubmatch(t); m != nil {
				flushModel()
				out.Materials = append(out.Materials, EssayMaterial{Theme: m[1], Content: m[2], Page: p.No})
				continue
			}
			if cur != nil {
				body = append(body, t)
			}
		}
		flushModel()
	}
	if out.Rubric != nil && len(out.Rubric.Dimensions) == 0 {
		out.Rubric = nil
	}
	if out.Topics == nil {
		out.Topics = []EssayTopic{}
	}
	if out.Methods == nil {
		out.Methods = []WritingMethod{}
	}
	if out.Materials == nil {
		out.Materials = []EssayMaterial{}
	}
	if out.Models == nil {
		out.Models = []ModelEssay{}
	}
	return out, nil
}
