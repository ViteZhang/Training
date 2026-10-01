package extract

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

// Docx 解析 Word（.docx）：保留段落、自动编号（如「一、」「1.」）与表格；每 charsPerPage 字一页、计 1 页（PRD 11.12）。
// 自动编号要保留，因为切题靠题号与「一、名词解释」这类题型标题（dev-spec 第六节第 5 步）。
func Docx(data []byte, charsPerPage int) (Result, error) {
	if isOLE(data) {
		return Result{}, userErr("这是旧版 Word（.doc）或加密的文档，暂不支持。请用 Word 或 WPS 另存为 .docx（不设密码）后再上传")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return Result{}, userErr("文件已损坏或不是 Word 文档，请检查后重新上传")
	}
	doc, err := readZip(zr, "word/document.xml")
	if err != nil {
		return Result{}, userErr("文件已损坏或不是 Word 文档，请检查后重新上传")
	}
	num := newNumbering(nil)
	if nb, err := readZip(zr, "word/numbering.xml"); err == nil {
		num = newNumbering(nb)
	}
	blocks, err := parseDocument(doc, num)
	if err != nil {
		return Result{}, userErr("文件内容无法读取，请用 Word 或 WPS 打开后重新保存为 .docx 再上传")
	}
	pages := paginate(blocks, charsPerPage)
	if len(pages) == 0 {
		return Result{}, userErr("文档里没有读到文字。如果是图片做成的文档，请改用拍照导入")
	}
	return Result{Pages: pages, BilledPages: len(pages)}, nil
}

func readZip(zr *zip.Reader, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// 单个 XML 最多读 64 MB，防止压缩炸弹。
	return io.ReadAll(io.LimitReader(f, 64<<20))
}

const wNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// parseDocument 顺序读 document.xml。表格里的段落合进单元格；嵌套表格的文字并入外层单元格。
func parseDocument(doc []byte, num *numbering) ([]block, error) {
	d := xml.NewDecoder(bytes.NewReader(doc))
	var (
		blocks     []block
		para       strings.Builder
		inText     bool
		numID, lvl string
		tblDepth   int
		table      *Table
		row        []string
		cell       strings.Builder
		inDel      int
	)
	endPara := func() {
		text := strings.TrimRight(para.String(), " \t")
		if numID != "" {
			if prefix := num.next(numID, lvl); prefix != "" {
				text = prefix + text
			}
		}
		para.Reset()
		numID, lvl = "", ""
		if tblDepth > 0 {
			if cell.Len() > 0 {
				cell.WriteString("\n")
			}
			cell.WriteString(text)
			return
		}
		if strings.TrimSpace(text) != "" {
			blocks = append(blocks, block{text: text})
		}
	}
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wNS {
				continue
			}
			switch t.Name.Local {
			case "del":
				inDel++ // 修订模式下删除的文字不要
			case "t":
				inText = inDel == 0
			case "tab":
				para.WriteString("\t")
			case "br", "cr":
				if attr(t, "type") != "page" {
					para.WriteString("\n")
				}
			case "numId":
				numID = attr(t, "val")
			case "ilvl":
				lvl = attr(t, "val")
			case "tbl":
				tblDepth++
				if tblDepth == 1 {
					table = &Table{}
				}
			case "tr":
				if tblDepth == 1 {
					row = nil
				}
			case "tc":
				if tblDepth == 1 {
					cell.Reset()
				}
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		case xml.EndElement:
			if t.Name.Space != wNS {
				continue
			}
			switch t.Name.Local {
			case "del":
				inDel--
			case "t":
				inText = false
			case "p":
				endPara()
			case "tc":
				if tblDepth == 1 {
					row = append(row, strings.TrimSpace(cell.String()))
				}
			case "tr":
				if tblDepth == 1 && table != nil {
					if table.Header == nil {
						table.Header = row
					} else {
						table.Rows = append(table.Rows, row)
					}
				}
			case "tbl":
				tblDepth--
				if tblDepth == 0 && table != nil {
					blocks = append(blocks, block{text: renderTable(*table), table: table})
					table = nil
				}
			}
		}
	}
	return blocks, nil
}

// renderTable 把表格写成文字放进页面，模型与「原文查看」都能读到。
func renderTable(t Table) string {
	var b strings.Builder
	b.WriteString(strings.Join(t.Header, " | "))
	for _, r := range t.Rows {
		b.WriteString("\n")
		b.WriteString(strings.Join(r, " | "))
	}
	return b.String()
}

// numbering 还原 Word 自动编号。只处理常见格式；认不出的格式按阿拉伯数字。
type numbering struct {
	numToAbs map[string]string
	levels   map[string]map[string]numLevel // abstractNumId → ilvl → 格式
	counters map[string][]int               // numId → 各级计数
}

