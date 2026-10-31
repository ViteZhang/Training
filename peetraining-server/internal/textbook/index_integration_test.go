package textbook

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/vector"
	"peetraining-server/internal/config"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// fakeVector 只测试故障/状态，不替代真实 Qdrant 协议与索引验收。
type fakeVector struct {
	mu       sync.Mutex
	points   map[string]map[uint64]vector.Point
	failAt   int
	writes   int
	onVerify func()
}

func (v *fakeVector) Ensure(_ context.Context, name string, _ int) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.points == nil {
		v.points = map[string]map[uint64]vector.Point{}
	}
	if v.points[name] == nil {
		v.points[name] = map[uint64]vector.Point{}
	}
	return nil
}
func (v *fakeVector) Upsert(_ context.Context, name string, s vector.Scope, id uint64, f []float64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.writes++
	if v.failAt == v.writes {
		return errors.New("injected vector failure")
	}
	v.points[name][id] = vector.Point{ID: id, Vector: f}
	return nil
}
func (v *fakeVector) Verify(_ context.Context, name string, _ int, _ vector.Scope, count int) error {
	v.mu.Lock()
	n := len(v.points[name])
	cb := v.onVerify
	v.mu.Unlock()
	if cb != nil {
		cb()
	}
	if n != count {
		return errors.New("incomplete")
	}
	return nil
}
func (v *fakeVector) Search(_ context.Context, name string, _ vector.Scope, _ []float64, limit int) ([]vector.Hit, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := []vector.Hit{}
	for i := 1; i <= len(v.points[name]) && i <= limit; i++ {
		out = append(out, vector.Hit{ID: uint64(i), Score: .9})
	}
	return out, nil
}
func indexedRevision(t *testing.T, s *Service) (Book, Revision, dbq.TextbookIndexGeneration, *fakeVector) {
	t.Helper()
	b := mustBook(t, s, "indexed-book-key")
	e := b.Editions[0]
	r, err := s.NewRevision(context.Background(), b.ID, e.ID, 1, "indexed-revision")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.SaveRevision(context.Background(), b.ID, e.ID, r.ID, 1, r.Version, []rules.TextbookSection{{Heading: "文学的性质", Text: strings.Repeat("文学具有审美意识形态性质。", 100)}}, true)
	if err != nil {
		t.Fatal(err)
	}
	v := &fakeVector{}
	s.ConfigureIndexes(IndexDeps{Embedding: ai.NewEmbedder(config.TextbookConfig{EmbeddingProvider: "mock", EmbeddingDimensions: 256, EmbeddingModel: "mock"}), Vector: v, Params: params.New(dbq.New(s.db)), Queue: &importQueue{fail: true}, VectorConfigured: true})
	g, err := s.BuildIndex(context.Background(), b.ID, e.ID, r.ID, 1, r.Version, "index-request-key")
	if err != nil {
		t.Fatal(err)
	}
	return b, r, g, v
}
func TestIndexCheckpointSearchAndAtomicPublish(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	b, r, g, v := indexedRevision(t, s)
	e := b.Editions[0]
	again, err := s.BuildIndex(ctx, b.ID, e.ID, r.ID, 1, r.Version, "index-request-key")
	if err != nil || again.ID != g.ID {
		t.Fatal("幂等构建", err)
	}
	v.failAt = 2
	if s.ProcessIndex(ctx, g.ID) == nil {
		t.Fatal("注入失败应停止")
	}
	failed, _ := dbq.New(s.db).GetTextbookIndex(ctx, g.ID)
	if failed.Status != "failed" {
		t.Fatal(failed)
	}
	retry, err := s.RetryIndex(ctx, b.ID, e.ID, r.ID, g.ID, "retry-index-key")
	if err != nil || retry.Attempt != 1 {
		t.Fatal(retry, err)
	}
	replay, err := s.RetryIndex(ctx, b.ID, e.ID, r.ID, g.ID, "retry-index-key")
	if err != nil || replay.Attempt != 1 {
		t.Fatal("重复重试", replay, err)
	}
	if err = s.ProcessIndex(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	ready, _ := dbq.New(s.db).GetTextbookIndex(ctx, g.ID)
	if ready.Status != "ready" || ready.ChunkCount < 2 || v.writes != int(ready.ChunkCount)+1 {
		t.Fatal("检查点未复用", ready, v.writes)
	}
	result, err := s.SearchRevision(ctx, b.ID, e.ID, r.ID, g.ID, "文学的审美意识形态性质")
	if err != nil || len(result.Hits) == 0 {
		t.Fatal(result, err)
	}
	foundFT := false
	for _, h := range result.Hits {
		if h.FulltextRank > 0 {
			foundFT = true
		}
		if h.Text == "" {
			t.Fatal(h)
		}
	}
	if !foundFT {
		t.Fatal("中文全文未召回")
	}
	in := PublishInput{"publish-request", r.Version, e.Version, g.ID, "测试发布"}
	published, err := s.Publish(ctx, b.ID, e.ID, r.ID, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	if published.Editions[0].Status != "published" || uint64(published.Editions[0].CurrentGenerationID.Int64) != g.ID {
		t.Fatal(published)
	}
	if _, err = s.Publish(ctx, b.ID, e.ID, r.ID, 1, in); err != nil {
		t.Fatal("重放发布", err)
	}
	if _, err = s.SaveRevision(ctx, b.ID, e.ID, r.ID, 1, r.Version, r.Sections, true); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("已发布正文被覆盖", err)
	}
	if _, err = s.SetDefault(ctx, b.ID, e.ID, 1, published.Version, published.Editions[0].Version, "default-request", true); err != nil {
		t.Fatal(err)
	}
	// 新草稿及索引失败不得改变已发布指针。
	draft, err := s.NewRevision(ctx, b.ID, e.ID, 1, "new-draft-after-publish")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Publish(ctx, b.ID, e.ID, draft.ID, 1, PublishInput{"wrong-draft-key", draft.Version, published.Editions[0].Version, g.ID, "错误代次"}); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal(err)
	}
	current, _ := s.Get(ctx, b.ID)
	if uint64(current.Editions[0].CurrentRevisionID.Int64) != r.ID {
		t.Fatal("新草稿影响线上")
	}
}
func TestIndexEditAndAuthorizationRace(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	b, r, g, v := indexedRevision(t, s)
	e := b.Editions[0]
	if err := s.ProcessIndex(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	// 在向量服务检查与发布事务之间修改原文，发布必须冲突。
	once := sync.Once{}
	v.onVerify = func() {
		once.Do(func() {
			_, err := s.SaveRevision(ctx, b.ID, e.ID, r.ID, 1, r.Version, []rules.TextbookSection{{Text: "修改后的正文"}}, true)
			if err != nil {
				t.Error(err)
			}
		})
	}
	_, err := s.Publish(ctx, b.ID, e.ID, r.ID, 1, PublishInput{"racing-publish-key", r.Version, e.Version, g.ID, "竞态测试"})
	if !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal(err)
	}
	if _, err = s.SearchRevision(ctx, b.ID, e.ID, r.ID, g.ID, "文学"); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("旧索引未失效", err)
	}
	current, _ := s.Get(ctx, b.ID)
	if current.Editions[0].Status == "published" {
		t.Fatal("发布了旧原文")
	}
}
func TestIndexPublishAuthorizationAndConcurrentVersion(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	b, r, g, v := indexedRevision(t, s)
	e := b.Editions[0]
	if err := s.ProcessIndex(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	once := sync.Once{}
	v.onVerify = func() {
		once.Do(func() {
			_, err := s.Availability(ctx, b.ID, e.ID, 1, AvailabilityInput{Key: "revoke-key-1", Version: e.Version, Authorized: false, LicenseNote: "授权撤销", Summary: "撤销", ExcerptsEnabled: false})
			if err != nil {
				t.Error(err)
			}
		})
	}
	if _, err := s.Publish(ctx, b.ID, e.ID, r.ID, 1, PublishInput{"publish-revoked-key", r.Version, e.Version, g.ID, "发布"}); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("授权并发变化未拒绝", err)
	}
	current, _ := s.Get(ctx, b.ID)
	if current.Editions[0].Authorized || current.Editions[0].ExcerptsEnabled {
		t.Fatal("授权未保存")
	}
}

