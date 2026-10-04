package importer

import (
	"strings"
	"testing"

	"peetraining-server/internal/ai"
)

func TestSplitExamPaper(t *testing.T) {
	pages := []Page{
		{No: 1, Text: `2024 年某某大学 中国语言文学基础 考研真题
一、名词解释（每题 5 分，共 20 分）
1. 意境
2、典型
二、简答题（每小题 15 分）
3. 简述唐传奇的艺术成就。
(1) 情节
(2) 人物`},
		{No: 2, Text: `三、论述题
4. 论述鲁迅小说的
启蒙意义。
参考答案
1. 意境是情景交融的艺术境界。
3. 情节曲折，人物鲜明。`},
		{No: 3, Text: "这一页接着上一页的答案继续写。"},
		{No: 4, Text: "短"},
		{No: 5, Text: `2023年某某大学考研试题
一、单项选择题（每题 2 分）
1. 下列属于唐传奇的是
A. 莺莺传
B. 搜神记`},
	}
	got := Split(pages)
	want := []ai.Block{
		{ID: 1, Page: 1, QType: "term", Score: 5, Year: 2024, No: "1", Text: "1. 意境"},
		{ID: 2, Page: 1, QType: "term", Score: 5, Year: 2024, No: "2", Text: "2、典型"},
		{ID: 3, Page: 1, QType: "short_answer", Score: 15, Year: 2024, No: "3", Text: "3. 简述唐传奇的艺术成就。\n(1) 情节\n(2) 人物"},
		{ID: 4, Page: 2, QType: "discussion", Year: 2024, No: "4", Text: "4. 论述鲁迅小说的\n启蒙意义。"},
		{ID: 5, Page: 2, QType: "discussion", Year: 2024, No: "1", Answer: true, Text: "1. 意境是情景交融的艺术境界。"},
		// 题目跨页时续到下一页
		{ID: 6, Page: 2, QType: "discussion", Year: 2024, No: "3", Answer: true, Text: "3. 情节曲折，人物鲜明。\n这一页接着上一页的答案继续写。\n短"},
		{ID: 7, Page: 5, QType: "single_choice", Score: 2, Year: 2023, No: "1", Text: "1. 下列属于唐传奇的是\nA. 莺莺传\nB. 搜神记"},
	}
	if len(got) != len(want) {
		t.Fatalf("块数 %d：%+v", len(got), got)
	}
	for i := range want {
		g := got[i]
		g.Section = ""
		if g != want[i] {
			t.Errorf("第 %d 块：\n got %+v\nwant %+v", i+1, g, want[i])
		}
	}
	if !strings.HasPrefix(got[0].Section, "一、名词解释") {
		t.Errorf("section：%q", got[0].Section)
	}
}

func TestSplitWholePages(t *testing.T) {
	got := Split([]Page{
		{No: 1, Text: "这一页是一大段没有题号的讲义文字，规则切不开，需要交给模型整体处理。"},
		{No: 2, Text: "短"},
		{No: 3, Text: "第三页同样没有题号，但是文字足够长，也要整页交给模型。"},
	})
	if len(got) != 2 || !got[0].Whole || got[0].Page != 1 || got[1].Page != 3 || got[1].ID != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestSplitParenStyle(t *testing.T) {
	got := Split([]Page{{No: 1, Text: "（1）什么是意境\n答案：情景交融\n（2）什么是典型"}})
	if len(got) != 2 || got[0].No != "1" || got[1].Text != "（2）什么是典型" {
		t.Fatalf("%+v", got)
	}
}

func TestChunk(t *testing.T) {
	bs := []ai.Block{{Text: strings.Repeat("a", 30)}, {Text: strings.Repeat("b", 30)}, {Text: strings.Repeat("c", 50)}, {Text: strings.Repeat("d", 200)}}
	got := Chunk(bs, 100)
	if len(got) != 3 || len(got[0]) != 2 || len(got[1]) != 1 || len(got[2]) != 1 {
		t.Fatalf("%d", len(got))
	}
	if Chunk(nil, 100) != nil {
		t.Fatal("空")
	}
}
