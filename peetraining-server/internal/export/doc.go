// Package export 是导出题库（6.4，T24）：按专业课把题目与参考答案、知识点卡片、错题和我的作答、AI 变式题整理成 PDF 或 Word，
// Worker 生成后放 OSS，给 1 小时有效的下载链接，24 小时后删除。只包含用户自己的内容（每条查询都带归属条件）。
package export

import (
	"strings"
	"unicode"
)

// BlockKind 是文档里的一块内容。
type BlockKind int

const (
	Heading BlockKind = iota + 1 // 一级标题（每部分）
	Sub                          // 二级标题（题型、板块）
	Para                         // 正文
	Note                         // 辅助说明（出处、标注）
)

// Block 是一段内容。
type Block struct {
	Kind BlockKind
	Text string
}

// Document 是要导出的文档。
type Document struct {
	Title    string
	Subtitle string
	Blocks   []Block
}

func (d *Document) add(k BlockKind, text string) {
	if t := clean(text); t != "" {
		d.Blocks = append(d.Blocks, Block{Kind: k, Text: t})
	}
}

// clean 去掉控制字符与字体里没有的字符（表情等补充平面字符），统一换行。
func clean(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r > 0xFFFF, unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// charsPerPage 是 A4 小四号字一页大约的字数（预计页数用）。
const charsPerPage = 900

// Pages 估计页数：按字数，每块至少算一行。
func (d *Document) Pages() int {
	n := 0
	for _, b := range d.Blocks {
		c := len([]rune(b.Text))
		n += max(c, 40)
		if b.Kind == Heading {
			n += 200
		}
	}
	return max(1, (n+charsPerPage-1)/charsPerPage)
}