func TestMatchingIsUnambiguousAndSnapshotAvailability(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	b, r, g, _ := indexedRevision(t, s)
	e := b.Editions[0]
	if err := s.ProcessIndex(ctx, g.ID); err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(ctx, b.ID, e.ID, r.ID, 1, PublishInput{"first-publish-key", r.Version, e.Version, g.ID, "首版"})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := s.MatchEditions(ctx, []string{"文论教程"}, "文学", nil)
	if err != nil || len(matches) != 1 || matches[0].ID != e.ID {
		t.Fatal(matches, err)
	}
	// 第二版未设默认时不自动选新旧版；用数据库准备另一完整发布版次测试目录规则。
	next := edition()
	next.Edition = "第六版"
	updated, err := s.AddEdition(ctx, b.ID, 1, "another-edition", next)
	if err != nil {
		t.Fatal(err)
	}
	var nextID uint64
	for _, item := range updated.Editions {
		if item.Edition == next.Edition {
			nextID = item.ID
		}
	}
	if _, err = db.Exec("UPDATE textbook_editions SET status='published',current_revision_id=?,current_generation_id=? WHERE id=?", r.ID, g.ID, nextID); err != nil {
		t.Fatal(err)
	}
	matches, err = s.MatchEditions(ctx, []string{"文学理论教程"}, "文学", nil)
	if err != nil || len(matches) != 0 {
		t.Fatal("未设默认不应猜版次", matches, err)
	}
	if _, err = s.SetDefault(ctx, b.ID, e.ID, 1, updated.Version, published.Editions[0].Version, "set-default-test", true); err != nil {
		t.Fatal(err)
	}
	matches, _ = s.MatchEditions(ctx, []string{"文学理论教程"}, "文学", nil)
	if len(matches) != 1 || matches[0].ID != e.ID {
		t.Fatal(matches)
	}
	matches, _ = s.MatchEditions(ctx, []string{"文学理论教程第六版"}, "文学", nil)
	if len(matches) != 1 || matches[0].ID != nextID {
		t.Fatal(matches)
	}
	chunk, err := dbq.New(db).GetTextbookChunk(ctx, dbq.GetTextbookChunkParams{GenerationID: g.ID, Ordinal: 1})
	if err != nil {
		t.Fatal(err)
	}
	source := ai.AnswerSource{EvidenceFragment: ai.EvidenceFragment{Kind: "official_textbook", BookID: b.ID, EditionID: e.ID, RevisionID: r.ID, GenerationID: g.ID, Ordinal: 1, SectionNo: chunk.SectionNo, StartRune: chunk.StartRune, EndRune: chunk.EndRune, TextHash: chunk.TextHash}, Quote: "文学具有审美意识形态性质。", Claim: "文学具有审美意识形态性质。"}
	snap := &ai.AnswerSnapshot{Sources: []ai.AnswerSource{source}}
	if err = store.WithTx(ctx, db, func(q *dbq.Queries) error { return s.ValidateAnswerSources(ctx, q, snap.Sources) }); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Get(ctx, b.ID)
	var currentE dbq.TextbookEdition
	for _, item := range current.Editions {
		if item.ID == e.ID {
			currentE = item.TextbookEdition
		}
	}
	if _, err = s.Availability(ctx, b.ID, e.ID, 1, AvailabilityInput{Key: "withdraw-source-key", Version: currentE.Version, Withdrawn: true, Authorized: true, ExcerptsEnabled: true, LicenseNote: "授权仍允许历史展示", Summary: "运营下架"}); err != nil {
		t.Fatal(err)
	}
	if err = store.WithTx(ctx, db, func(q *dbq.Queries) error { return s.ValidateAnswerSources(ctx, q, snap.Sources) }); err == nil {
		t.Fatal("下架来源被用于新答案")
	}
	visible, err := VisibleSnapshot(ctx, dbq.New(db), snap)
	if err != nil || visible.Sources[0].Quote == "" {
		t.Fatal("运营下架不应隐藏历史摘录", err)
	}
	if _, err = db.Exec("UPDATE textbook_editions SET excerpts_enabled=FALSE WHERE id=?", e.ID); err != nil {
		t.Fatal(err)
	}
	visible, err = VisibleSnapshot(ctx, dbq.New(db), snap)
	if err != nil || visible.Sources[0].Quote != "" || !visible.Sources[0].Unavailable {
		t.Fatal("撤销摘录展示未生效", visible, err)
	}
}
