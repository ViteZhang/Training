package extract

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func makeDocx(t *testing.T, body, numbering string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="` + wNS + `"><w:body>` + body + `</w:body></w:document>`,
	}
	if numbering != "" {
		files["word/numbering.xml"] = `<?xml version="1.0" encoding="UTF-8"?><w:numbering xmlns:w="` + wNS + `">` + numbering + `</w:numbering>`
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func p(text string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func np(numID, ilvl, text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + ilvl + `"/><w:numId w:val="` + numID + `"/></w:numPr></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

const examNumbering = `
<w:abstractNum w:abstractNumId="0">
  <w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="chineseCounting"/><w:lvlText w:val="%1、"/></w:lvl>
  <w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%2."/></w:lvl>
  <w:lvl w:ilvl="2"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="(%3)"/></w:lvl>
</w:abstractNum>
<w:abstractNum w:abstractNumId="1"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
<w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num>`

func TestDocxNumberingAndTables(t *testing.T) {
	body := p("2024 年中国语言文学基础真题") +
		np("1", "0", "名词解释（每题 5 分）") + np("1", "1", "意境") + np("1", "1", "典型") +
		np("1", "0", "简答题") + np("1", "1", "简述唐传奇的艺术成就") + np("1", "2", "情节") +
		np("2", "0", "无编号的列表项") +
		`<w:p><w:r><w:t>保留</w:t></w:r><w:del><w:r><w:t>删掉的修订</w:t></w:r></w:del><w:r><w:tab/><w:t>制表</w:t><w:br/><w:t>换行</w:t></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc>` + p("题号") + `</w:tc><w:tc>` + p("答案") + `</w:tc></w:tr>` +
		`<w:tr><w:tc>` + p("1") + `</w:tc><w:tc>` + p("A") + p("第二段") + `</w:tc></w:tr></w:tbl>` +
		p("") + p("   ")
	res, err := Docx(makeDocx(t, body, examNumbering), 1500)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 1 || res.BilledPages != 1 {
		t.Fatalf("页数：%+v", res)
	}
	want := strings.Join([]string{
		"2024 年中国语言文学基础真题",
		"一、 名词解释（每题 5 分）", "1. 意境", "2. 典型",
		"二、 简答题", "1. 简述唐传奇的艺术成就", "(1) 情节",
		"无编号的列表项",
		"保留\t制表\n换行",
		"题号 | 答案\n1 | A\n第二段",
	}, "\n")
	if got := res.Pages[0].Text; got != want {
		t.Fatalf("文字：\n%s\n---want---\n%s", got, want)
	}
	tb := res.Pages[0].Tables
	if len(tb) != 1 || strings.Join(tb[0].Header, ",") != "题号,答案" || tb[0].Rows[0][1] != "A\n第二段" {
		t.Fatalf("表格：%+v", tb)
	}
}

func TestDocxPaging(t *testing.T) {
	var body strings.Builder
	for i := range 40 {
		body.WriteString(p(fmt.Sprintf("%02d", i) + strings.Repeat("字", 98))) // 每段 100 字 + 换行
	}
	body.WriteString(p(strings.Repeat("长", 3200))) // 单段超过一页
	res, err := Docx(makeDocx(t, body.String(), ""), 1500)
	if err != nil {
		t.Fatal(err)
	}
	// 40 段 × 101 = 4040 字：每页放 14 段 → 3 页；长段 3200 字另起页再切 3 页
	if len(res.Pages) != 6 || res.BilledPages != 6 {
		t.Fatalf("页数 %d", len(res.Pages))
	}
	if !strings.HasPrefix(res.Pages[1].Text, "14") || !strings.HasPrefix(res.Pages[3].Text, "长") {
		t.Fatalf("段落不应被拆到两页：%q %q", res.Pages[1].Text[:2], res.Pages[3].Text[:3])
	}
	for i, pg := range res.Pages {
		if pg.No != i+1 || len([]rune(pg.Text)) > 1500 {
			t.Fatalf("第 %d 页：No=%d 字数=%d", i+1, pg.No, len([]rune(pg.Text)))
		}
	}
}

func TestDocxRejects(t *testing.T) {
	cases := map[string][]byte{
		"旧版 doc": append(append([]byte{}, oleMagic...), make([]byte, 100)...),
		"不是 zip": []byte("hello"),
		"没有正文":   makeDocx(t, p(" "), ""),
	}
	for name, data := range cases {
		_, err := Docx(data, 1500)
		ue, ok := AsUserError(err)
		if !ok || ue.Msg == "" {
			t.Errorf("%s：应返回用户能看懂的原因，got %v", name, err)
		}
	}
	_, err := Docx(append(append([]byte{}, oleMagic...), 0), 1500)
	if ue, _ := AsUserError(err); !strings.Contains(ue.Msg, ".docx") {
		t.Errorf("旧版 .doc 应提示另存为 .docx：%v", err)
	}
}

func TestNumberFormats(t *testing.T) {
	cases := []struct {
		v    int
		f    string
		want string
	}{
		{1, "chineseCounting", "一"}, {10, "chineseCounting", "十"}, {12, "chineseCounting", "十二"},
		{20, "chineseCounting", "二十"}, {35, "chineseCounting", "三十五"}, {120, "chineseCounting", "120"},
		{3, "decimalEnclosedCircle", "③"}, {27, "lowerLetter", "aa"}, {2, "upperLetter", "B"},
		{4, "upperRoman", "IV"}, {9, "lowerRoman", "ix"}, {7, "decimal", "7"}, {0, "upperRoman", "0"},
	}
	for _, c := range cases {
		if got := formatNum(c.v, c.f); got != c.want {
			t.Errorf("formatNum(%d, %s) = %s，want %s", c.v, c.f, got, c.want)
		}
	}
}

func TestXlsxTemplateAndPaging(t *testing.T) {
	tpl, err := Template()
	if err != nil {
		t.Fatal(err)
	}
	res, err := Xlsx(tpl, 50)
	if err != nil {
		t.Fatal(err)
	}
	// 模板：题目表 2 行示例 → 1 页；说明表第一行当表头、4 行 → 1 页
	if len(res.Pages) != 2 || !strings.Contains(res.Pages[0].Text, "题型：名词解释\n题干：意境") {
		t.Fatalf("模板：%+v", res.Pages)
	}
	if res.Pages[0].Tables[0].Header[1] != "题干" || len(res.Pages[0].Tables[0].Rows) != 2 {
		t.Fatalf("表格：%+v", res.Pages[0].Tables)
	}

	f := excelize.NewFile()
	_ = f.SetSheetRow("Sheet1", "A1", &[]any{"题型", "题干", "答案"})
	for i := range 120 {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		_ = f.SetSheetRow("Sheet1", cell, &[]any{"简答", fmt.Sprintf("第 %d 题", i+1), ""})
	}
	_ = f.SetSheetRow("Sheet1", "A200", &[]any{"", " ", ""}) // 空行跳过
	hidden, _ := f.NewSheet("隐藏")
	_ = f.SetSheetRow("隐藏", "A1", &[]any{"x"})
	_ = f.SetSheetRow("隐藏", "A2", &[]any{"y"})
	_ = f.SetSheetVisible("隐藏", false)
	_ = hidden
	buf, _ := f.WriteToBuffer()
	res, err = Xlsx(buf.Bytes(), 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pages) != 3 || res.BilledPages != 3 || len(res.Pages[2].Tables[0].Rows) != 20 {
		t.Fatalf("120 行应计 3 页：%d", len(res.Pages))
	}
	if strings.Contains(res.Pages[0].Text, "答案：") {
		t.Error("空单元格不应输出")
	}

	empty := excelize.NewFile()
	_ = empty.SetSheetRow("Sheet1", "A1", &[]any{"只有表头"})
	buf, _ = empty.WriteToBuffer()
	if _, err := Xlsx(buf.Bytes(), 50); err == nil {
		t.Error("没有数据行应报错")
	}
	for _, bad := range [][]byte{[]byte("nope"), append(append([]byte{}, oleMagic...), 0)} {
		if _, ok := AsUserError(func() error { _, err := Xlsx(bad, 50); return err }()); !ok {
			t.Error("坏文件应返回 UserError")
		}
	}
}

func TestText(t *testing.T) {
	r := Text("  "+strings.Repeat("字", 3001)+"\n", 1500)
	if r.BilledPages != 3 || r.Pages[2].No != 3 || r.Pages[2].Text != "字" {
		t.Fatalf("%+v", r.BilledPages)
	}
}