type numLevel struct {
	fmt, text string
	start     int
}

func newNumbering(data []byte) *numbering {
	n := &numbering{numToAbs: map[string]string{}, levels: map[string]map[string]numLevel{}, counters: map[string][]int{}}
	if data == nil {
		return n
	}
	var doc struct {
		Abstract []struct {
			ID  string `xml:"abstractNumId,attr"`
			Lvl []struct {
				Ilvl  string `xml:"ilvl,attr"`
				Start struct {
					Val string `xml:"val,attr"`
				} `xml:"start"`
				NumFmt struct {
					Val string `xml:"val,attr"`
				} `xml:"numFmt"`
				LvlText struct {
					Val string `xml:"val,attr"`
				} `xml:"lvlText"`
			} `xml:"lvl"`
		} `xml:"abstractNum"`
		Num []struct {
			ID  string `xml:"numId,attr"`
			Abs struct {
				Val string `xml:"val,attr"`
			} `xml:"abstractNumId"`
		} `xml:"num"`
	}
	if xml.Unmarshal(data, &doc) != nil {
		return n
	}
	for _, a := range doc.Abstract {
		m := map[string]numLevel{}
		for _, l := range a.Lvl {
			start, err := strconv.Atoi(l.Start.Val)
			if err != nil {
				start = 1
			}
			m[l.Ilvl] = numLevel{fmt: l.NumFmt.Val, text: l.LvlText.Val, start: start}
		}
		n.levels[a.ID] = m
	}
	for _, x := range doc.Num {
		n.numToAbs[x.ID] = x.Abs.Val
	}
	return n
}

// next 返回本段的编号前缀并推进计数；下级计数在上级推进时归零。
func (n *numbering) next(numID, ilvl string) string {
	if numID == "0" {
		return ""
	}
	lv, err := strconv.Atoi(ilvl)
	if err != nil || lv < 0 || lv > 8 {
		lv = 0
	}
	levels := n.levels[n.numToAbs[numID]]
	c := n.counters[numID]
	if c == nil {
		c = make([]int, 9)
		for i := range c {
			c[i] = levelOf(levels, i).start - 1
		}
	}
	c[lv]++
	for i := lv + 1; i < 9; i++ {
		c[i] = levelOf(levels, i).start - 1
	}
	n.counters[numID] = c

	l := levelOf(levels, lv)
	if l.fmt == "bullet" || l.fmt == "none" {
		return ""
	}
	text := l.text
	if text == "" {
		text = "%" + strconv.Itoa(lv+1) + "."
	}
	for i := 8; i >= 0; i-- {
		text = strings.ReplaceAll(text, "%"+strconv.Itoa(i+1), formatNum(c[i], levelOf(levels, i).fmt))
	}
	return text + " "
}

func levelOf(levels map[string]numLevel, i int) numLevel {
	if l, ok := levels[strconv.Itoa(i)]; ok {
		return l
	}
	return numLevel{fmt: "decimal", start: 1}
}

func formatNum(v int, f string) string {
	switch f {
	case "chineseCounting", "chineseCountingThousand", "ideographTraditional", "taiwaneseCountingThousand", "chineseLegalSimplified", "ideographDigital":
		return chineseNum(v)
	case "decimalEnclosedCircle", "decimalEnclosedCircleChinese":
		if v >= 1 && v <= 20 {
			return string(rune('①' + v - 1))
		}
	case "lowerLetter":
		return letters(v, 'a')
	case "upperLetter":
		return letters(v, 'A')
	case "lowerRoman":
		return strings.ToLower(roman(v))
	case "upperRoman":
		return roman(v)
	}
	return strconv.Itoa(v)
}

func chineseNum(v int) string {
	digits := []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	switch {
	case v < 0:
		return strconv.Itoa(v)
	case v < 10:
		return digits[v]
	case v < 20:
		return "十" + strings.TrimPrefix(digits[v%10], "零")
	case v < 100:
		s := digits[v/10] + "十"
		if v%10 != 0 {
			s += digits[v%10]
		}
		return s
	}
	return strconv.Itoa(v)
}

func letters(v int, base rune) string {
	if v <= 0 {
		return strconv.Itoa(v)
	}
	var s []rune
	for v > 0 {
		v--
		s = append([]rune{base + rune(v%26)}, s...)
		v /= 26
	}
	return string(s)
}

func roman(v int) string {
	if v <= 0 || v >= 4000 {
		return strconv.Itoa(v)
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, x := range vals {
		for v >= x {
			b.WriteString(syms[i])
			v -= x
		}
	}
	return b.String()
}
