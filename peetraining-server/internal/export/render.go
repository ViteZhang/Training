package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/signintech/gopdf"
)

// A4 版面（单位 pt）。
const (
	pageW, pageH = 595.28, 841.89
	margin       = 56.0
	contentW     = pageW - 2*margin
)

var styles = map[BlockKind]struct {
	size, lead, gap float64
	gray            bool
}{
	Heading: {18, 26, 14, false},
	Sub:     {14, 20, 8, false},
	Para:    {11, 17, 4, false},
	Note:    {9, 14, 4, true},
}

// Fonts 是导出 PDF 用的字体：中文字体只含中日韩字形，字母、数字与西文标点用西文字体（逐段切换）。
type Fonts struct {
	CJK, Latin string
}

// latin 判断字符用西文字体排：ASCII 与常用西文符号。
func latin(r rune) bool { return r < 0x2000 }

// pdfWriter 按字体逐段测宽、折行、输出。
type pdfWriter struct {
	pdf   *gopdf.GoPdf
	size  float64
	width map[[2]any]float64
}

func (w *pdfWriter) font(r rune) string {
	if latin(r) {
		return "latin"
	}
	return "cjk"
}

func (w *pdfWriter) runeWidth(r rune) (float64, error) {
	k := [2]any{r, w.size}
	if v, ok := w.width[k]; ok {
		return v, nil
	}
	if err := w.pdf.SetFont(w.font(r), "", w.size); err != nil {
		return 0, err
	}
	v, err := w.pdf.MeasureTextWidth(string(r))
	if err != nil {
		return 0, err
	}
	w.width[k] = v
	return v, nil
}

// wrap 按版心宽度逐字折行（中文没有空格，按字断行）。
func (w *pdfWriter) wrap(text string) ([]string, error) {
	var lines []string
	var cur []rune
	cw := 0.0
	for _, r := range text {
		rw, err := w.runeWidth(r)
		if err != nil {
			return nil, err
		}
		if cw+rw > contentW && len(cur) > 0 {
			lines = append(lines, string(cur))
			cur, cw = cur[:0], 0
		}
		cur = append(cur, r)
		cw += rw
	}
	if len(cur) > 0 {
		lines = append(lines, string(cur))
	}
	return lines, nil
}

// line 输出一行：同一字体的连续字符作为一段。
func (w *pdfWriter) line(text string, x, y float64) error {
	runes := []rune(text)
	for i := 0; i < len(runes); {
		f := w.font(runes[i])
		j := i
		seg := 0.0
		for j < len(runes) && w.font(runes[j]) == f {
			rw, err := w.runeWidth(runes[j])
			if err != nil {
				return err
			}
			seg += rw
			j++
		}
		if err := w.pdf.SetFont(f, "", w.size); err != nil {
			return err
		}
		w.pdf.SetXY(x, y)
		if err := w.pdf.Text(string(runes[i:j])); err != nil {
			return err
		}
		x += seg
		i = j
	}
	return nil
}

// RenderPDF 排版成 A4 PDF。
func RenderPDF(d Document, fonts Fonts) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}})
	if err := pdf.AddTTFFont("cjk", fonts.CJK); err != nil {
		return nil, fmt.Errorf("export: 加载中文字体 %s：%w", fonts.CJK, err)
	}
	if err := pdf.AddTTFFont("latin", fonts.Latin); err != nil {
		return nil, fmt.Errorf("export: 加载西文字体 %s：%w", fonts.Latin, err)
	}
	w := &pdfWriter{pdf: pdf, width: map[[2]any]float64{}}
	pdf.AddPage()
	y := margin
	write := func(k BlockKind, text string) error {
		st := styles[k]
		w.size = st.size
		if st.gray {
			pdf.SetTextColor(107, 103, 95)
		} else {
			pdf.SetTextColor(27, 26, 23)
		}
		y += st.gap
		for _, para := range strings.Split(text, "\n") {
			if strings.TrimSpace(para) == "" {
				continue
			}
			lines, err := w.wrap(para)
			if err != nil {
				return err
			}
			for _, ln := range lines {
				if y+st.lead > pageH-margin {
					pdf.AddPage()
					y = margin
				}
				// Text 以基线定位：往下挪一个字号。
				if err := w.line(ln, margin, y+st.size); err != nil {
					return err
				}
				y += st.lead
			}
		}
		return nil
	}
	if err := write(Heading, d.Title); err != nil {
		return nil, err
	}
	if d.Subtitle != "" {
		if err := write(Note, d.Subtitle); err != nil {
			return nil, err
		}
	}
	for _, b := range d.Blocks {
		// 新的一部分另起一页（第一部分除外）。
		if b.Kind == Heading && y > margin+120 {
			pdf.AddPage()
			y = margin
		}
		if err := write(b.Kind, b.Text); err != nil {
			return nil, err
		}
	}
	return pdf.GetBytesPdf(), nil
}

// RenderDOCX 生成 Word 文档（OOXML，标准库打包，不依赖第三方库）。
func RenderDOCX(d Document) ([]byte, error) {
	var body strings.Builder
	para := func(k BlockKind, text string) {
		st := styles[k]
		for _, line := range strings.Split(text, "\n") {
			var esc bytes.Buffer
			_ = xml.EscapeText(&esc, []byte(line))
			color := ""
			if st.gray {
				color = `<w:color w:val="6B675F"/>`
			}
			bold := ""
			if k == Heading || k == Sub {
				bold = "<w:b/>"
			}
			fmt.Fprintf(&body, `<w:p><w:pPr><w:spacing w:before="%d" w:after="60"/></w:pPr><w:r><w:rPr><w:rFonts w:eastAsia="宋体"/>%s%s<w:sz w:val="%d"/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`,
				int(st.gap*20), bold, color, int(st.size*2), esc.String())
		}
	}
	para(Heading, d.Title)
	if d.Subtitle != "" {
		para(Note, d.Subtitle)
	}
	for _, b := range d.Blocks {
		para(b.Kind, b.Text)
	}
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() +
			`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="1134" w:bottom="1134" w:left="1134" w:header="0" w:footer="0" w:gutter="0"/></w:sectPr></w:body></w:document>`,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
