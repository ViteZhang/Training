// Package ocr 把图片或 PDF 页识别成文字（印刷体、手写）。选哪家服务在 T09 定（ADR 0007）。
package ocr

import (
	"context"
	"strings"
)

// Image 是一张待识别的图片或 PDF 的一页，二选一：OSS 对象键，或原始字节。
type Image struct {
	ObjectKey string
	Data      []byte
	// Handwriting 为 true 时按手写识别（4.5、5.4）。
	Handwriting bool
}

// Span 是一段低置信度文字在结果中的位置（按 rune 计）。
type Span struct {
	Start, End int
}

// Result 是一页的识别结果。
type Result struct {
	Text          string
	LowConfidence []Span
}

// Recognizer 识别文字。
type Recognizer interface {
	Recognize(ctx context.Context, img Image) (Result, error)
}

// Mock 把图片字节当作 UTF-8 文本原样返回，便于在测试里构造「识别结果」。
// 文本中用【】括起来的部分标为低置信度。
type Mock struct{}

func NewMock() Mock { return Mock{} }

func (Mock) Recognize(ctx context.Context, img Image) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var out []rune
	var spans []Span
	start := -1
	for _, r := range string(img.Data) {
		switch r {
		case '【':
			start = len(out)
		case '】':
			if start >= 0 {
				spans = append(spans, Span{Start: start, End: len(out)})
				start = -1
			}
		default:
			out = append(out, r)
		}
	}
	return Result{Text: strings.TrimSpace(string(out)), LowConfidence: spans}, nil
}

// PDFPage 是 PDF 一页的识别结果。
type PDFPage struct {
	Text          string
	LowConfidence []Span
	// Scanned 为 true 表示这一页没有文字层，是逐页 OCR 得到的（扫描版 PDF 受功能开关 scanned_pdf 控制）。
	Scanned bool
}

// PDFParser 解析 PDF：有文字层的页直接取文字，没有的逐页 OCR。选哪家服务见 ADR 0007。
type PDFParser interface {
	ParsePDF(ctx context.Context, data []byte) ([]PDFPage, error)
}

// MockScannedMark 出现在 mock PDF 某页开头时，这一页按扫描页处理。
const MockScannedMark = "[扫描]"

// ParsePDF 的 mock 把字节当作 UTF-8 文本，按换页符 \f 分页；以 MockScannedMark 开头的页记为扫描页，
// 其余规则同 Recognize（【】内为低置信度）。
func (m Mock) ParsePDF(ctx context.Context, data []byte) ([]PDFPage, error) {
	var pages []PDFPage
	for _, raw := range strings.Split(string(data), "\f") {
		scanned := strings.HasPrefix(raw, MockScannedMark)
		r, err := m.Recognize(ctx, Image{Data: []byte(strings.TrimPrefix(raw, MockScannedMark))})
		if err != nil {
			return nil, err
		}
		pages = append(pages, PDFPage{Text: r.Text, LowConfidence: r.LowConfidence, Scanned: scanned})
	}
	return pages, nil
}
