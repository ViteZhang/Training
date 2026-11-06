package official

import (
	"testing"

	"peetraining-server/internal/rules"
)

func TestFrameworkText(t *testing.T) {
	secs := []rules.TextbookSection{{Heading: "第一编 文学活动", Text: "文学活动的四要素……"}, {Heading: "第八章 文学意境", Text: "意境是……"}, {Heading: "", Text: "附录"}}
	if got := FrameworkText(rules.MaterialTextbook, secs); got != "第一编 文学活动\n第八章 文学意境" {
		t.Errorf("教材只取章节标题：%q", got)
	}
	if got := FrameworkText(rules.MaterialSyllabus, secs); got != "第一编 文学活动\n文学活动的四要素……\n第八章 文学意境\n意境是……\n附录" {
		t.Errorf("考纲用全文：%q", got)
	}
}
