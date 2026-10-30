package textbook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

type PublishInput struct {
	Key                                         string
	RevisionVersion, EditionVersion, Generation uint64
	Summary                                     string
}

func (s *Service) verifyIndex(ctx context.Context, g dbq.TextbookIndexGeneration) error {
	if g.Status != "ready" || g.ChunkCount == 0 {
		return apperr.New(apperr.BadRequest, "全文及向量索引尚未就绪")
	}
	var profile ai.EmbeddingProfile
	if json.Unmarshal(g.Profile, &profile) != nil || profile.ID() != g.ProfileHash {
		return errors.New("索引配置校验失败")
	}
	if s.indexes.Production && profile.Provider != "openai" {
		return apperr.New(apperr.BadRequest, "生产环境不能发布模拟索引")
	}
	if s.indexes.Embedding == nil || s.indexes.Embedding.Profile().ID() != g.ProfileHash {
		return apperr.New(apperr.BadRequest, "嵌入配置已变化，请重建索引")
	}
	count, err := dbq.New(s.db).CountTextbookChunks(ctx, g.ID)
	if err != nil {
		return err
	}
	if count.Total != int64(g.ChunkCount) || count.Completed != count.Total {
		return apperr.New(apperr.BadRequest, "索引片段尚未完整")
	}
	if s.indexes.Vector == nil {
		return apperr.New(apperr.BadRequest, "向量服务未配置")
	}
	if err = s.indexes.Vector.Verify(ctx, g.CollectionName, profile.Dimensions, indexScope(g), int(g.ChunkCount)); err != nil {
		return apperr.New(apperr.BadRequest, "向量服务不可用或索引校验未通过")
	}
	return nil
}
func validLicense(e dbq.TextbookEdition, now time.Time) bool {
	return e.Authorized && strings.TrimSpace(e.LicenseNote) != "" && (!e.LicenseExpiresAt.Valid || e.LicenseExpiresAt.Time.After(now))
}
func (s *Service) Publish(ctx context.Context, book, edition, revision, actor uint64, in PublishInput) (Book, error) {
	if err := rulesPublicationInput(in.Key, in.Summary); err != nil {
		return Book{}, err
	}
	hash, _, _ := publicationHash("publish", edition, revision, in)
	// 网络重试已成功的请求直接重放，不因之后服务故障而报告发布失败。
	if old, err := dbq.New(s.db).GetTextbookPublicationRequest(ctx, dbq.GetTextbookPublicationRequestParams{BookID: book, CreatedBy: actor, RequestKey: in.Key}); err == nil {
		if old.RequestHash != hash {
			return Book{}, requestConflict()
		}
		return s.Get(ctx, book)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Book{}, err
	}
	if err := s.checkRevision(ctx, book, edition, revision); err != nil {
		return Book{}, err
	}
	g, err := dbq.New(s.db).GetTextbookIndex(ctx, in.Generation)
	if err != nil {
		return Book{}, mapError(err)
	}
	if g.BookID != book || g.EditionID != edition || g.RevisionID != revision {
		return Book{}, apperr.NotFoundErr()
	}
	if err = s.verifyIndex(ctx, g); err != nil {
		return Book{}, err
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbook(ctx, book); err != nil {
			return err
		}
		e, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book})
		if err != nil {
			return err
		}
		r, err := q.LockTextbookRevision(ctx, dbq.LockTextbookRevisionParams{ID: revision, EditionID: edition})
		if err != nil {
			return err
		}
		if replay, err := s.publicationReplay(ctx, q, book, actor, in.Key, hash); err != nil || replay {
			return err
		}
		if e.Version != in.EditionVersion || r.Version != in.RevisionVersion {
			return versionConflict()
		}
		if !validLicense(e, s.now().UTC()) {
			return apperr.New(apperr.BadRequest, "授权已停用或过期，请先维护授权")
		}
		// 原文hash、校对、代次在行锁下检查；并发编辑不会发布旧正文。
		if (r.Status != "reviewed" && r.Status != "published") || !r.ReviewedAt.Valid || r.ContentHash != g.ContentHash || r.IndexStatus != "ready" || !r.ReadyGenerationID.Valid || uint64(r.ReadyGenerationID.Int64) != g.ID {
			return apperr.New(apperr.Conflict, "校对或索引已变化，请重新检查")
		}
		current, err := q.LockTextbookIndex(ctx, g.ID)
		if err != nil {
			return err
		}
		if current.Status != "ready" {
			return versionConflict()
		}
		if err = q.PublishTextbookEdition(ctx, dbq.PublishTextbookEditionParams{CurrentRevisionID: sql.NullInt64{Int64: int64(revision), Valid: true}, CurrentGenerationID: sql.NullInt64{Int64: int64(g.ID), Valid: true}, ID: edition}); err != nil {
			return err
		}
		if err = q.PublishTextbookRevision(ctx, dbq.PublishTextbookRevisionParams{UpdatedAt: s.now().UTC(), ID: revision}); err != nil {
			return err
		}
		return s.publicationEvent(ctx, q, book, edition, revision, g.ID, actor, in.Key, hash, "publish", in.Summary)
	})
	if err != nil {
		return Book{}, mapError(err)
	}
	return s.Get(ctx, book)
}
func rulesPublicationInput(key, summary string) error {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return err
	}
	if strings.TrimSpace(summary) == "" || utf8.RuneCountInString(summary) > 500 {
		return apperr.New(apperr.BadRequest, "请填写 1–500 字变更说明")
	}
	return nil
}
func publicationHash(action string, edition, revision uint64, input any) (string, []byte, error) {
	h, j, e := rules.TextbookHash(struct {
		Action            string
		Edition, Revision uint64
		Input             any
	}{action, edition, revision, input})
	return h, j, e
}
func (s *Service) publicationReplay(ctx context.Context, q *dbq.Queries, book, actor uint64, key, hash string) (bool, error) {
	old, err := q.GetTextbookPublicationRequest(ctx, dbq.GetTextbookPublicationRequestParams{BookID: book, CreatedBy: actor, RequestKey: key})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if old.RequestHash != hash {
		return false, requestConflict()
	}
	return true, nil
}
func (s *Service) publicationEvent(ctx context.Context, q *dbq.Queries, book, edition, revision, generation, actor uint64, key, hash, action, summary string) error {
	r, g := sql.NullInt64{}, sql.NullInt64{}
	if revision > 0 {
		r = sql.NullInt64{Int64: int64(revision), Valid: true}
	}
	if generation > 0 {
		g = sql.NullInt64{Int64: int64(generation), Valid: true}
	}
	return q.CreateTextbookPublicationEvent(ctx, dbq.CreateTextbookPublicationEventParams{BookID: book, EditionID: edition, RevisionID: r, GenerationID: g, Action: action, Summary: summary, CreatedBy: actor, RequestKey: key, RequestHash: hash, CreatedAt: s.now().UTC()})
}

