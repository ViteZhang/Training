package textbook

import (
	"context"
	"strings"
	"testing"

	"peetraining-server/internal/ai"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/config"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
)

// TestPrepareAnswerSearchesEveryBook：项目关联多本书时每本都检索，按相似度取段落。
// 以前按关联顺序一本本取，前两本占满名额后后面的书不再检索，相关的教材排在后面就永远用不上。
func TestPrepareAnswerSearchesEveryBook(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	collections := map[uint64]string{}
	relevant := ""
	vec := &fakeVector{score: func(name string) float64 {
		if name == relevant {
			return .92
		}
		return .41
	}}
	s.ConfigureIndexes(IndexDeps{Embedding: ai.NewEmbedder(config.TextbookConfig{EmbeddingProvider: "mock", EmbeddingDimensions: 256, EmbeddingModel: "mock"}),
		Vector: vec, Params: params.New(dbq.New(db)), Queue: &importQueue{fail: true}, VectorConfigured: true})
	s.ConfigureEvidence(ai.NewEngine(ai.Config{Client: selectFirstClient{}}), flags.New(dbq.New(db)))
	build := func(key, title, text string) dbq.TextbookEdition {
		t.Helper()
		b, err := s.Create(ctx, 1, key, rules.TextbookMetadata{Title: title, Author: "某作者", Kind: rules.MaterialTextbook, Discipline: "文学"}, edition())
		if err != nil {
			t.Fatal(err)
		}
		ed := b.Editions[0]
		r, err := s.NewRevision(ctx, b.ID, ed.ID, 1, key+"-rev")
		if err != nil {
			t.Fatal(err)
		}
		r, err = s.SaveRevision(ctx, b.ID, ed.ID, r.ID, 1, r.Version, []rules.TextbookSection{{Heading: title, Text: strings.Repeat(text, 400)}}, true)
		if err != nil {
			t.Fatal(err)
		}
		g, err := s.BuildIndex(ctx, b.ID, ed.ID, r.ID, 1, r.Version, key+"-idx")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ProcessIndex(ctx, g.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Publish(ctx, b.ID, ed.ID, r.ID, 1, PublishInput{key + "-pub", r.Version, ed.Version, g.ID, "发布"}); err != nil {
			t.Fatal(err)
		}
		g, err = dbq.New(db).GetTextbookIndex(ctx, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		collections[ed.ID] = g.CollectionName
		out, err := dbq.New(db).GetTextbookEditionByID(ctx, ed.ID)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// 关联顺序按版本编号：两本不相关的书在前，语言学教材在最后。
	a := build("rank-book-a", "中国古代文学史", "诗经是中国最早的诗歌总集。")
	b := build("rank-book-b", "外国文学史", "荷马史诗包括伊利亚特和奥德赛。")
	c := build("rank-book-c", "语言学纲要", "组合关系和聚合关系是语言系统中的两种基本关系。")
	relevant = collections[c.ID]
	if relevant == collections[a.ID] {
		t.Fatal("每本书要有自己的向量集合，测试才能区分相似度")
	}

	in, err := s.PrepareAnswer(ctx, 0, ai.GenerateAnswerIn{Subject: "语言学", QType: "essay", Stem: "试论述语言的组合关系和聚合关系", Score: 20, EditionIDs: []uint64{a.ID, b.ID, c.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if in.Basis != nil {
		t.Fatalf("用上了教材不该有原因：%+v", in.Basis)
	}
	order := []uint64{}
	fromC := 0
	for _, f := range in.Evidence {
		order = append(order, f.EditionID)
		if f.EditionID == c.ID {
			fromC++
		}
	}
	if len(order) == 0 || order[0] != c.ID {
		t.Fatalf("相似度最高的语言学教材（%d）要排在最前，实际各段来自：%v", c.ID, order)
	}
	if fromC < 8 {
		t.Fatalf("排在前面的要是语言学教材的段落，只取到 %d 段", fromC)
	}
}

// selectFirstClient 按给的顺序选前 8 段（相关性挑选最多 8 段），用来看排在前面的是哪本书。
type selectFirstClient struct{}

func (selectFirstClient) Complete(_ context.Context, r cloudai.Request) (cloudai.Response, error) {
	ids := []string{}
	for _, m := range reEvidenceID.FindAllStringSubmatch(r.Messages[len(r.Messages)-1].Content, 8) {
		ids = append(ids, `"`+m[1]+`"`)
	}
	return cloudai.Response{Content: `{"coverage":"partial","source_ids":[` + strings.Join(ids, ",") + `]}`}, nil
}
