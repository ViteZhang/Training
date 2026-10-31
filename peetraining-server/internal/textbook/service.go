// Package textbook 管理跨项目共享的官方教材及不可覆盖的内容修订。
package textbook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

type Service struct {
	engine  *ai.Engine
	flags   *flags.Service
	imports ImportDeps
	indexes IndexDeps
	db      *sql.DB
	now     func() time.Time
}

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }

type Book struct {
	dbq.Textbook
	Aliases  []string
	Editions []Edition
}
type Edition struct {
	dbq.TextbookEdition
	Revisions []dbq.ListTextbookRevisionsRow
}
type Revision struct {
	dbq.TextbookRevision
	Sections []rules.TextbookSection
}
type Filter struct {
	Search, Kind, Discipline string
	Page, PageSize           int
}

func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	var e *mysql.MySQLError
	if errors.As(err, &e) && e.Number == 1062 {
		return apperr.New(apperr.Conflict, "记录已存在，请刷新后重试")
	}
	return err
}
func requestConflict() error {
	return apperr.New(apperr.Conflict, "请求标识已用于其他内容，请重新提交")
}
func versionConflict() error {
	return apperr.New(apperr.Conflict, "内容已被更新，请刷新后重试")
}
func (s *Service) hydrate(ctx context.Context, q *dbq.Queries, b dbq.Textbook) (Book, error) {
	out := Book{Textbook: b, Editions: []Edition{}}
	if err := json.Unmarshal(b.Aliases, &out.Aliases); err != nil {
		return out, err
	}
	editions, err := q.ListTextbookEditions(ctx, b.ID)
	if err != nil {
		return out, err
	}
	for _, e := range editions {
		revs, err := q.ListTextbookRevisions(ctx, e.ID)
		if err != nil {
			return out, err
		}
		out.Editions = append(out.Editions, Edition{e, revs})
	}
	return out, nil
}
func (s *Service) Get(ctx context.Context, id uint64) (Book, error) {
	var out Book
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		b, err := q.GetTextbook(ctx, id)
		if err != nil {
			return err
		}
		out, err = s.hydrate(ctx, q, b)
		return err
	})
	return out, mapError(err)
}
func (s *Service) List(ctx context.Context, f Filter) ([]Book, int64, error) {
	if f.Page < 1 || f.Page > 100000 || f.PageSize < 1 || f.PageSize > 50 || (f.Kind != "" && f.Kind != "textbook" && f.Kind != "handout") {
		return nil, 0, apperr.New(apperr.BadRequest, "筛选条件不正确")
	}
	out := []Book{}
	var total int64
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		var err error
		total, err = q.CountTextbooks(ctx, dbq.CountTextbooksParams{Search: f.Search, Kind: f.Kind, Discipline: f.Discipline})
		if err != nil {
			return err
		}
		rows, err := q.ListTextbooks(ctx, dbq.ListTextbooksParams{Search: f.Search, Kind: f.Kind, Discipline: f.Discipline, Limit: int32(f.PageSize), Offset: int32((f.Page - 1) * f.PageSize)})
		if err != nil {
			return err
		}
		for _, b := range rows {
			book, err := s.hydrate(ctx, q, b)
			if err != nil {
				return err
			}
			out = append(out, book)
		}
		return nil
	})
	return out, total, mapError(err)
}
func (s *Service) Create(ctx context.Context, actor uint64, key string, m rules.TextbookMetadata, e rules.TextbookEditionInput) (Book, error) {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return Book{}, err
	}
	m, err := rules.NormalizeTextbook(m)
	if err != nil {
		return Book{}, err
	}
	e, err = rules.NormalizeTextbookEdition(e)
	if err != nil {
		return Book{}, err
	}
	hash, _, err := rules.TextbookHash(struct {
		Metadata rules.TextbookMetadata
		Edition  rules.TextbookEditionInput
	}{m, e})
	if err != nil {
		return Book{}, err
	}
	_, aliases, err := rules.TextbookHash(m.Aliases)
	if err != nil {
		return Book{}, err
	}
	var id uint64
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		old, err := q.GetTextbookRequest(ctx, dbq.GetTextbookRequestParams{CreatedBy: actor, RequestKey: key})
		if err == nil {
			if old.RequestHash != hash {
				return requestConflict()
			}
			id = old.ID
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.now().UTC()
		n, err := q.CreateTextbook(ctx, dbq.CreateTextbookParams{Title: m.Title, Author: m.Author, Kind: m.Kind, Discipline: m.Discipline, Aliases: aliases, CreatedBy: actor, RequestKey: key, RequestHash: hash, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		id = uint64(n)
		_, err = createEdition(ctx, q, id, actor, key, e, now)
		return err
	})
	// 并发重试可能在唯一键处等待并失败；事务回滚后再读，仍核对请求内容。
	var duplicate *mysql.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		old, lookup := dbq.New(s.db).GetTextbookRequest(ctx, dbq.GetTextbookRequestParams{CreatedBy: actor, RequestKey: key})
		if lookup == nil {
			if old.RequestHash != hash {
				return Book{}, requestConflict()
			}
			id = old.ID
			err = nil
		}
	}
	if err != nil {
		return Book{}, mapError(err)
	}
	return s.Get(ctx, id)
}
func createEdition(ctx context.Context, q *dbq.Queries, book, actor uint64, key string, e rules.TextbookEditionInput, now time.Time) (uint64, error) {
	hash, _, err := rules.TextbookHash(e)
	if err != nil {
		return 0, err
	}
	year := sql.NullInt16{}
	if e.PublicationYear != nil {
		year = sql.NullInt16{Int16: int16(*e.PublicationYear), Valid: true}
	}
	id, err := q.CreateTextbookEdition(ctx, dbq.CreateTextbookEditionParams{BookID: book, Edition: e.Edition, Publisher: e.Publisher, PublicationYear: year, Isbn: e.ISBN, LicenseNote: e.LicenseNote, Authorized: e.Authorized, ScopeNote: e.ScopeNote, CreatedBy: actor, RequestKey: key, RequestHash: hash, CreatedAt: now})
	return uint64(id), err
}
func (s *Service) Update(ctx context.Context, id, version uint64, m rules.TextbookMetadata) (Book, error) {
	m, err := rules.NormalizeTextbook(m)
	if err != nil {
		return Book{}, err
	}
	_, aliases, err := rules.TextbookHash(m.Aliases)
	if err != nil {
		return Book{}, err
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		b, err := q.LockTextbook(ctx, id)
		if err != nil {
			return err
		}
		if b.Version != version {
			return versionConflict()
		}
		_, err = q.UpdateTextbookMetadata(ctx, dbq.UpdateTextbookMetadataParams{Title: m.Title, Author: m.Author, Kind: m.Kind, Discipline: m.Discipline, Aliases: aliases, UpdatedAt: s.now().UTC(), ID: id, Version: version})
		return err
	})
	if err != nil {
		return Book{}, mapError(err)
	}
	return s.Get(ctx, id)
}
func (s *Service) AddEdition(ctx context.Context, book, actor uint64, key string, e rules.TextbookEditionInput) (Book, error) {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return Book{}, err
	}
	e, err := rules.NormalizeTextbookEdition(e)
	if err != nil {
		return Book{}, err
	}
	hash, _, err := rules.TextbookHash(e)
	if err != nil {
		return Book{}, err
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbook(ctx, book); err != nil {
			return err
		}
		old, err := q.GetTextbookEditionRequest(ctx, dbq.GetTextbookEditionRequestParams{BookID: book, CreatedBy: actor, RequestKey: key})
		if err == nil {
			if old.RequestHash != hash {
				return requestConflict()
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = createEdition(ctx, q, book, actor, key, e, s.now().UTC())
		return err
	})
	if err != nil {
		return Book{}, mapError(err)
	}
	return s.Get(ctx, book)
}
func decodeRevision(r dbq.TextbookRevision) (Revision, error) {
	out := Revision{TextbookRevision: r}
	err := json.Unmarshal(r.Sections, &out.Sections)
	return out, err
}
func (s *Service) GetRevision(ctx context.Context, book, edition, id uint64) (Revision, error) {
	q := dbq.New(s.db)
	if _, err := q.GetTextbookEdition(ctx, dbq.GetTextbookEditionParams{ID: edition, BookID: book}); err != nil {
		return Revision{}, mapError(err)
	}
	r, err := q.GetTextbookRevision(ctx, dbq.GetTextbookRevisionParams{ID: id, EditionID: edition})
	if err != nil {
		return Revision{}, mapError(err)
	}
	return decodeRevision(r)
}

