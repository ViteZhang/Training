package textbook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hibiken/asynq"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/vector"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

type Embedding interface {
	Profile() ai.EmbeddingProfile
	Ready() bool
	Embed(context.Context, []string) ([][]float64, error)
}
type IndexDeps struct {
	Embedding        Embedding
	Vector           vector.Store
	Params           *params.Store
	Queue            Enqueuer
	Production       bool
	VectorConfigured bool
}

func (s *Service) ConfigureIndexes(d IndexDeps) { s.indexes = d }
func (s *Service) RetrievalParameters(ctx context.Context) (rules.TextbookRetrievalParams, error) {
	var p rules.TextbookRetrievalParams
	if s.indexes.Params == nil {
		return p, errors.New("教材检索尚未配置")
	}
	err := s.indexes.Params.Get(ctx, "textbook_retrieval", &p)
	if err == nil {
		err = rules.ValidateTextbookRetrieval(p)
	}
	return p, err
}
func indexScope(g dbq.TextbookIndexGeneration) vector.Scope {
	return vector.Scope{Book: strconv.FormatUint(g.BookID, 10), Edition: strconv.FormatUint(g.EditionID, 10), Revision: strconv.FormatUint(g.RevisionID, 10), Generation: strconv.FormatUint(g.ID, 10)}
}
func (s *Service) ListIndexes(ctx context.Context, book, edition, revision uint64) ([]dbq.ListTextbookIndexesRow, error) {
	if err := s.checkRevision(ctx, book, edition, revision); err != nil {
		return nil, err
	}
	return dbq.New(s.db).ListTextbookIndexes(ctx, revision)
}
func (s *Service) BuildIndex(ctx context.Context, book, edition, revision, actor, version uint64, key string) (dbq.TextbookIndexGeneration, error) {
	var out dbq.TextbookIndexGeneration
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return out, err
	}
	if s.indexes.Embedding == nil || !s.indexes.Embedding.Ready() || s.indexes.Vector == nil || !s.indexes.VectorConfigured {
		return out, apperr.New(apperr.BadRequest, "请先配置教材嵌入模型和向量服务")
	}
	p, err := s.RetrievalParameters(ctx)
	if err != nil {
		return out, err
	}
	profile := s.indexes.Embedding.Profile()
	_, profileJSON, _ := rules.TextbookHash(profile)
	_, splitJSON, _ := rules.TextbookHash(p)
	hash, _, _ := rules.TextbookHash(struct {
		Version uint64
		Profile string
		Split   rules.TextbookRetrievalParams
	}{version, profile.ID(), p})
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book}); err != nil {
			return err
		}
		r, err := q.LockTextbookRevision(ctx, dbq.LockTextbookRevisionParams{ID: revision, EditionID: edition})
		if err != nil {
			return err
		}
		old, err := q.GetTextbookIndexRequest(ctx, dbq.GetTextbookIndexRequestParams{RevisionID: revision, CreatedBy: actor, RequestKey: key})
		if err == nil {
			if old.RequestHash != hash {
				return requestConflict()
			}
			out = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if r.Version != version {
			return versionConflict()
		}
		if r.Status != "reviewed" && r.Status != "published" || !r.ReviewedAt.Valid {
			return apperr.New(apperr.BadRequest, "请先完成并确认原文校对")
		}
		now := s.now().UTC()
		id, err := q.CreateTextbookIndex(ctx, dbq.CreateTextbookIndexParams{BookID: book, EditionID: edition, RevisionID: revision, SourceVersion: version, ContentHash: r.ContentHash, Profile: profileJSON, ProfileHash: profile.ID(), SplitConfig: splitJSON, CreatedBy: actor, RequestKey: key, RequestHash: hash, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		out, err = q.GetTextbookIndex(ctx, uint64(id))
		return err
	})
	if err != nil {
		return out, mapError(err)
	}
	// 数据库任务先提交；投递失败由恢复调度补投，不丢失任务。
	_ = s.enqueueIndex(ctx, out.ID, out.Attempt)
	return out, nil
}
func (s *Service) enqueueIndex(ctx context.Context, id uint64, attempt uint32) error {
	if s.indexes.Queue == nil {
		return errors.New("教材索引队列未配置")
	}
	task, err := jobs.NewTextbookIndexTask(id, attempt)
	if err != nil {
		return err
	}
	_, err = s.indexes.Queue.EnqueueContext(ctx, task)
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}
func (s *Service) RecoverIndexes(ctx context.Context) error {
	rows, err := dbq.New(s.db).RecoverableTextbookIndexes(ctx, s.now().UTC().Add(-5*time.Minute))
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err = s.enqueueIndex(ctx, row.ID, row.Attempt); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) lockIndex(ctx context.Context, id uint64) (func(), error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("textbook-index-%d", id)
	var got sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", name).Scan(&got); err != nil || !got.Valid || got.Int64 != 1 {
		_ = conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, apperr.New(apperr.Conflict, "索引正在构建")
	}
	return func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, "SELECT RELEASE_LOCK(?)", name)
		_ = conn.Close()
	}, nil
}
func (s *Service) ProcessIndex(ctx context.Context, id uint64) (retErr error) {
	release, err := s.lockIndex(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	q := dbq.New(s.db)
	g, err := q.GetTextbookIndex(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if g.Status == "ready" || g.Status == "stale" {
		return nil
	}
	defer func() {
		if retErr != nil {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = q.SetTextbookIndexStatus(c, dbq.SetTextbookIndexStatusParams{Status: "failed", FailureMessage: "索引构建失败，请检查嵌入及向量服务配置后重新构建", UpdatedAt: s.now().UTC(), ID: id})
		}
	}()
	if s.indexes.Embedding == nil || !s.indexes.Embedding.Ready() || s.indexes.Vector == nil {
		return errors.New("索引依赖未配置")
	}
	if g.ProfileHash != s.indexes.Embedding.Profile().ID() {
		return errors.New("嵌入配置已变更，请构建新代次")
	}
	r, err := q.GetTextbookRevision(ctx, dbq.GetTextbookRevisionParams{ID: g.RevisionID, EditionID: g.EditionID})
	if err != nil {
		return err
	}
	if r.ContentHash != g.ContentHash || !r.ReviewedAt.Valid {
		return q.SetTextbookIndexStatus(ctx, dbq.SetTextbookIndexStatusParams{Status: "stale", FailureMessage: "原文已变更，请重新校对并构建", UpdatedAt: s.now().UTC(), ID: id})
	}
	var p rules.TextbookRetrievalParams
	if json.Unmarshal(g.SplitConfig, &p) != nil {
		return errors.New("切分配置损坏")
	}
	var sections []rules.TextbookSection
	if json.Unmarshal(r.Sections, &sections) != nil {
		return errors.New("原文损坏")
	}
	chunks, err := rules.TextbookChunks(sections, p)
	if err != nil {
		return err
	}
	collection := fmt.Sprintf("tb_%s_%d", g.ProfileHash[:16], g.ID)
	if err = q.StartTextbookIndex(ctx, dbq.StartTextbookIndexParams{CollectionName: collection, UpdatedAt: s.now().UTC(), ID: id}); err != nil {
		return err
	}
	if err = s.indexes.Vector.Ensure(ctx, collection, s.indexes.Embedding.Profile().Dimensions); err != nil {
		return err
	}
	for _, c := range chunks {
		if err = q.InsertTextbookChunk(ctx, dbq.InsertTextbookChunkParams{GenerationID: id, Ordinal: uint32(c.Ordinal), SectionNo: uint32(c.Section), StartRune: uint32(c.Start), EndRune: uint32(c.End), Heading: c.Heading, Body: c.Text, TextHash: c.Hash}); err != nil {
			return err
		}
	}
	if err = q.SetTextbookIndexCount(ctx, dbq.SetTextbookIndexCountParams{ChunkCount: uint32(len(chunks)), UpdatedAt: s.now().UTC(), ID: id}); err != nil {
		return err
	}
	for _, c := range chunks {
		row, err := q.GetTextbookChunk(ctx, dbq.GetTextbookChunkParams{GenerationID: id, Ordinal: uint32(c.Ordinal)})
		if err != nil {
			return err
		}
		if row.TextHash != c.Hash {
			return errors.New("索引原文校验不一致")
		}
		if row.VectorReady {
			continue
		}
		embeddings, err := s.indexes.Embedding.Embed(ctx, []string{c.Text})
		if err != nil {
			return err
		}
		if len(embeddings) != 1 {
			return errors.New("嵌入数量异常")
		}
		if err = s.indexes.Vector.Upsert(ctx, collection, indexScope(g), uint64(c.Ordinal), embeddings[0]); err != nil {
			return err
		}
		if err = q.MarkTextbookChunkVector(ctx, dbq.MarkTextbookChunkVectorParams{GenerationID: id, Ordinal: uint32(c.Ordinal)}); err != nil {
			return err
		}
		if err = q.HeartbeatTextbookIndex(ctx, dbq.HeartbeatTextbookIndexParams{UpdatedAt: s.now().UTC(), ID: id}); err != nil {
			return err
		}
	}
	if err = s.indexes.Vector.Verify(ctx, collection, s.indexes.Embedding.Profile().Dimensions, indexScope(g), len(chunks)); err != nil {
		return err
	}
	return store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: g.EditionID, BookID: g.BookID}); err != nil {
			return err
		}
		current, err := q.LockTextbookRevision(ctx, dbq.LockTextbookRevisionParams{ID: g.RevisionID, EditionID: g.EditionID})
		if err != nil {
			return err
		}
		state := "ready"
		if current.ContentHash != g.ContentHash || !current.ReviewedAt.Valid {
			state = "stale"
		}
		if err = q.SetTextbookIndexStatus(ctx, dbq.SetTextbookIndexStatusParams{Status: state, UpdatedAt: s.now().UTC(), ID: id}); err != nil {
			return err
		}
		if state == "ready" {
			return q.SetRevisionIndexStatus(ctx, dbq.SetRevisionIndexStatusParams{IndexStatus: "ready", ReadyGenerationID: sql.NullInt64{Int64: int64(id), Valid: true}, ID: g.RevisionID})
		}
		return nil
	})
}

