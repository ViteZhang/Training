// Package extract 把各种格式的资料取成带页码的文字（导入流水线第 3 步，dev-spec 第六节「格式」表）。
//
// 只做格式解析，不访问数据库与云服务；PDF 与图片的识别在 internal/cloud/ocr，由 material 包组装。
package extract

import (
	"errors"
	"strings"
)

// Table 是识别出的表格，第一行作表头。
type Table struct {
	Header []string   `json:"header"`
	Rows   [][]string `json:"rows"`
}

// Span 是一段文字在页内的位置（按 rune 计）。
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Page 是一页结果。页码从 1 开始，后面所有「出处页码」都靠它。
type Page struct {
	No            int
	Text          string
	Tables        []Table
	LowConfidence []Span
}

// Result 是一份资料的取文本结果。
type Result struct {
	Pages []Page
	// BilledPages 是计费页数（PRD 11.12）：Word 与粘贴每 1500 字一页，Excel 每 50 行一页，PDF 按实际页数，图片每张一页。
	BilledPages int
}

// UserError 是用户能看懂、重试也不会好的失败（格式不支持、文件损坏），原因直接显示在 1.6b。
type UserError struct{ Msg string }

func (e *UserError) Error() string { return e.Msg }

func userErr(msg string) error { return &UserError{Msg: msg} }

// AsUserError 取出 UserError。
func AsUserError(err error) (*UserError, bool) {
	var ue *UserError
	ok := errors.As(err, &ue)
	return ue, ok
}

// 旧版 Office（.doc / .xls）与加密的 Office 文件都是 OLE 复合文档。
var oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

func isOLE(data []byte) bool {
	return len(data) >= len(oleMagic) && string(data[:len(oleMagic)]) == string(oleMagic)
}

// SplitPages 按字数分页，尽量在换行处断开（在每页后四分之一里找换行）。
func SplitPages(text string, perPage int) []string {
	runes := []rune(text)
	var pages []string
	for len(runes) > 0 {
		if len(runes) <= perPage {
			pages = append(pages, string(runes))
			break
		}
		cut := perPage
		for i := perPage; i > perPage*3/4; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		pages = append(pages, string(runes[:cut]))
		runes = runes[cut:]
	}
	return pages
}

// block 是分页前的一段内容：一个段落或一张表格。
type block struct {
	text  string
	table *Table
}

// paginate 把段落与表格按字数排成页：一段放不下就换页，单段超过一页再按字数切开；表格记在它开始的那一页。
func paginate(blocks []block, perPage int) []Page {
	var pages []Page
	var cur strings.Builder
	var tables []Table
	n := 0
	flush := func() {
		if n == 0 && len(tables) == 0 {
			return
		}
		pages = append(pages, Page{No: len(pages) + 1, Text: strings.TrimRight(cur.String(), "\n"), Tables: tables})
		cur.Reset()
		tables, n = nil, 0
	}
	for _, b := range blocks {
		size := len([]rune(b.text)) + 1
		if n > 0 && n+size > perPage {
			flush()
		}
		if b.table != nil {
			tables = append(tables, *b.table)
		}
		if size > perPage {
			parts := SplitPages(b.text, perPage)
			for i, p := range parts {
				cur.WriteString(p)
				cur.WriteString("\n")
				n += len([]rune(p)) + 1
				if i < len(parts)-1 {
					flush()
				}
			}
			continue
		}
		cur.WriteString(b.text)
		cur.WriteString("\n")
		n += size
	}
	flush()
	return pages
}

// Text 处理粘贴的文字：每 1500 字一页。
func Text(text string, charsPerPage int) Result {
	text = strings.TrimSpace(text)
	parts := SplitPages(text, charsPerPage)
	pages := make([]Page, len(parts))
	for i, p := range parts {
		pages[i] = Page{No: i + 1, Text: p}
	}
	return Result{Pages: pages, BilledPages: len(pages)}
}
