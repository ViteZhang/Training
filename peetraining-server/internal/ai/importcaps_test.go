package ai

import (
	"errors"
	"testing"
)

// 块上没有年份、分值时，模型会把可选字段输出成 null（qwen-plus 实测），应按缺省处理而不是整批判为不合格。
func TestStructureAcceptsNullOptionals(t *testing.T) {
	in := StructureIn{Blocks: []Block{{ID: 1, Page: 1, Text: "1. 现代汉民族共同语"}}}
	out, err := Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":"term","stem":"现代汉民族共同语",
		"options":null,"answer":null,"score":null,"exam_year":null,"question_no":"1","page":1,"confidence":0.9,"is_recollection":null}]}`)
	if err != nil {
		t.Fatalf("可选字段为 null 应能通过校验：%v", err)
	}
	it := out.Items[0]
	if it.ExamYear != nil || it.Score != nil || it.Answer != "" || it.Options != nil {
		t.Fatalf("null 应解析为缺省：%+v", it)
	}
	// 必填字段仍然要有：缺题型照样不合格（唯一一条不合格，整批判不合格）。
	_, err = Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":null,"stem":"现代汉民族共同语","page":1,"confidence":0.9}]}`)
	if !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("题目缺题型应不合格：%v", err)
	}
	// 范围校验不受影响。
	_, err = Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":"term","stem":"现代汉民族共同语","exam_year":1900,"page":1,"confidence":0.9}]}`)
	if !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("年份超出范围应不合格：%v", err)
	}
}

// 不开思考的 qwen3 模型会把路径照抄成 [["板块", "章节"]]（实测 qwen3.8-flash），整理成一层后应能通过校验。
func TestKPPathFlattened(t *testing.T) {
	in := KPIn{Pages: []KPPage{{No: 1, Text: "一、神话\n神话：远古人民表现对自然及社会现象的认识。"}}}
	out, err := ExtractKPs.validate(in, `{"points":[{"name":"神话","original_text":"神话：远古人民表现对自然及社会现象的认识。","page":1,
		"path":[["第一章 先秦文学","一、神话"]],"rubric_points":[{"content":"远古人民","keywords":["远古人民"]}]}]}`)
	if err != nil {
		t.Fatalf("嵌套路径应整理后通过：%v", err)
	}
	if p := out.Points[0].Path; len(p) != 2 || p[0] != "第一章 先秦文学" || p[1] != "一、神话" {
		t.Fatalf("路径：%v", p)
	}
}

// 一批里个别条目不合格时只剔除这几条，其余照常保留（川大真题合集曾因一条整批 108 道作废）；
// 题型标题里的要求并进题干算来自原文。
func TestStructureDropsBadItemsOnly(t *testing.T) {
	sec := "二、简答（每題 10 分，共 40 分）说出以下各语的出处，并简要分析其含义："
	in := StructureIn{Blocks: []Block{
		{ID: 1, Page: 1, Section: sec, Text: "1.興於詩，立於禮，成於樂."},
		{ID: 2, Page: 1, Section: sec, Text: "2.窮則變，變則通，通則久。"},
		{ID: 3, Page: 1, Text: "3.坐忘"},
	}}
	out, err := Structure.validate(in, `{"items":[
		{"block":1,"kind":"question","qtype":"short_answer","stem":"说出以下各语的出处，并简要分析其含义：興於詩，立於禮，成於樂.","page":1,"confidence":0.9},
		{"block":2,"kind":"question","qtype":"short_answer","stem":"论述宋明理学的发展脉络","page":1,"confidence":0.9},
		{"block":3,"kind":"question","qtype":"term","stem":"坐忘","page":1,"confidence":0.9},
		{"block":9,"kind":"question","qtype":"term","stem":"心斋","page":1,"confidence":0.9}]}`)
	if err != nil {
		t.Fatalf("个别条目不合格不应整批失败：%v", err)
	}
	if len(out.Items) != 2 || out.Items[0].Block != 1 || out.Items[1].Block != 3 {
		t.Fatalf("保留的条目：%+v", out.Items)
	}
	if len(out.Dropped) != 1 || out.Dropped[0] != 2 {
		t.Fatalf("剔除的块：%v", out.Dropped)
	}
}
