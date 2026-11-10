package practice

import (
	"slices"
	"testing"
)

// 每道题从不同位置取片段，片段少于每题份额时全给。
func TestSampleExcerpts(t *testing.T) {
	few := []string{"a", "b"}
	if got := sampleExcerpts(few, 3); !slices.Equal(got, few) {
		t.Fatal(got)
	}
	all := []string{"0", "1", "2", "3", "4", "5"}
	if got := sampleExcerpts(all, 0); !slices.Equal(got, []string{"0", "1", "2", "3"}) {
		t.Fatal(got)
	}
	if got := sampleExcerpts(all, 2); !slices.Equal(got, []string{"4", "5", "0", "1"}) {
		t.Fatal(got)
	}
	if got := sampleExcerpts(nil, 1); len(got) != 0 {
		t.Fatal(got)
	}
}
