package rules

import (
	"strings"
	"testing"
)

func TestTextbookSplitKeepsExactLocations(t *testing.T) {
	p := TextbookRetrievalParams{"v1", 200, 30, 20, 8, 6000}
	text := strings.Repeat("文学的审美性质与社会意识。\n", 70)
	chunks, err := TextbookChunks([]TextbookSection{{Heading: "第一章", Text: text}}, p)
	if err != nil {
		t.Fatal(err)
	}
	r := []rune(text)
	covered := make([]bool, len(r))
	for _, c := range chunks {
		if c.Text != string(r[c.Start:c.End]) || len([]rune(c.Text)) > 200 {
			t.Fatal(c)
		}
		for i := c.Start; i < c.End; i++ {
			covered[i] = true
		}
	}
	for _, v := range covered {
		if !v {
			t.Fatal("原文丢失")
		}
	}
	again, _ := TextbookChunks([]TextbookSection{{Heading: "第一章", Text: text}}, p)
	if len(again) != len(chunks) || again[1] != chunks[1] {
		t.Fatal("定位不稳定")
	}
}
func TestTextbookRRFUsesIndependentRanks(t *testing.T) {
	out := FuseTextbookRanks([]uint32{1, 2, 3}, []uint32{3, 4, 2})
	if out[0].Ordinal != 3 || out[1].Ordinal != 2 || len(out) != 4 {
		t.Fatal(out)
	}
	if out[0].Score <= out[2].Score || out[0].Score >= 1 {
		t.Fatal(out)
	}
}
