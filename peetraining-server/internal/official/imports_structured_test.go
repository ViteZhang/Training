package official

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseGroupedTableSubquestionsAndOptions(t *testing.T) {
	raw := `年份,题号,题型,分值,题干,大题标识,子问序号,子问题干,子问分值,子问采分点,选项A,选项B,选项C,选项D,正确选项
2026,33,材料分析,15,阅读材料并回答。,333-2026-33,1,第一问,7,"要点一 4
要点二 3",,,,,
2026,33,材料分析,15,阅读材料并回答。,333-2026-33,2,第二问,8,要点三 8,,,,,`
	r := csv.NewReader(strings.NewReader(raw))
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		if k, ok := tableColumns[strings.TrimSpace(h)]; ok {
			col[k] = i
		}
	}
	items, err := (&Service{}).parseGroupedTable(context.Background(), 1, rows, col, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("应合并成一道题，实际 %d", len(items))
	}
	var p QuestionPayload
	if err := json.Unmarshal(items[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.QType != "material_analysis" || len(p.Subquestions) != 2 || *p.Subquestions[0].FullScore != 7 || *p.Subquestions[1].FullScore != 8 {
		t.Fatalf("分问解析不对：%+v", p)
	}
	if len(p.Rubric) != 3 || p.Rubric[0].SubquestionOrdinal != 1 || p.Rubric[1].SubquestionOrdinal != 1 || p.Rubric[2].SubquestionOrdinal != 2 {
		t.Fatalf("分问采分点归属不对：%+v", p.Rubric)
	}
}

func TestParseGroupedTableSingleChoice(t *testing.T) {
	raw := `年份,题号,题型,分值,题干,大题标识,选项A,选项B,选项C,选项D,正确选项
2026,1,单选,2,题干,311-2026-01,甲,乙,丙,丁,C`
	r := csv.NewReader(strings.NewReader(raw))
	rows, _ := r.ReadAll()
	col := map[string]int{}
	for i, h := range rows[0] {
		if k, ok := tableColumns[strings.TrimSpace(h)]; ok {
			col[k] = i
		}
	}
	items, err := (&Service{}).parseGroupedTable(context.Background(), 1, rows, col, false)
	if err != nil {
		t.Fatal(err)
	}
	var p QuestionPayload
	_ = json.Unmarshal(items[0].Payload, &p)
	if len(p.Options) != 4 || p.Answer != "C" || p.QType != "single_choice" {
		t.Fatalf("单选解析不对：%+v", p)
	}
}
