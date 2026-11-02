package rules

import (
	"strings"
	"testing"
)

func TestTextbookValidation(t *testing.T) {
	m := TextbookMetadata{Title: " 教材 ", Author: " 编者 ", Kind: "handout", Discipline: "文学", Aliases: []string{"讲义", "讲义"}}
	normalized, err := NormalizeTextbook(m)
	if err != nil || normalized.Title != "教材" || len(normalized.Aliases) != 1 {
		t.Fatal(normalized, err)
	}
	for _, title := range []string{"  ", strings.Repeat("书", 129)} {
		m.Title = title
		if _, err := NormalizeTextbook(m); err == nil {
			t.Fatal("应拒绝无效书名")
		}
	}
	e := TextbookEditionInput{Edition: "春季讲义", LicenseNote: "授权"}
	if _, err := NormalizeTextbookEdition(e); err == nil {
		t.Fatal("无授权不能导入")
	}
	e.Authorized = true
	if _, err := NormalizeTextbookEdition(e); err != nil {
		t.Fatal("无ISBN讲义应允许", err)
	}
	original := "  原文中的空格\n不应润色。\n"
	sections, err := NormalizeTextbookSections([]TextbookSection{{Text: original}})
	if err != nil || sections[0].Text != original {
		t.Fatal("正文被改写")
	}
	page := 0
	for _, v := range [][]TextbookSection{nil, {{Text: "  "}}, {{Text: "原文", SourcePage: &page}}, {{Text: strings.Repeat("字", 100001)}}} {
		if _, err := NormalizeTextbookSections(v); err == nil {
			t.Fatal("应拒绝无效原文")
		}
	}
}

func TestNormalizeTextbookEditionScopes(t *testing.T) {
	valid := TextbookEditionScopeUpdate{EditionID: 1, ExpectedVersion: 1, ScopeNote: "  院校课程  "}
	got, err := NormalizeTextbookEditionScopes([]TextbookEditionScopeUpdate{valid})
	if err != nil || got[0].ScopeNote != "院校课程" {
		t.Fatalf("%+v %v", got, err)
	}
	valid.ScopeNote = ""
	if _, err := NormalizeTextbookEditionScopes([]TextbookEditionScopeUpdate{valid}); err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]TextbookEditionScopeUpdate{
		{{EditionID: 0, ExpectedVersion: 1}}, {{EditionID: 1}}, {valid, valid},
		{{EditionID: 1, ExpectedVersion: 1, ScopeNote: strings.Repeat("字", 256)}},
		make([]TextbookEditionScopeUpdate, 1001),
	} {
		if _, err := NormalizeTextbookEditionScopes(scopes); err == nil {
			t.Fatalf("invalid scopes accepted: %+v", scopes)
		}
	}
}

func TestTextbookSelectionsAllowSeries(t *testing.T) {
	got, err := CleanTextbookSelections([]string{"西方哲学史"}, []TextbookSelection{{"西方哲学史", 1}, {"西方哲学史", 2}, {"西方哲学史", 2}, {"已删除", 3}})
	if err != nil || len(got) != 2 || got[0].EditionID != 1 || got[1].EditionID != 2 {
		t.Fatal(got, err)
	}
	many := []TextbookSelection{}
	for i := 1; i <= MaxEditionsPerTextbook+1; i++ {
		many = append(many, TextbookSelection{"西方哲学史", uint64(i)})
	}
	if _, err = CleanTextbookSelections([]string{"西方哲学史"}, many); err == nil {
		t.Fatal("超出册数上限未拒绝")
	}
	if _, err = CleanTextbookSelections([]string{"西方哲学史"}, []TextbookSelection{{"西方哲学史", 0}}); err == nil {
		t.Fatal("空版次未拒绝")
	}
	if _, err = NormalizeTextbook(TextbookMetadata{Title: "a", Author: "b", Kind: "textbook", Discipline: "c", Series: string(make([]rune, 129))}); err == nil {
		t.Fatal("套书名超长未拒绝")
	}
}
