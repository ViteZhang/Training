package official

import (
	"strings"
	"testing"
)

func TestSplitReference(t *testing.T) {
	text := "第一章 文学活动\n\n文学是一种审美意识形态。\n文学活动由作品、作家、世界、读者四要素构成。\n" +
		"第二节 文学的本质\n" + strings.Repeat("意境是情景交融、虚实相生的艺术境界。", 50) + "\n" +
		"一、典型\n典型是显出特征的艺术形象。"
	segs := SplitReference(text)
	if len(segs) < 3 {
		t.Fatalf("应按章节切出至少 3 段：%d", len(segs))
	}
	if segs[0].Heading != "第一章 文学活动" || !strings.Contains(segs[0].Text, "四要素") {
		t.Errorf("第一段：%+v", segs[0])
	}
	last := segs[len(segs)-1]
	if last.Heading != "一、典型" || last.Text != "典型是显出特征的艺术形象。" {
		t.Errorf("最后一段：%+v", last)
	}
	for _, g := range segs {
		if n := len([]rune(g.Text)); n > segmentMax+segmentTarget {
			t.Errorf("段落太长：%d", n)
		}
		if g.Heading == "第二节 文学的本质" && !strings.Contains(g.Text, "意境") {
			t.Errorf("第二节的段落：%+v", g)
		}
	}
	if len(SplitReference(" \n\n ")) != 0 {
		t.Error("空文本不应切出段落")
	}
	// 「第一章」开头的长句不是标题
	if g := SplitReference("第一章讲的是文学活动的四个要素，包括作品、作家、世界和读者，它们构成了文学活动的整体结构和基本框架。"); len(g) != 1 || g[0].Heading != "" {
		t.Errorf("长句不应当成标题：%+v", g)
	}
}
