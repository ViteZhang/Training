package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/ai"
)

func TestEditDistanceAndOCRNormalize(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"意境", "意境", 0},
		{"意境是情景交融", "意竟是情景交融", 1},
		{"典型", "", 2},
		{"", "典型", 2},
		{"情景交融", "情交融景", 2},
	}
	for _, c := range cases {
		if got := editDistance([]rune(c.a), []rune(c.b)); got != c.want {
			t.Errorf("%q → %q：%d，期望 %d", c.a, c.b, got, c.want)
		}
	}
	// 换行、空格与半角标点不算错字。
	if a, b := ocrNormalize("意境是情景交融,\n虚实相生."), ocrNormalize("意境是 情景交融，虚实相生。"); a != b {
		t.Errorf("%q != %q", a, b)
	}
}

func TestRubricHit(t *testing.T) {
	got := []ai.RubricPoint{{Content: "意境是情景交融的"}, {Content: "虚实相生"}}
	if !rubricHit1("情景交融", got) || !rubricHit1("虚实相生", got) {
		t.Error("同义的采分点应算召回")
	}
	if rubricHit1("韵味无穷", got) {
		t.Error("没提到的采分点不算召回")
	}
}

func TestStatsSummary(t *testing.T) {
	s := &stats{}
	for i := range 10 {
		s.add(ai.Call{Capability: "grade_subjective", Model: "m", Version: "v1", CostMicroYuan: 2000, Latency: time.Duration(i+1) * 100 * time.Millisecond})
	}
	s.add(ai.Call{Capability: "grade_subjective", Model: "m", Version: "v1", ErrorKind: "invalid_output", CostMicroYuan: 2000, Latency: time.Second})
	sum := s.summary()
	// 每次 0.002 元 → 每千次 2 元。
	if !strings.Contains(sum, "调用 11 次（失败 1）") || !strings.Contains(sum, "每千次 ¥2.00") || !strings.Contains(sum, "P95 1s") {
		t.Errorf("汇总：%s", sum)
	}
	if s.versionList() != "grade_subjective@v1 · m" {
		t.Errorf("版本：%s", s.versionList())
	}
}

func TestAppendRecord(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("## 记录\n\n| 日期 | 能力 |\n| --- | --- |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := appendRecord(readme, "grading", 20, result{pass: true, metrics: "偏差 ≤ 1 分 85%"}, "grade_subjective@v2 · qwen-max", "每千次 ¥3.10", "加了 | 示例", filepath.Join(dir, "samples")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(readme)
	last := strings.TrimSpace(string(b))
	last = last[strings.LastIndex(last, "\n")+1:]
	for _, want := range []string{"| grading | 20 | 偏差 ≤ 1 分 85% | 是 | grade_subjective@v2 · qwen-max | 每千次 ¥3.10 |", "公开示例 加了 / 示例"} {
		if !strings.Contains(last, want) {
			t.Errorf("记录行 %q 缺 %q", last, want)
		}
	}
}
