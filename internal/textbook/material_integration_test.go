package textbook

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/config"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
)

// selectAllClient 在挑选相关段落时把给的段落全选上，并记下提示词，用来检查资料类型有没有交给模型。
type selectAllClient struct {
	mu      sync.Mutex
	prompts []string
}

var reEvidenceID = regexp.MustCompile(`ID=(textbook:\d+:\d+)`)

func (c *selectAllClient) Complete(_ context.Context, r cloudai.Request) (cloudai.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	user := r.Messages[len(r.Messages)-1].Content
	c.prompts = append(c.prompts, user)
	ids := []string{}
	for _, m := range reEvidenceID.FindAllStringSubmatch(user, -1) {
		ids = append(ids, `"`+m[1]+`"`)
	}
	return cloudai.Response{Content: `{"coverage":"partial","source_ids":[` + strings.Join(ids, ",") + `]}`}, nil
}

// TestMaterialKinds 是 T55 的验收：资料库有教材、讲义、考纲、经验总结四种；经验总结上传即视为已获授权；
// 生成官方题答案时各类资料都能取到段落并标明类型；考生填书名只匹配教材讲义；考生看到的出处里没有考纲、经验总结。
func TestMaterialKinds(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	s.ConfigureIndexes(IndexDeps{Embedding: ai.NewEmbedder(config.TextbookConfig{EmbeddingProvider: "mock", EmbeddingDimensions: 256, EmbeddingModel: "mock"}),
		Vector: &fakeVector{}, Params: params.New(dbq.New(db)), Queue: &importQueue{fail: true}, VectorConfigured: true})
	client := &selectAllClient{}
	s.ConfigureEvidence(ai.NewEngine(ai.Config{Client: client}), flags.New(dbq.New(db)))

	// 考纲照常要授权；经验总结不勾授权、不写说明也能建，自动记为已获授权。
	if _, err := s.Create(ctx, 1, "syllabus-unauthorized", rules.TextbookMetadata{Title: "北师大808考纲", Author: "北京师范大学", Kind: rules.MaterialSyllabus, Discipline: "文学"},
		rules.TextbookEditionInput{Edition: "2026"}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("考纲没授权：%v", err)
	}
	if _, err := s.Create(ctx, 1, "unknown-kind-key", rules.TextbookMetadata{Title: "某资料", Author: "某人", Kind: "notes", Discipline: "文学"}, edition()); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("不认识的类型：%v", err)
	}
	build := func(key string, m rules.TextbookMetadata, e rules.TextbookEditionInput, text string) dbq.TextbookEdition {
		t.Helper()
		b, err := s.Create(ctx, 1, key, m, e)
		if err != nil {
			t.Fatal(err)
		}
		ed := b.Editions[0]
		r, err := s.NewRevision(ctx, b.ID, ed.ID, 1, key+"-rev")
		if err != nil {
			t.Fatal(err)
		}
		r, err = s.SaveRevision(ctx, b.ID, ed.ID, r.ID, 1, r.Version, []rules.TextbookSection{{Heading: m.Title, Text: strings.Repeat(text, 60)}}, true)
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
		out, err := dbq.New(db).GetTextbookEditionByID(ctx, ed.ID)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	book := build("material-textbook", metadata(), edition(), "意境是情景交融的艺术境界。")
	syllabus := build("material-syllabus", rules.TextbookMetadata{Title: "北师大808考纲", Author: "北京师范大学", Kind: rules.MaterialSyllabus, Discipline: "文学"},
		rules.TextbookEditionInput{Edition: "2026", Authorized: true, LicenseNote: "公开考纲"}, "考查意境的内涵与特征。")
	exp := build("material-experience", rules.TextbookMetadata{Title: "808 上岸经验", Author: "北师大808上岸学长", Kind: rules.MaterialExperience, Discipline: "文学"},
		rules.TextbookEditionInput{Edition: "2025"}, "名词解释先下定义再举作品为例。")
	if !exp.Authorized || exp.LicenseNote != rules.ExperienceLicenseNote {
		t.Fatalf("经验总结自动记为已获授权：%v %q", exp.Authorized, exp.LicenseNote)
	}

	// 考生填书名：考纲、经验总结匹配不到；项目明确关联时都能用。
	if m, err := s.MatchEditions(ctx, []string{"北师大808考纲"}, "文学", nil); err != nil || len(m) != 0 {
		t.Fatalf("书名匹配不到考纲：%+v %v", m, err)
	}
	if m, err := s.MatchEditions(ctx, []string{"文学理论教程"}, "文学", nil); err != nil || len(m) != 1 {
		t.Fatalf("书名匹配教材：%+v %v", m, err)
	}

	in, err := s.PrepareAnswer(ctx, 0, ai.GenerateAnswerIn{Subject: "中国语言文学基础", QType: "term", Stem: "意境", Score: 5, EditionIDs: []uint64{book.ID, syllabus.ID, exp.ID}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range in.Evidence {
		got[f.Material]++
	}
	if got[rules.MaterialTextbook] == 0 || got[rules.MaterialSyllabus] == 0 || got[rules.MaterialExperience] == 0 || got[rules.MaterialSyllabus] > 3 || got[rules.MaterialExperience] > 3 || len(in.Evidence) > 16 {
		t.Fatalf("三类资料都取到、考纲与经验总结各不超过 3 段：%+v", got)
	}
	if in.Basis != nil {
		t.Fatalf("用上了教材不该有原因：%+v", in.Basis)
	}
	last := client.prompts[len(client.prompts)-1]
	for _, label := range []string{"类型=教材", "类型=考纲", "类型=经验总结"} {
		if !strings.Contains(last, label) {
			t.Errorf("提示词里要有 %s", label)
		}
	}

	// 只关联了考纲、经验总结：照样用，但说明没有教材。
	in, err = s.PrepareAnswer(ctx, 0, ai.GenerateAnswerIn{Subject: "中国语言文学基础", QType: "term", Stem: "意境", Score: 5, EditionIDs: []uint64{syllabus.ID, exp.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if in.Basis == nil || in.Basis.Reason != ai.BasisNoTextbook || len(in.Evidence) == 0 || slices.ContainsFunc(in.Evidence, func(f ai.EvidenceFragment) bool { return f.IsKnowledge() }) {
		t.Fatalf("只有考纲与经验总结：%+v %d", in.Basis, len(in.Evidence))
	}

	// 考生看到的出处里去掉考纲、经验总结。
	src := func(material string) ai.AnswerSource {
		return ai.AnswerSource{EvidenceFragment: ai.EvidenceFragment{Kind: "official_textbook", Material: material, EditionID: book.ID, BookID: book.BookID}, Quote: "q", Claim: "c"}
	}
	visible, err := VisibleSnapshot(ctx, dbq.New(db), &ai.AnswerSnapshot{Sources: []ai.AnswerSource{src(""), src(rules.MaterialTextbook), src(rules.MaterialSyllabus), src(rules.MaterialExperience)}})
	if err != nil || len(visible.Sources) != 2 {
		t.Fatalf("考生看到的出处：%+v %v", visible, err)
	}
}
