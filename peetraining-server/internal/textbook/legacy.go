package textbook

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// LegacyMigrationResult 不输出原文。source_changed 表示迁移后旧记录变化，必须人工对账，不能覆盖已校对内容。
type LegacyMigrationResult struct {
	ReferenceID uint64 `json:"reference_id"`
	BookID      uint64 `json:"book_id,omitempty"`
	EditionID   uint64 `json:"edition_id,omitempty"`
	RevisionID  uint64 `json:"revision_id,omitempty"`
	Segments    int    `json:"segments"`
	Chars       int    `json:"chars"`
	SourceHash  string `json:"source_hash"`
	Status      string `json:"status"`
}

// MigrateLegacyReferences 默认只预览。每条旧素材独立建待校对、未授权草稿；不合并同名、不关联项目、不删除旧记录。
func (s *Service) MigrateLegacyReferences(ctx context.Context, actor uint64, apply bool) ([]LegacyMigrationResult, error) {
	if apply && actor == 0 {
		return nil, errors.New("apply requires an administrator actor id")
	}
	refs, err := dbq.New(s.db).ListLegacyTextbookReferences(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LegacyMigrationResult, 0, len(refs))
	for _, ref := range refs {
		var result LegacyMigrationResult
		err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
			source, err := q.LockLegacyTextbookReference(ctx, ref)
			if err != nil {
				return err
			}
			segments, err := q.ListLegacyTextbookSegments(ctx, ref)
			if err != nil {
				return err
			}
			hash, _, err := rules.TextbookHash(struct {
				Reference dbq.LockLegacyTextbookReferenceRow
				Segments  []dbq.OfficialReferenceSegment
			}{source, segments})
			if err != nil {
				return err
			}
			result = LegacyMigrationResult{ReferenceID: ref, Segments: len(segments), SourceHash: hash, Status: "would_create"}
			sections := make([]rules.TextbookSection, 0, len(segments))
			for _, seg := range segments {
				sections = append(sections, rules.TextbookSection{Heading: seg.Heading, Text: seg.Text, NeedsReview: true})
				result.Chars += utf8.RuneCountInString(seg.Text)
			}
			// manifest 仅保留原始序号与每段校验和，避免再次存储整本原文。
			type segmentCheck struct {
				ID   uint64 `json:"id"`
				Seq  uint32 `json:"seq"`
				Hash string `json:"hash"`
			}
			checks := make([]segmentCheck, 0, len(segments))
			for _, seg := range segments {
				h, _, e := rules.TextbookHash(seg)
				if e != nil {
					return e
				}
				checks = append(checks, segmentCheck{seg.ID, seg.Seq, h})
			}
			_, manifest, err := rules.TextbookHash(checks)
			if err != nil {
				return err
			}
			old, err := q.GetLegacyTextbookMigration(ctx, ref)
			if err == nil {
				result.BookID, result.EditionID, result.RevisionID = old.BookID, old.EditionID, old.RevisionID
				result.Status = "already_migrated"
				if old.SourceHash != hash {
					result.Status = "source_changed"
				}
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if !apply {
				return nil
			}
			now := s.now().UTC()
			key := fmt.Sprintf("legacy-reference-%d", ref)
			book, err := q.CreateTextbook(ctx, dbq.CreateTextbookParams{Title: source.Title, Author: "待核对（旧项目素材）", Kind: string(source.Kind), Discipline: source.SubjectName, Aliases: []byte("[]"), CreatedBy: actor, RequestKey: key, RequestHash: hash, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			result.BookID = uint64(book)
			edition, err := createEdition(ctx, q, result.BookID, actor, key, rules.TextbookEditionInput{Edition: "待核对（旧项目素材）", LicenseNote: source.LicenseNote, Authorized: false, ScopeNote: "迁移保留旧授权说明，需重新核实作者、版次与使用授权"}, now)
			if err != nil {
				return err
			}
			result.EditionID = edition
			contentHash, data, err := rules.TextbookHash(sections)
			if err != nil {
				return err
			}
			revision, err := q.CreateTextbookRevision(ctx, dbq.CreateTextbookRevisionParams{EditionID: edition, RevisionNo: 1, Sections: data, ContentHash: contentHash, CreatedBy: actor, RequestKey: key, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				return err
			}
			result.RevisionID = uint64(revision)
			if err = q.SaveLegacyTextbookMigration(ctx, dbq.SaveLegacyTextbookMigrationParams{ReferenceID: ref, BookID: result.BookID, EditionID: edition, RevisionID: result.RevisionID, CreatedAt: now, SourceHash: hash, Manifest: manifest, SourceProjectID: source.ProjectID, SourceTitle: source.Title}); err != nil {
				return err
			}
			result.Status = "created"
			return nil
		})
		if err != nil {
			return out, fmt.Errorf("legacy reference %d: %w", ref, err)
		}
		out = append(out, result)
	}
	return out, nil
}
