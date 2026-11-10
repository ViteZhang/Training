package ai

import (
	"strings"
	"testing"
)

// 关联教材时提示词带上原文摘录；没关联时（含 App 现场出题）不出现这一行。
func TestSampleQuestionPromptExcerpts(t *testing.T) {
	in := SampleQuestionIn{Subject: "文学 · 古代文学", Textbooks: []string{"中国古代文学史"}, QType: "term", Score: 5}
	_, usr, err := render("sample_question", SampleQuestion.Version, in)
	if err != nil || strings.Contains(usr, "教材原文摘录") {
		t.Fatalf("没有摘录：%v\n%s", err, usr)
	}
	in.Excerpts = []string{"【中国古代文学史 · 诗经】诗经是最早的诗歌总集。"}
	_, usr, err = render("sample_question", SampleQuestion.Version, in)
	if err != nil || !strings.Contains(usr, "教材原文摘录") || !strings.Contains(usr, "诗经是最早的诗歌总集") {
		t.Fatalf("有摘录：%v\n%s", err, usr)
	}
}
