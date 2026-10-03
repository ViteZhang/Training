package extract

import (
	"bytes"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Xlsx 读 Excel（.xlsx）：每张工作表第一行非空行当表头；每 rowsPerPage 行数据一页、计 1 页（PRD 11.12）。
// 每行写成「表头：值」的形式，切题与 AI 结构化不需要再猜列的含义。
func Xlsx(data []byte, rowsPerPage int) (Result, error) {
	if isOLE(data) {
		return Result{}, userErr("这是旧版 Excel（.xls）或加密的表格，暂不支持。请另存为 .xlsx（不设密码）后再上传")
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return Result{}, userErr("文件已损坏或不是 Excel 表格，请检查后重新上传")
	}
	defer f.Close()

	var pages []Page
	for _, sheet := range f.GetSheetList() {
		if v, err := f.GetSheetVisible(sheet); err == nil && !v {
			continue
		}
		rows, err := f.GetRows(sheet)
		if err != nil {
			return Result{}, userErr("表格内容无法读取，请用 Excel 或 WPS 打开后重新保存再上传")
		}
		var header []string
		var data [][]string
		for _, r := range rows {
			r = trimRow(r)
			if len(r) == 0 {
				continue
			}
			if header == nil {
				header = r
				continue
			}
			data = append(data, r)
		}
		for start := 0; start < len(data); start += rowsPerPage {
			chunk := data[start:min(start+rowsPerPage, len(data))]
			var b strings.Builder
			b.WriteString("【" + sheet + "】")
			for _, r := range chunk {
				b.WriteString("\n")
				var cells []string
				for i, v := range r {
					if v == "" {
						continue
					}
					name := ""
					if i < len(header) {
						name = header[i]
					}
					if name == "" {
						cells = append(cells, v)
					} else {
						cells = append(cells, name+"："+v)
					}
				}
				b.WriteString(strings.Join(cells, "\n"))
				b.WriteString("\n")
			}
			pages = append(pages, Page{No: len(pages) + 1, Text: strings.TrimRight(b.String(), "\n"), Tables: []Table{{Header: header, Rows: chunk}}})
		}
	}
	if len(pages) == 0 {
		return Result{}, userErr("表格里没有读到数据：第一行应是表头，从第二行起每行一道题")
	}
	return Result{Pages: pages, BilledPages: len(pages)}, nil
}

func trimRow(r []string) []string {
	for i := range r {
		r[i] = strings.TrimSpace(r[i])
	}
	end := len(r)
	for end > 0 && r[end-1] == "" {
		end--
	}
	return r[:end]
}

// TemplateColumns 是导入模板的列（dev-spec 第六节「Excel 提供导入模板」）。
var TemplateColumns = []string{"题型", "题干", "选项", "答案", "分值", "年份", "题号", "知识点"}

// Template 生成 Excel 导入模板：表头 + 两行示例 + 填写说明。
func Template() ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "题目"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	rows := [][]any{
		toAny(TemplateColumns),
		{"名词解释", "意境", "", "意境是抒情性作品中呈现的情景交融、虚实相生的形象系统……", 5, 2024, "1", "意境"},
		{"单选", "下列属于唐代传奇的是", "A. 莺莺传\nB. 搜神记\nC. 世说新语\nD. 聊斋志异", "A", 2, 2023, "3", "唐传奇"},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow(sheet, cell, &r); err != nil {
			return nil, err
		}
	}
	help := "说明"
	if _, err := f.NewSheet(help); err != nil {
		return nil, err
	}
	notes := []string{
		"第一行是表头，不要改；从第二行起每行一道题",
		"题型：单选、多选、判断、填空、名词解释、简答、论述、作文",
		"选项：每个选项一行，以 A. B. C. 开头；主观题留空",
		"答案：客观题写选项字母；主观题写参考答案，没有可留空，导入后可让 AI 生成",
		"分值、年份、题号、知识点都可以留空",
	}
	for i, n := range notes {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetCellValue(help, cell, n); err != nil {
			return nil, err
		}
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
