package ai

import (
	"fmt"
	"sort"
	"strings"
)

// StyleIn 是出题风格标签的输入（3.8「出题风格」，标「AI 统计」）：按年份给题型与题干。
type StyleIn struct {
	Subject   string      `json:"subject"`
	Questions []StyleItem `json:"questions"`
}

type StyleItem struct {
	Year  int    `json:"year"`
	QType string `json:"qtype"`
	Stem  string `json:"stem"`
}

type StyleOut struct {
	Tags []string `json:"tags"`
}

var ExamStyle = &Cap[StyleIn, StyleOut]{
	Def: Def{Name: "exam_style", Version: "v1", Tier: "cheap", Temperature: 0.2, Schema: `{
		"type": "object", "required": ["tags"],
		"properties": {"tags": {"type": "array", "minItems": 2, "maxItems": 4, "items": {"type": "string", "minLength": 2, "maxLength": 10}}}}`},
	Check: func(_ StyleIn, out *StyleOut) error {
		seen := map[string]bool{}
		for _, t := range out.Tags {
			if seen[t] {
				return fmt.Errorf("标签「%s」重复", t)
			}
			seen[t] = true
		}
		return nil
	},
	Mock: mockStyle,
}

// mockStyle 按题型分布给标签（开发与测试用）。
func mockStyle(in StyleIn) (StyleOut, error) {
	count := map[string]int{}
	for _, q := range in.Questions {
		count[q.QType]++
	}
	type kv struct {
		k string
		n int
	}
	var kvs []kv
	for k, n := range count {
		kvs = append(kvs, kv{k, n})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].n > kvs[j].n || (kvs[i].n == kvs[j].n && kvs[i].k < kvs[j].k) })
	names := map[string]string{"term": "名词解释为主", "short_answer": "重简答", "discussion": "论述跨板块", "essay": "重写作", "single_choice": "考基础记忆"}
	tags := []string{"偏原文表述"}
	for _, x := range kvs {
		if n, ok := names[x.k]; ok && len(tags) < 3 {
			tags = append(tags, n)
		}
	}
	if len(tags) < 2 {
		tags = append(tags, "常考作品分析")
	}
	return StyleOut{Tags: tags}, nil
}

// RelateIn 是知识关联的输入（3.9）：同一门课的知识点，AI 找出易混的对比关系；同章并列、同题出现由规则直接生成。
type RelateIn struct {
	Subject string      `json:"subject"`
	Points  []RelatePKP `json:"points"`
}

type RelatePKP struct {
	ID   int      `json:"id"`
	Name string   `json:"name"`
	Path []string `json:"path"`
}

type RelatePair struct {
	A int `json:"a"`
	B int `json:"b"`
}

type RelateOut struct {
	Contrasts []RelatePair `json:"contrasts"`
}

var Relate = &Cap[RelateIn, RelateOut]{
	Def: Def{Name: "kp_relate", Version: "v1", Tier: "cheap", Temperature: 0.1, Schema: `{
		"type": "object", "required": ["contrasts"],
		"properties": {"contrasts": {"type": "array", "maxItems": 60, "items": {"type": "object", "required": ["a", "b"], "properties": {"a": {"type": "integer"}, "b": {"type": "integer"}}}}}}`},
	Check: func(in RelateIn, out *RelateOut) error {
		ids := map[int]bool{}
		for _, p := range in.Points {
			ids[p.ID] = true
		}
		for _, c := range out.Contrasts {
			if !ids[c.A] || !ids[c.B] || c.A == c.B {
				return fmt.Errorf("关联引用了不存在的知识点 %d-%d", c.A, c.B)
			}
		}
		return nil
	},
	// mock：名称有共同字且不在同一章的两个知识点视为易混（如「意象」「意境」）。
	Mock: func(in RelateIn) (RelateOut, error) {
		var out RelateOut
		for i := 0; i < len(in.Points); i++ {
			for j := i + 1; j < len(in.Points); j++ {
				a, b := in.Points[i], in.Points[j]
				if sharesRune(a.Name, b.Name) && strings.Join(a.Path, "/") != strings.Join(b.Path, "/") {
					out.Contrasts = append(out.Contrasts, RelatePair{A: a.ID, B: b.ID})
				}
			}
		}
		return out, nil
	},
}

func sharesRune(a, b string) bool {
	for _, r := range a {
		if strings.ContainsRune(b, r) {
			return true
		}
	}
	return false
}
