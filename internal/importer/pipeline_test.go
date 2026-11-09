package importer

import (
	"context"
	"testing"

	"peetraining-server/internal/ai"
)

func numberedBlocks(n int) []ai.Block {
	blocks := make([]ai.Block, n)
	for i := range blocks {
		blocks[i] = ai.Block{ID: i + 1, Page: 1, QType: "term", Text: "题"}
	}
	return blocks
}

func coveredBlocks(items []ai.QItem) map[int]int {
	got := map[int]int{}
	for _, it := range items {
		got[it.Block]++
	}
	return got
}

// 线上实测模型单批只回约 30 条：每批 20 块时首轮就应全部覆盖，不依赖补漏。
func TestStructureAllChunkBelowModelCap(t *testing.T) {
	blocks := numberedBlocks(120)
	calls := 0
	batch := func(_ context.Context, bs []ai.Block) ([]ai.QItem, error) {
		calls++
		n := len(bs)
		if n > 30 {
			n = 30
		}
		var out []ai.QItem
		for _, b := range bs[:n] {
			out = append(out, ai.QItem{Block: b.ID, Kind: "question"})
		}
		return out, nil
	}
	items, err := structureAll(context.Background(), blocks, batch)
	if err != nil {
		t.Fatal(err)
	}
	got := coveredBlocks(items)
	if len(got) != 120 {
		t.Fatalf("应覆盖 120 块，实际 %d 块", len(got))
	}
	for id, n := range got {
		if n != 1 {
			t.Fatalf("块 %d 出现 %d 次", id, n)
		}
	}
	if calls != 6 {
		t.Fatalf("120 块按每批 20 块应调用 6 次，实际 %d 次", calls)
	}
}

// 模型整批少回几条时，补漏轮只请求缺口，仍能覆盖全部块。
func TestStructureAllRecoversOmittedBlocks(t *testing.T) {
	blocks := numberedBlocks(45)
	batch := func(_ context.Context, bs []ai.Block) ([]ai.QItem, error) {
		n := len(bs)
		if n > 10 {
			n -= 2 // 模拟模型少回两条
		}
		var out []ai.QItem
		for _, b := range bs[:n] {
			out = append(out, ai.QItem{Block: b.ID, Kind: "question"})
		}
		return out, nil
	}
	items, err := structureAll(context.Background(), blocks, batch)
	if err != nil {
		t.Fatal(err)
	}
	got := coveredBlocks(items)
	if len(got) != 45 {
		t.Fatalf("补漏后应覆盖 45 块，实际 %d 块", len(got))
	}
}

// 补漏后仍没有条目的块用规则原文兜底；整页块规则拆不开则计入失败。
func TestAssembleStructuredFallback(t *testing.T) {
	blocks := []ai.Block{
		{ID: 1, Page: 1, QType: "term", Text: "1. 柏拉图"},
		{ID: 2, Page: 1, QType: "term", Text: "2. 亚里士多德"},
		{ID: 3, Page: 1, QType: "term", Text: "3. 奥古斯丁"},
	}
	items := []ai.QItem{{Block: 1, Kind: "question"}, {Block: 2, Kind: "question"}}
	ordered, failed := assembleStructured(blocks, items)
	if failed != 0 {
		t.Fatalf("普通块应规则兜底，failed=%d", failed)
	}
	if len(ordered) != 3 {
		t.Fatalf("应输出 3 条，实际 %d 条", len(ordered))
	}
	if ordered[2].Block != 3 || ordered[2].Confidence > FallbackConfidence {
		t.Fatalf("缺失块应由规则兜底并降低置信度：%+v", ordered[2])
	}

	whole := []ai.Block{{ID: 9, Page: 1, Whole: true, Text: "整页原文"}}
	ordered, failed = assembleStructured(whole, nil)
	if failed != 1 || len(ordered) != 0 {
		t.Fatalf("整页块规则拆不开应计入失败：failed=%d len=%d", failed, len(ordered))
	}
}
