// Package essay 是作文（T23，PRD 模块 5、11.13）：选题（真题、AI 命题、自拟）、写作与草稿、拍照手写稿、
// 按评分标准异步批改（分维度得分、总评、逐段批注、范文对比）、多稿、复核、作文本与评分标准。
//
// 批改次数：提交时预占 1 次（免费版每周 1 篇），批改完成结算，批改失败退回；复核重批不计次（PRD 13.1）。
// 按用户评分细则批改、且以真题限时完成的作文计入作文课预估分（PRD 11.13），按通用标准批改的只作参考。
package essay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/logx"
	"peetraining-server/internal/params"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// Enqueuer 排后台任务（asynq.Client 实现）。
type Enqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// Estimator 重算预估分（score.Service 实现）。
type Estimator interface {
	Recompute(ctx context.Context, userID, subjectID uint64, reason string) error
}

// Service 是作文服务。
type Service struct {
	db     *sql.DB
	q      *dbq.Queries
	params *params.Store
	ai     *ai.Engine
	quota  *quota.Service
	queue  Enqueuer
	score  Estimator
	now    func() time.Time
}

// Deps 是创建服务的依赖。Queue 为空时提交后同步批改（测试与本地）。
type Deps struct {
	DB     *sql.DB
	Params *params.Store
	AI     *ai.Engine
	Quota  *quota.Service
	Queue  Enqueuer
	Score  Estimator
	Now    func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: d.DB, q: dbq.New(d.DB), params: d.Params, ai: d.AI, quota: d.Quota, queue: d.Queue, score: d.Score, now: now}
}

func owner(userID uint64) sql.NullInt64 { return sql.NullInt64{Int64: int64(userID), Valid: true} }

func dec(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func fmtScore(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// 写作字数：去掉空白后的字符数。
func countWords(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// paragraphs 按换行分段，去掉空段。
func paragraphs(s string) []string {
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type subjectRow struct {
	ID, BankID uint64
	Name       string
	IsEssay    bool
}

func (s *Service) subject(ctx context.Context, userID, subjectID uint64) (subjectRow, error) {
	r, err := s.q.GetSubject(ctx, dbq.GetSubjectParams{ID: subjectID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return subjectRow{}, apperr.NotFoundErr()
	}
	if err != nil {
		return subjectRow{}, err
	}
	return subjectRow{ID: r.ID, BankID: r.BankID, Name: r.Name, IsEssay: r.IsEssay}, nil
}

// Rubric 是一份评分标准（5.9）。
type Rubric struct {
	ID         uint64
	Name       string
	Source     string // user_material / generic
	Origin     string
	FullScore  float64
	Dimensions []ai.EssayDim
	MaterialID uint64
	FileName   string
	Page       int
}

func rubricOf(id uint64, name, source, origin, full string, dims []byte, mid sql.NullInt64, file sql.NullString, page sql.NullInt32) Rubric {
	r := Rubric{ID: id, Name: name, Source: source, Origin: origin, FullScore: dec(full), FileName: file.String, Page: int(page.Int32)}
	_ = json.Unmarshal(dims, &r.Dimensions)
	if mid.Valid {
		r.MaterialID = uint64(mid.Int64)
	}
	return r
}

// activeRubric 是新写的作文用的评分标准：用户资料里识别出的优先，没有或切到通用时用通用五维度（PRD 11.13）。
func (s *Service) activeRubric(ctx context.Context, userID, subjectID uint64) (Rubric, error) {
	r, err := s.q.GetActiveEssayRubric(ctx, dbq.GetActiveEssayRubricParams{UserID: owner(userID), SubjectID: sql.NullInt64{Int64: int64(subjectID), Valid: true}})
	if err != nil {
		return Rubric{}, err
	}
	return rubricOf(r.ID, r.Name, string(r.Source), string(r.Origin), r.FullScore, r.Dimensions, r.SourceMaterialID, r.FileName, r.SourcePage), nil
}

func (s *Service) withTx(ctx context.Context, fn func(q *dbq.Queries) error) error {
	return store.WithTx(ctx, s.db, fn)
}

func (s *Service) essay(ctx context.Context, userID, id uint64) (dbq.Essay, error) {
	e, err := s.q.GetEssay(ctx, dbq.GetEssayParams{ID: id, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return dbq.Essay{}, apperr.NotFoundErr()
	}
	return e, err
}

// weekStart 是本周一 0 点（北京时间）对应的 UTC 时刻：每周目标与免费批改次数都按自然周。
func weekStart(t time.Time) time.Time {
	d := rules.DayOf(t)
	wd := int(d.Time().Weekday())
	return d.AddDays(-((wd + 6) % 7)).Time().UTC()
}

func logWarn(ctx context.Context, msg string, args ...any) { logx.From(ctx).Warn(msg, args...) }

func (s *Service) enqueue(ctx context.Context, userID, essayID uint64, round int) error {
	if s.queue == nil {
		return s.GradeEssay(ctx, userID, essayID)
	}
	t, err := jobs.NewEssayGradeTask(userID, essayID, round)
	if err != nil {
		return err
	}
	if _, err := s.queue.EnqueueContext(ctx, t); err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		return fmt.Errorf("排作文批改任务：%w", err)
	}
	return nil
}
