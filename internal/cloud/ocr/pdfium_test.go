package ocr

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func readPDF(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// countingOCR 记录被调用的次数，确认扫描版开关关闭时不花识别费用。
type countingOCR struct{ calls int }

func (c *countingOCR) Recognize(_ context.Context, img Image) (Result, error) {
	c.calls++
	if len(img.Data) == 0 {
		return Result{}, errors.New("空图片")
	}
	return Result{Text: "识别出的文字", LowConfidence: []Span{{Start: 0, End: 2}}}, nil
}

func TestPDFiumTextLayer(t *testing.T) {
	o := &countingOCR{}
	p := NewPDFium(o)
	pages, err := p.ParsePDF(context.Background(), readPDF(t, "text.pdf"), PDFOptions{OCRScanned: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Scanned {
		t.Fatalf("应为 1 页文字页：%+v", pages)
	}
	for _, want := range []string{"一、名词解释", "1. 社会契约论：卢梭认为国家起源于人们之间的契约。", "二、简答题"} {
		if !strings.Contains(pages[0].Text, want) {
			t.Errorf("文字层缺少 %q：%q", want, pages[0].Text)
		}
	}
	if strings.Contains(pages[0].Text, "\r") {
		t.Error("换行应统一为 \\n")
	}
	if o.calls != 0 {
		t.Error("文字页不应调用 OCR")
	}
}

func TestPDFiumScanned(t *testing.T) {
	o := &countingOCR{}
	p := NewPDFium(o)
	data := readPDF(t, "scanned.pdf")
	pages, err := p.ParsePDF(context.Background(), data, PDFOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || !pages[0].Scanned || pages[0].Text != "" || o.calls != 0 {
		t.Fatalf("开关关闭时只标扫描页、不识别：%+v calls=%d", pages, o.calls)
	}
	pages, err = p.ParsePDF(context.Background(), data, PDFOptions{OCRScanned: true})
	if err != nil {
		t.Fatal(err)
	}
	if !pages[0].Scanned || pages[0].Text != "识别出的文字" || len(pages[0].LowConfidence) != 1 || o.calls != 1 {
		t.Fatalf("开关打开时扫描页送 OCR：%+v calls=%d", pages, o.calls)
	}
}

func TestPDFiumBroken(t *testing.T) {
	_, err := NewPDFium(nil).ParsePDF(context.Background(), []byte("这不是 PDF"), PDFOptions{})
	if !errors.Is(err, ErrPDFBroken) {
		t.Fatalf("损坏文件应返回 ErrPDFBroken：%v", err)
	}
}

func TestNormalizeText(t *testing.T) {
	if got := normalizeText("  第一行  \r\n第二\u00ad行\r\n\r\n"); got != "第一行\n第二行" {
		t.Errorf("%q", got)
	}
}

// 官方教材的文字 PDF 与扫描 PDF 都只渲染页面，完全不调用传统 OCR。
func TestOfficialTextbookRendersEveryPage(t *testing.T) {
	for _, name := range []string{"text.pdf", "scanned.pdf"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			pages := 0
			err = NewPDFium(nil).RenderAllPages(context.Background(), data, 100, func(total, page int, jpeg []byte) error {
				pages++
				if total < page || page != pages || len(jpeg) < 3 || jpeg[0] != 0xff || jpeg[1] != 0xd8 {
					t.Fatal("页图或顺序不正确")
				}
				return nil
			})
			if err != nil || pages < 1 {
				t.Fatal(err)
			}
		})
	}
}
