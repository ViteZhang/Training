package official

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
	"sort"
)

func (s *Service) ProjectTextbooks(ctx context.Context, project uint64) (rows []dbq.ListProjectTextbooksRow, version uint64, err error) {
	err = store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		p, e := q.LockOfficialProject(ctx, project)
		if e != nil {
			return e
		}
		version = p.TextbookLinkVersion
		rows, e = q.ListProjectTextbooks(ctx, project)
		return e
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = apperr.NotFoundErr()
	}
	return
}
func (s *Service) SetProjectTextbooks(ctx context.Context, project uint64, editions []uint64, expectedVersion uint64, key string) error {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return err
	}
	editions = append([]uint64{}, editions...)
	sort.Slice(editions, func(i, j int) bool { return editions[i] < editions[j] })
	hash := ai.AnswerHash("", editions)
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		p, err := q.LockOfficialProject(ctx, project)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		old, err := q.GetProjectTextbookWrite(ctx, dbq.GetProjectTextbookWriteParams{ProjectID: project, RequestKey: key})
		if err == nil {
			if old != hash {
				return apperr.New(apperr.Conflict, "同一请求不能更改关联内容")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if p.TextbookLinkVersion != expectedVersion {
			return apperr.New(apperr.Conflict, "教材关联已被更新，请刷新后重试")
		}
		if len(editions) > 20 {
			return apperr.New(apperr.BadRequest, "最多关联20个教材版本")
		}
		for _, id := range editions {
			e, err := q.GetTextbookEditionByID(ctx, id)
			if errors.Is(err, sql.ErrNoRows) {
				return apperr.NotFoundErr()
			}
			if err != nil {
				return err
			}
			e, err = q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: id, BookID: e.BookID})
			if err != nil {
				return err
			}
			if e.Status != "published" || !e.CurrentGenerationID.Valid || !e.Authorized || (e.LicenseExpiresAt.Valid && !e.LicenseExpiresAt.Time.After(s.now().UTC())) {
				return apperr.New(apperr.Conflict, "请选择当前可用的已发布教材版本")
			}
		}
		if err := q.DeleteProjectTextbooks(ctx, project); err != nil {
			return err
		}
		seen := map[uint64]bool{}
		for _, id := range editions {
			if seen[id] {
				continue
			}
			seen[id] = true
			if err := q.AddProjectTextbook(ctx, dbq.AddProjectTextbookParams{ProjectID: project, EditionID: id, CreatedAt: s.now().UTC()}); err != nil {
				return err
			}
		}
		if err := q.BumpProjectTextbookVersion(ctx, project); err != nil {
			return err
		}
		return q.SaveProjectTextbookWrite(ctx, dbq.SaveProjectTextbookWriteParams{ProjectID: project, RequestKey: key, RequestHash: hash})
	})
}

func officialEvidenceHash(p QuestionPayload) string {
	return ai.AnswerHash(p.Answer, struct {
		Stem, QType string
		Points      []Point
	}{p.Stem, p.QType, p.Rubric})
}
func (s *Service) saveOfficialEvidence(ctx context.Context, project uint64, qtype, stem string, out ai.GenerateAnswerOut, points []Point) (uint64, error) {
	var id uint64
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		var err error
		id, err = s.d.AI.SaveAnswerSnapshot(ctx, q, sql.NullInt64{}, out.Answer, out.Points, out.Snapshot)
		if err != nil || id == 0 {
			return err
		}
		return q.SaveOfficialAnswerCandidate(ctx, dbq.SaveOfficialAnswerCandidateParams{ProjectID: project, SnapshotID: id, ContentHash: officialEvidenceHash(QuestionPayload{Answer: out.Answer, Stem: stem, QType: qtype, Rubric: points})})
	})
	return id, err
}

// Copy only the citation snapshot; no public textbook content is duplicated.
func (s *Service) copyQuestionEvidence(ctx context.Context, q *dbq.Queries, sourceID uint64, user, question uint64) error {
	if sourceID == 0 {
		return nil
	}
	row, err := q.GetAnswerSnapshot(ctx, dbq.GetAnswerSnapshotParams{ID: sourceID})
	if err != nil {
		return err
	}
	var snapshot ai.AnswerSnapshot
	if err = json.Unmarshal(row.Payload, &snapshot); err != nil {
		return err
	}
	if err = s.d.AI.ValidateAnswerSnapshot(ctx, q, &snapshot); err != nil {
		return apperr.New(apperr.Conflict, "官方内容依据已变化，请稍后重试")
	}
	id, err := q.CreateAnswerSnapshot(ctx, dbq.CreateAnswerSnapshotParams{OwnerUserID: sql.NullInt64{Int64: int64(user), Valid: true}, AnswerHash: row.AnswerHash, Payload: row.Payload, CreatedAt: s.now().UTC()})
	if err != nil {
		return err
	}
	return q.SetQuestionAnswerSnapshot(ctx, dbq.SetQuestionAnswerSnapshotParams{ID: question, OwnerUserID: sql.NullInt64{Int64: int64(user), Valid: true}, AnswerSnapshotID: sql.NullInt64{Int64: id, Valid: true}})
}