type AvailabilityInput struct {
	Key                                    string
	Version                                uint64
	Withdrawn, Authorized, ExcerptsEnabled bool
	LicenseNote, Summary                   string
	ExpiresAt                              *time.Time
}

func (s *Service) Availability(ctx context.Context, book, edition, actor uint64, in AvailabilityInput) (Book, error) {
	if err := rulesPublicationInput(in.Key, in.Summary); err != nil {
		return Book{}, err
	}
	if strings.TrimSpace(in.LicenseNote) == "" || utf8.RuneCountInString(in.LicenseNote) > 500 {
		return Book{}, apperr.New(apperr.BadRequest, "请填写授权说明")
	}
	hash, _, _ := publicationHash("availability", edition, 0, in)
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbook(ctx, book); err != nil {
			return err
		}
		e, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book})
		if err != nil {
			return err
		}
		if replay, err := s.publicationReplay(ctx, q, book, actor, in.Key, hash); err != nil || replay {
			return err
		}
		if e.Version != in.Version {
			return versionConflict()
		}
		status := e.Status
		// 恢复运营下架必须再次经过发布校验；恢复授权不隐式恢复发布。
		if in.Withdrawn || !in.Authorized {
			status = "withdrawn"
		}
		expires := sql.NullTime{}
		if in.ExpiresAt != nil {
			expires = sql.NullTime{Time: in.ExpiresAt.UTC(), Valid: true}
		}
		if err = q.SetTextbookEditionAvailability(ctx, dbq.SetTextbookEditionAvailabilityParams{Status: status, Authorized: in.Authorized, LicenseNote: strings.TrimSpace(in.LicenseNote), LicenseExpiresAt: expires, ExcerptsEnabled: in.ExcerptsEnabled, ID: edition}); err != nil {
			return err
		}
		return s.publicationEvent(ctx, q, book, edition, 0, 0, actor, in.Key, hash, "availability", in.Summary)
	})
	if err != nil {
		return Book{}, mapError(err)
	}
	return s.Get(ctx, book)
}
func (s *Service) SetDefault(ctx context.Context, book, edition, actor, bookVersion, editionVersion uint64, key string, enabled bool) (Book, error) {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return Book{}, err
	}
	hash, _, _ := publicationHash("default", edition, 0, struct {
		BookVersion, EditionVersion uint64
		Enabled                     bool
	}{bookVersion, editionVersion, enabled})
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		b, err := q.LockTextbook(ctx, book)
		if err != nil {
			return err
		}
		e, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book})
		if err != nil {
			return err
		}
		if replay, err := s.publicationReplay(ctx, q, book, actor, key, hash); err != nil || replay {
			return err
		}
		if b.Version != bookVersion || e.Version != editionVersion {
			return versionConflict()
		}
		if enabled && (e.Status != "published" || !validLicense(e, s.now().UTC()) || !e.CurrentGenerationID.Valid) {
			return apperr.New(apperr.BadRequest, "仅可设定授权有效的已发布版本")
		}
		value := b.DefaultEditionID
		if enabled {
			value = sql.NullInt64{Int64: int64(edition), Valid: true}
		} else if value.Valid && uint64(value.Int64) == edition {
			value = sql.NullInt64{}
		}
		if err = q.SetTextbookDefaultEdition(ctx, dbq.SetTextbookDefaultEditionParams{DefaultEditionID: value, UpdatedAt: s.now().UTC(), ID: book}); err != nil {
			return err
		}
		summary := "取消默认版次"
		if enabled {
			summary = "设为默认版次"
		}
		return s.publicationEvent(ctx, q, book, edition, 0, 0, actor, key, hash, "default", summary)
	})
	if err != nil {
		return Book{}, mapError(err)
	}
	return s.Get(ctx, book)
}
func (s *Service) Publications(ctx context.Context, book uint64) ([]dbq.TextbookPublicationEvent, error) {
	if _, err := dbq.New(s.db).GetTextbook(ctx, book); err != nil {
		return nil, mapError(err)
	}
	return dbq.New(s.db).ListTextbookPublicationEvents(ctx, book)
}