// NewRevision 首版创建空草稿，原文保存后方可校对；不会隐式复制其他修订内容。
func (s *Service) NewRevision(ctx context.Context, book, edition, actor uint64, key string) (Revision, error) {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return Revision{}, err
	}
	var out dbq.TextbookRevision
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book}); err != nil {
			return err
		}
		old, err := q.GetTextbookRevisionRequest(ctx, dbq.GetTextbookRevisionRequestParams{EditionID: edition, CreatedBy: actor, RequestKey: key})
		if err == nil {
			out = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		n, err := q.NextTextbookRevisionNo(ctx, edition)
		if err != nil {
			return err
		}
		hash, data, err := rules.TextbookHash([]rules.TextbookSection{})
		if err != nil {
			return err
		}
		now := s.now().UTC()
		id, err := q.CreateTextbookRevision(ctx, dbq.CreateTextbookRevisionParams{EditionID: edition, RevisionNo: uint64(n), Sections: data, ContentHash: hash, CreatedBy: actor, RequestKey: key, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		out, err = q.GetTextbookRevision(ctx, dbq.GetTextbookRevisionParams{ID: uint64(id), EditionID: edition})
		return err
	})
	if err != nil {
		return Revision{}, mapError(err)
	}
	return decodeRevision(out)
}
func (s *Service) SaveRevision(ctx context.Context, book, edition, id, actor, version uint64, sections []rules.TextbookSection, reviewed bool) (Revision, error) {
	if reviewed {
		for _, section := range sections {
			if section.NeedsReview {
				return Revision{}, apperr.New(apperr.BadRequest, "请先核对并确认所有不确定的原文单元")
			}
		}
	}
	sections, err := rules.NormalizeTextbookSections(sections)
	if err != nil {
		return Revision{}, err
	}
	hash, data, err := rules.TextbookHash(sections)
	if err != nil {
		return Revision{}, err
	}
	var out dbq.TextbookRevision
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book}); err != nil {
			return err
		}
		r, err := q.LockTextbookRevision(ctx, dbq.LockTextbookRevisionParams{ID: id, EditionID: edition})
		if err != nil {
			return err
		}
		if r.Status == "published" {
			return apperr.New(apperr.Conflict, "已发布原文不能覆盖，请创建新修订")
		}
		for _, sec := range sections {
			if sec.SourceImportID != nil {
				if *sec.SourceImportID < 1 || sec.SourcePage == nil || *sec.SourcePage < 1 {
					return apperr.New(apperr.BadRequest, "导入来源与页序不完整")
				}
				j, err := q.GetTextbookImport(ctx, dbq.GetTextbookImportParams{ID: uint64(*sec.SourceImportID), RevisionID: id})
				if err != nil {
					return err
				}
				if j.Status != "applied" {
					return apperr.New(apperr.BadRequest, "来源文件尚未采用")
				}
				if _, err = q.GetTextbookImportPage(ctx, dbq.GetTextbookImportPageParams{ImportID: j.ID, PageNo: uint32(*sec.SourcePage)}); err != nil {
					return err
				}
			}
		}
		status := "draft"
		if reviewed {
			status = "reviewed"
		}
		// 仅接受同一保存的网络重试；并发编辑即使正文相同也需检查版本。
		if r.Version != version {
			if r.Version == version+1 && r.ContentHash == hash && r.Status == status {
				out = r
				return nil
			}
			return versionConflict()
		}
		if r.ContentHash == hash && r.Status == status {
			out = r
			return nil
		}
		now := s.now().UTC()
		by := sql.NullInt64{}
		at := sql.NullTime{}
		if reviewed {
			by = sql.NullInt64{Int64: int64(actor), Valid: true}
			at = sql.NullTime{Time: now, Valid: true}
		}
		_, err = q.SaveTextbookRevision(ctx, dbq.SaveTextbookRevisionParams{Sections: data, ContentHash: hash, Status: status, ReviewedBy: by, ReviewedAt: at, UpdatedAt: now, ID: id, EditionID: edition, Version: version})
		if err != nil {
			return err
		}
		out, err = q.GetTextbookRevision(ctx, dbq.GetTextbookRevisionParams{ID: id, EditionID: edition})
		return err
	})
	if err != nil {
		return Revision{}, mapError(err)
	}
	return decodeRevision(out)
}
