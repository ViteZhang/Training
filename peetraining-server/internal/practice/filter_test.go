package practice

import (
	"testing"

	"peetraining-server/internal/dbq"
)

// 题目详情「再练这道题」：给了题目 ID 就只练这几道，不再看题型等其他条件。
func TestFilterByQuestionIDs(t *testing.T) {
	items := []poolItem{
		{ListPracticePoolRow: dbq.ListPracticePoolRow{ID: 1, Qtype: "term"}},
		{ListPracticePoolRow: dbq.ListPracticePoolRow{ID: 2, Qtype: "essay"}},
		{ListPracticePoolRow: dbq.ListPracticePoolRow{ID: 3, Qtype: "term"}},
	}
	got := filter(items, Config{QuestionIDs: []uint64{2, 99}, QTypes: []string{"term"}})
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("应只返回题 2，得到 %+v", got)
	}
	if got := filter(items, Config{QTypes: []string{"term"}}); len(got) != 2 {
		t.Fatalf("不给题目 ID 时按题型筛，得到 %d 道", len(got))
	}
}
