package textbook

import (
	"context"
	"strings"
	"testing"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/config"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
)

// TestSampleMaterial：后台批量出体验题关联已发布教材时，返回书名和检索到的原文片段；未发布的版本报错。
func TestSampleMaterial(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	s.ConfigureIndexes(IndexDeps{Embedding: ai.NewEmbedder(config.TextbookConfig{EmbeddingProvider: "mock", EmbeddingDimensions: 256, EmbeddingModel: "mock"}),
		Vector: &fakeVector{}, Params: params.New(dbq.New(db)), Queue: &importQueue{fail: true}, VectorConfigured: true})
	b, err := s.Create(ctx, 1, "sample-book", rules.TextbookMetadata{Title: "中国古代文学史", Author: "袁行霈", Kind: rules.MaterialTextbook, Discipline: "文学"}, edition())
	if err != nil {
		t.Fatal(err)
	}
	ed := b.Editions[0]
	r, err := s.NewRevision(ctx, b.ID, ed.ID, 1, "sample-rev")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.SaveRevision(ctx, b.ID, ed.ID, r.ID, 1, r.Version, []rules.TextbookSection{{Heading: "诗经", Text: strings.Repeat("诗经是中国古代文学最早的诗歌总集。", 200)}}, true)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.BuildIndex(ctx, b.ID, ed.ID, r.ID, 1, r.Version, "sample-idx")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessIndex(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	// 还没发布：不能用于出题。
	if _, _, err := s.SampleMaterial(ctx, []uint64{ed.ID}, "文学 古代文学"); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("未发布的版本要报错：%v", err)
	}
	if _, err := s.Publish(ctx, b.ID, ed.ID, r.ID, 1, PublishInput{"sample-pub", r.Version, ed.Version, g.ID, "发布"}); err != nil {
		t.Fatal(err)
	}
	names, excerpts, err := s.SampleMaterial(ctx, []uint64{ed.ID, ed.ID}, "文学 古代文学")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || !strings.Contains(names[0], "中国古代文学史") || !strings.Contains(names[0], "袁行霈") {
		t.Fatalf("书名：%v", names)
	}
	if len(excerpts) == 0 || len(excerpts) > maxSampleExcerpts || !strings.Contains(excerpts[0], "诗经") || !strings.HasPrefix(excerpts[0], "【中国古代文学史") {
		t.Fatalf("片段：%v", excerpts)
	}
	if _, _, err := s.SampleMaterial(ctx, []uint64{ed.ID, 999999}, "文学"); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("不存在的版本要报错：%v", err)
	}
	if n, e, err := s.SampleMaterial(ctx, nil, "文学"); err != nil || n != nil || e != nil {
		t.Fatal("不关联教材时什么都不做", err)
	}
}
