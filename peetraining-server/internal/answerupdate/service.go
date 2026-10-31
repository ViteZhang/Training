// Package answerupdate 提供持久化主动更新候选；生成成功扣次，采用独立事务且不再扣次。
package answerupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
	"peetraining-server/internal/textbook"
)

type Enqueuer interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}
type Service struct {
	db     *sql.DB
	engine *ai.Engine
	quota  *quota.Service
	queue  Enqueuer
	now    func() time.Time
}

func New(db *sql.DB, engine *ai.Engine, qs *quota.Service, queue Enqueuer) *Service {
	return &Service{db, engine, qs, queue, time.Now}
}
func owner(id uint64) sql.NullInt64 { return sql.NullInt64{Int64: int64(id), Valid: true} }
func mapError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	return err
}
func fingerprint(q dbq.Question) string {
	return ai.AnswerHash(q.Answer.String, struct {
		Stem, Passage, Score, Type, Origin string
		Options                            any
		RubricVersion                      uint32
	}{q.Stem, q.Passage.String, q.Score.String, string(q.Qtype), string(q.AnswerOrigin.QuestionsAnswerOrigin), q.Options, q.RubricVersion})
}
func (s *Service) Create(ctx context.Context, user, question uint64, key string) (dbq.AnswerUpdateCandidate, error) {
	var out dbq.AnswerUpdateCandidate
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return out, err
	}
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		current, err := q.LockQuestionAnswer(ctx, dbq.LockQuestionAnswerParams{ID: question, OwnerUserID: owner(user)})
		if err != nil {
			return err
		}
		old, err := q.GetAnswerCandidateRequest(ctx, dbq.GetAnswerCandidateRequestParams{QuestionID: question, OwnerUserID: user, RequestKey: key})
		if err == nil {
			out = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if strings.TrimSpace(current.Answer.String) == "" {
			return apperr.New(apperr.BadRequest, "请先生成当前参考答案")
		}
		candidates, err := q.ListAnswerCandidates(ctx, dbq.ListAnswerCandidatesParams{QuestionID: question, OwnerUserID: user})
		if err != nil {
			return err
		}
		for _, c := range candidates {
			if c.Status == "queued" || c.Status == "processing" {
				return apperr.New(apperr.Conflict, "已有答案正在生成，请稍后查看")
			}
		}
		if err = s.checkQuota(ctx, user); err != nil {
			return err
		}
		now := s.now().UTC()
		id, err := q.CreateAnswerCandidate(ctx, dbq.CreateAnswerCandidateParams{OwnerUserID: user, QuestionID: question, RequestKey: key, BaseHash: fingerprint(current), CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		out, err = q.GetAnswerCandidate(ctx, dbq.GetAnswerCandidateParams{ID: uint64(id), QuestionID: question, OwnerUserID: user})
		return err
	})
	if err != nil {
		return out, mapError(err)
	}
	if out.Status == "queued" {
		_ = s.enqueue(ctx, out.ID)
	}
	return out, nil
}
func (s *Service) checkQuota(ctx context.Context, user uint64) error {
	remaining, err := s.quota.Remaining(ctx, user, quota.AIQuestions)
	if err != nil {
		return err
	}
	if remaining != nil && *remaining <= 0 {
		return apperr.New(apperr.QuotaExceeded, "今天的 AI 出题次数用完了，明天 0 点恢复").With("quota_type", string(quota.AIQuestions)).With("remaining", 0)
	}
	return nil
}
func (s *Service) enqueue(ctx context.Context, id uint64) error {
	if s.queue == nil {
		return errors.New("candidate queue unavailable")
	}
	t, err := jobs.NewAnswerCandidateTask(id)
	if err != nil {
		return err
	}
	_, err = s.queue.EnqueueContext(ctx, t)
	if errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}
