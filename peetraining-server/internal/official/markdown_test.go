package official

import "testing"

func TestMarkdownText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"## 一、名词解释（每题 6 分） ##", "一、名词解释（每题 6 分）"},
		{"**1.** 复句", "1. 复句"},
		{"> 第一组：请选择*文艺学*专业的考生作答", "第一组：请选择文艺学专业的考生作答"},
		{"- [x] 简述`得意忘言`说", "简述得意忘言说"},
		{"说明[类书](https://a.cn/x)的意义![图](a.png)", "说明类书的意义"},
		{"| 题号 | 题干 |\n|---|:---:|\n| 1 | 复句 |", "题号  题干\n1  复句"},
		{"第一行<br>第二行", "第一行\n第二行"},
		{"计算 2*3*4 与 \\*号", "计算 2*3*4 与 *号"},
		{"上文\n\n***\n\n```\n下文\n```", "上文\n\n\n下文"},
	}
	for _, c := range cases {
		if got := markdownText(c.in); got != c.want {
			t.Errorf("markdownText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