// RetrievalSettings 不包含地址、凭证或用户内容。
func (s *Service) RetrievalSettings(p rules.TextbookRetrievalParams) any {
	profile := ai.EmbeddingProfile{}
	if s.indexes.Embedding != nil {
		profile = s.indexes.Embedding.Profile()
	}
	return struct {
		EmbeddingEnabled    bool                          `json:"embedding_enabled"`
		EmbeddingProvider   string                        `json:"embedding_provider"`
		EmbeddingModel      string                        `json:"embedding_model"`
		EmbeddingDimensions int                           `json:"embedding_dimensions"`
		ProfileID           string                        `json:"profile_id"`
		VectorConfigured    bool                          `json:"vector_configured"`
		Production          bool                          `json:"production"`
		Parameters          rules.TextbookRetrievalParams `json:"parameters"`
	}{s.indexes.Embedding != nil && s.indexes.Embedding.Ready(), profile.Provider, profile.Model, profile.Dimensions, profile.ID(), s.indexes.VectorConfigured, s.indexes.Production, p}
}

func (s *Service) RetryIndex(ctx context.Context, book, edition, revision, id uint64, key string) (dbq.TextbookIndexGeneration, error) {
	var out dbq.TextbookIndexGeneration
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return out, err
	}
	if err := s.checkRevision(ctx, book, edition, revision); err != nil {
		return out, err
	}
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		_, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book})
		if err != nil {
			return err
		}
		r, err := q.LockTextbookRevision(ctx, dbq.LockTextbookRevisionParams{ID: revision, EditionID: edition})
		if err != nil {
			return err
		}
		g, err := q.LockTextbookIndex(ctx, id)
		if err != nil {
			return err
		}
		if g.BookID != book || g.EditionID != edition || g.RevisionID != revision {
			return apperr.NotFoundErr()
		}
		out = g
		if g.RetryKey == key || g.Status == "queued" || g.Status == "processing" || g.Status == "ready" {
			return nil
		}
		if g.Status != "failed" || g.Attempt >= 5 {
			return apperr.New(apperr.BadRequest, "本代次不能继续，请检查配置并创建新代次")
		}
		if r.ContentHash != g.ContentHash || !r.ReviewedAt.Valid || s.indexes.Embedding == nil || g.ProfileHash != s.indexes.Embedding.Profile().ID() {
			return apperr.New(apperr.Conflict, "原文或模型配置已变化，请构建新代次")
		}
		if err = q.RetryTextbookIndex(ctx, dbq.RetryTextbookIndexParams{RetryKey: key, UpdatedAt: s.now().UTC(), ID: id}); err != nil {
			return err
		}
		out, err = q.GetTextbookIndex(ctx, id)
		return err
	})
	if err != nil {
		return out, mapError(err)
	}
	_ = s.enqueueIndex(ctx, id, out.Attempt)
	return out, nil
}