func (s *Service) RecoverAnswerCandidates(ctx context.Context) error {
	rows, err := dbq.New(s.db).RecoverAnswerCandidates(ctx, s.now().UTC().Add(-30*time.Minute))
	if err != nil {
		return err
	}
	for _, id := range rows {
		if err = s.enqueue(ctx, id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ProcessAnswerCandidate(ctx context.Context, id uint64) (retErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	lock := fmt.Sprintf("answer-candidate-%d", id)
	var got sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&got); err != nil {
		return err
	}
	if got.Int64 != 1 {
		return nil
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(c, "SELECT RELEASE_LOCK(?)", lock)
	}()
	q := dbq.New(s.db)
	job, err := q.GetAnswerCandidateJob(ctx, id)
	if err != nil {
		return err
	}
	if job.Status != "queued" && job.Status != "processing" {
		return nil
	}
	defer func() {
		if retErr != nil {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// 提交响应丢失时结果可能已经成功并扣次，不能把 ready/adopted 改回失败。
			_ = q.FailAnswerCandidate(c, dbq.FailAnswerCandidateParams{ID: id, QuestionID: job.QuestionID, OwnerUserID: job.OwnerUserID, FailureMessage: "生成失败，未扣除次数，请重新生成", UpdatedAt: s.now().UTC()})
		}
	}()
	if err = s.checkQuota(ctx, job.OwnerUserID); err != nil {
		return err
	}
	if err = q.SetAnswerCandidateProcessing(ctx, dbq.SetAnswerCandidateProcessingParams{ID: id, OwnerUserID: job.OwnerUserID, UpdatedAt: s.now().UTC()}); err != nil {
		return err
	}
	qr, err := q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: job.QuestionID, OwnerUserID: owner(job.OwnerUserID)})
	if err != nil {
		return err
	}
	score, _ := strconv.ParseFloat(qr.Score.String, 64)
	if !ai.Subjective(string(qr.Qtype)) {
		score = 0
	}
	in, err := importer.AnswerInput(ctx, q, job.OwnerUserID, qr.BankID, string(qr.Qtype), importer.FullStem(qr.Stem, qr.Passage.String), score)
	if err != nil {
		return err
	}
	out, _, err := s.engine.GenerateReferenceAnswer(ctx, job.OwnerUserID, in)
	if err != nil {
		return err
	}
	points, err := json.Marshal(out.Points)
	if err != nil {
		return err
	}
	return store.WithTx(ctx, s.db, func(tx *dbq.Queries) error {
		current, err := tx.LockQuestionAnswer(ctx, dbq.LockQuestionAnswerParams{ID: job.QuestionID, OwnerUserID: owner(job.OwnerUserID)})
		if err != nil {
			return err
		}
		c, err := tx.LockAnswerCandidate(ctx, dbq.LockAnswerCandidateParams{ID: id, QuestionID: job.QuestionID, OwnerUserID: job.OwnerUserID})
		if err != nil {
			return err
		}
		if c.Status == "ready" {
			return nil
		}
		if c.BaseHash != fingerprint(current) {
			return apperr.New(apperr.Conflict, "当前题目已修改，请重新生成")
		}
		snapshot, err := s.engine.SaveAnswerSnapshot(ctx, tx, owner(job.OwnerUserID), out.Answer, out.Points, out.Snapshot)
		if err != nil {
			return err
		}
		if err = s.quota.Consume(ctx, tx, quota.Charge{UserID: job.OwnerUserID, Type: quota.AIQuestions, Amount: 1, Ref: quota.Ref{Type: "answer_candidate", ID: id}, Key: quota.KeyFor("answer_candidate", id)}); err != nil {
			return err
		}
		return tx.CompleteAnswerCandidate(ctx, dbq.CompleteAnswerCandidateParams{ID: id, OwnerUserID: job.OwnerUserID, Answer: sql.NullString{String: out.Answer, Valid: true}, Points: points, SnapshotID: sql.NullInt64{Int64: int64(snapshot), Valid: snapshot > 0}, UpdatedAt: s.now().UTC()})
	})
}
func (s *Service) Get(ctx context.Context, user, question, id uint64) (dbq.AnswerUpdateCandidate, error) {
	if _, err := dbq.New(s.db).GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: question, OwnerUserID: owner(user)}); err != nil {
		return dbq.AnswerUpdateCandidate{}, mapError(err)
	}
	c, err := dbq.New(s.db).GetAnswerCandidate(ctx, dbq.GetAnswerCandidateParams{ID: id, QuestionID: question, OwnerUserID: user})
	return c, mapError(err)
}
func (s *Service) List(ctx context.Context, user, question uint64) ([]dbq.AnswerUpdateCandidate, error) {
	if _, err := dbq.New(s.db).GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: question, OwnerUserID: owner(user)}); err != nil {
		return nil, mapError(err)
	}
	return dbq.New(s.db).ListAnswerCandidates(ctx, dbq.ListAnswerCandidatesParams{QuestionID: question, OwnerUserID: user})
}
func (s *Service) Snapshot(ctx context.Context, user, id uint64) (*ai.AnswerSnapshot, error) {
	if id == 0 {
		return nil, nil
	}
	q := dbq.New(s.db)
	row, err := q.GetAnswerSnapshot(ctx, dbq.GetAnswerSnapshotParams{ID: id, OwnerUserID: owner(user)})
	if err != nil {
		return nil, mapError(err)
	}
	var out ai.AnswerSnapshot
	if json.Unmarshal(row.Payload, &out) != nil {
		return nil, errors.New("invalid answer evidence")
	}
	return textbook.VisibleSnapshot(ctx, q, &out)
}
func (s *Service) QuestionSnapshot(ctx context.Context, user, question uint64) (*ai.AnswerSnapshot, error) {
	q, err := dbq.New(s.db).GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: question, OwnerUserID: owner(user)})
	if err != nil {
		return nil, mapError(err)
	}
	return s.Snapshot(ctx, user, uint64(q.AnswerSnapshotID.Int64))
}
func (s *Service) Decide(ctx context.Context, user, question, id uint64, key string, adopt bool) (dbq.AnswerUpdateCandidate, error) {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return dbq.AnswerUpdateCandidate{}, err
	}
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		current, err := q.LockQuestionAnswer(ctx, dbq.LockQuestionAnswerParams{ID: question, OwnerUserID: owner(user)})
		if err != nil {
			return err
		}
		c, err := q.LockAnswerCandidate(ctx, dbq.LockAnswerCandidateParams{ID: id, QuestionID: question, OwnerUserID: user})
		if err != nil {
			return err
		}
		desired := "discarded"
		if adopt {
			desired = "adopted"
		}
		if c.Status == desired {
			return nil
		}
		if c.Status != "ready" {
			return apperr.New(apperr.Conflict, "这份候选暂不可采用，请重新生成")
		}
		if adopt {
			if c.BaseHash != fingerprint(current) {
				return apperr.New(apperr.Conflict, "当前答案或题目已修改，请重新生成")
			}
			var points []ai.RubricPoint
			if json.Unmarshal(c.Points, &points) != nil {
				return errors.New("invalid candidate points")
			}
			if c.SnapshotID.Valid {
				row, err := q.GetAnswerSnapshot(ctx, dbq.GetAnswerSnapshotParams{ID: uint64(c.SnapshotID.Int64), OwnerUserID: owner(user)})
				if err != nil {
					return err
				}
				var snap ai.AnswerSnapshot
				if json.Unmarshal(row.Payload, &snap) != nil {
					return errors.New("invalid snapshot")
				}
				if err = s.engine.ValidateAnswerSnapshot(ctx, q, &snap); err != nil {
					return apperr.New(apperr.Conflict, "这份候选暂不可采用，请重新生成")
				}
			}
			if err = q.SetQuestionAnswer(ctx, dbq.SetQuestionAnswerParams{ID: question, OwnerUserID: owner(user), Answer: c.Answer, AnswerOrigin: dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginAiGenerated, Valid: true}, AiAnswerAt: sql.NullTime{Time: s.now().UTC(), Valid: true}}); err != nil {
				return err
			}
			if err = q.SetQuestionAnswerSnapshot(ctx, dbq.SetQuestionAnswerSnapshotParams{ID: question, OwnerUserID: owner(user), AnswerSnapshotID: c.SnapshotID}); err != nil {
				return err
			}
			if err = q.DeleteQuestionRubric(ctx, dbq.DeleteQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(question), Valid: true}, OwnerUserID: owner(user)}); err != nil {
				return err
			}
			for i, p := range points {
				kw, _ := json.Marshal(p.Keywords)
				if err = q.InsertRubricPoint(ctx, dbq.InsertRubricPointParams{OwnerUserID: owner(user), QuestionID: sql.NullInt64{Int64: int64(question), Valid: true}, Seq: uint16(i + 1), Content: p.Content, Keywords: kw, Score: sql.NullString{String: strconv.FormatFloat(p.Score, 'f', 2, 64), Valid: true}, Origin: dbq.RubricPointsOriginAiGenerated}); err != nil {
					return err
				}
			}
			if err = q.BumpQuestionAnswerVersion(ctx, dbq.BumpQuestionAnswerVersionParams{ID: question, OwnerUserID: owner(user)}); err != nil {
				return err
			}
		}
		return q.SetAnswerCandidateStatus(ctx, dbq.SetAnswerCandidateStatusParams{ID: id, QuestionID: question, OwnerUserID: user, Status: desired, UpdatedAt: s.now().UTC()})
	})
	if err != nil {
		return dbq.AnswerUpdateCandidate{}, mapError(err)
	}
	return s.Get(ctx, user, question, id)
}

func (s *Service) GradingSnapshot(ctx context.Context, user, id uint64) (*ai.AnswerSnapshot, error) {
	row, err := dbq.New(s.db).GetGrading(ctx, dbq.GetGradingParams{ID: id, OwnerUserID: user})
	if err != nil {
		return nil, mapError(err)
	}
	var snap struct {
		AnswerSnapshotID uint64 `json:"answer_snapshot_id"`
	}
	if err = json.Unmarshal(row.RubricSnapshot, &snap); err != nil && len(row.RubricSnapshot) > 0 {
		return nil, err
	}
	return s.Snapshot(ctx, user, snap.AnswerSnapshotID)
}
func (s *Service) ImportSnapshot(ctx context.Context, user, id uint64) (*ai.AnswerSnapshot, error) {
	row, err := dbq.New(s.db).GetImportItem(ctx, dbq.GetImportItemParams{ID: id, OwnerUserID: user})
	if err != nil {
		return nil, mapError(err)
	}
	var draft importer.QuestionDraft
	if err = json.Unmarshal(row.Payload, &draft); err != nil {
		return nil, err
	}
	return s.Snapshot(ctx, user, draft.AnswerSnapshotID)
}
