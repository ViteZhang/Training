package events

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRecord(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 10, 4, 2, 0, 0, 0, time.UTC)
	r := New(slog.New(slog.NewJSONHandler(&buf, nil)), func() time.Time { return now })
	n := r.Record(context.Background(), Common{UserID: 7, SubjectCode: "654", Stage: "strengthen", AppVersion: "1.0.0", Platform: "ios"}, []Event{
		{Name: "answer_submit", At: now.Add(-time.Minute), Props: map[string]any{
			"qtype": "term", "duration_sec": float64(95), "timed": true,
			"answer_text": "我的作答原文", "stem": "题干", "note": "x", "long": strings.Repeat("长", 65), "BadKey": "x", "nested": map[string]any{"a": 1},
		}},
		{Name: "unknown_event"},
		{Name: "paper_submit", At: now.AddDate(-1, 0, 0)},
	})
	if n != 2 {
		t.Fatalf("收下 2 条（不认识的跳过）：%d", n)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var first, second map[string]any
	_ = json.Unmarshal([]byte(lines[0]), &first)
	_ = json.Unmarshal([]byte(lines[1]), &second)
	if first["log_type"] != "event" || first["event"] != "answer_submit" || first["user_id"] != float64(7) || first["subject_code"] != "654" {
		t.Errorf("公共字段：%v", first)
	}
	props := first["props"].(map[string]any)
	if len(props) != 3 || props["qtype"] != "term" || props["timed"] != true {
		t.Errorf("只留短的标量属性，不留像正文的键：%v", props)
	}
	if strings.Contains(buf.String(), "我的作答原文") || strings.Contains(buf.String(), "题干") {
		t.Error("事件里不能带作答原文和题目内容")
	}
	if second["event_at"] != now.Format(time.RFC3339) {
		t.Errorf("时间太久远的按服务端时间记：%v", second["event_at"])
	}
}
