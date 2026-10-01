// Package quota 是额度台账（PRD 13.1，dev-spec「额度台账」）：解析页数、导入题数、批改次数、AI 出题数、整卷批改、作文批改。
//
// 规矩（CLAUDE.md 必须遵守第 7 条）：扣额度与写结果在同一个事务里——这里的扣减方法都接收调用方事务里的 *dbq.Queries；
// 每次扣减带幂等键，同一个键只生效一次；扣减前锁住计数行，并发扣减不会超额。
package quota

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
)

// Type 是额度类型，与 quota_counters.quota_type 一致。
type Type string

const (
	ParsePages      Type = "parse_pages"
	ImportQuestions Type = "import_questions"
	Grading         Type = "grading"
	AIQuestions     Type = "ai_questions"
	PaperGrading    Type = "paper_grading"
	EssayGrading    Type = "essay_grading"
)

// AllTypes 是额度查询接口返回的顺序。
var AllTypes = []Type{ParsePages, ImportQuestions, Grading, AIQuestions, PaperGrading, EssayGrading}

// Period 是重置周期。
type Period string

const (
	Total   Period = "total"
	Monthly Period = "monthly"
	Daily   Period = "daily"
	Weekly  Period = "weekly"
)

var names = map[Type]string{
	ParsePages: "资料解析页数", ImportQuestions: "题目导入", Grading: "主观题批改", AIQuestions: "AI 出题",
	PaperGrading: "整卷批改", EssayGrading: "作文批改",
}

// limits 是 rule_params.quota，nil 表示不限。
type limits struct {
	Free struct {
		ParsePagesTotal      *int `json:"parse_pages_total"`
		ImportQuestionsTotal *int `json:"import_questions_total"`
		GradingDaily         *int `json:"grading_daily"`
		AIQuestionsDaily     *int `json:"ai_questions_daily"`
		PaperGradingWeekly   *int `json:"paper_grading_weekly"`
		EssayGradingWeekly   *int `json:"essay_grading_weekly"`
	} `json:"free"`
	Member struct {
		ParsePagesMonthly    *int `json:"parse_pages_monthly"`
		ImportQuestionsTotal *int `json:"import_questions_total"`
		GradingDaily         *int `json:"grading_daily"`
		AIQuestionsDaily     *int `json:"ai_questions_daily"`
		PaperGradingWeekly   *int `json:"paper_grading_weekly"`
		EssayGradingWeekly   *int `json:"essay_grading_weekly"`
	} `json:"member"`
}

// Rule 是某用户某类额度的规则：上限（nil 不限）与周期。
type Rule struct {
	Limit  *int
	Period Period
}

// Service 读额度规则、查询用量、执行扣减。
type Service struct {
	q      dbq.Querier
	params *params.Store
	now    func() time.Time
}

func New(q dbq.Querier, ps *params.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{q: q, params: ps, now: now}
}

// Rules 返回用户当前身份下每类额度的规则（免费版累计 / 每日 / 每周；会员解析页数按月，其余不限）。
func (s *Service) Rules(ctx context.Context, userID uint64) (map[Type]Rule, bool, error) {
	var l limits
	if err := s.params.Get(ctx, "quota", &l); err != nil {
		return nil, false, err
	}
	member, err := membership.IsMember(ctx, s.q, userID, s.now().UTC())
	if err != nil {
		return nil, false, err
	}
	if member {
		return map[Type]Rule{
			ParsePages:      {l.Member.ParsePagesMonthly, Monthly},
			ImportQuestions: {l.Member.ImportQuestionsTotal, Total},
			Grading:         {l.Member.GradingDaily, Daily},
			AIQuestions:     {l.Member.AIQuestionsDaily, Daily},
			PaperGrading:    {l.Member.PaperGradingWeekly, Weekly},
			EssayGrading:    {l.Member.EssayGradingWeekly, Weekly},
		}, true, nil
	}
	return map[Type]Rule{
		ParsePages:      {l.Free.ParsePagesTotal, Total},
		ImportQuestions: {l.Free.ImportQuestionsTotal, Total},
		Grading:         {l.Free.GradingDaily, Daily},
		AIQuestions:     {l.Free.AIQuestionsDaily, Daily},
		PaperGrading:    {l.Free.PaperGradingWeekly, Weekly},
		EssayGrading:    {l.Free.EssayGradingWeekly, Weekly},
	}, false, nil
}

// PeriodKey 返回某周期在某时刻（北京时间）的键：total / 2026-10 / 2026-10-01 / 2026-W40。
func PeriodKey(p Period, t time.Time) string {
	day := rules.DayOf(t)
	d := day.Date()
	switch p {
	case Monthly:
		return d.Format("2006-01")
	case Daily:
		return d.Format("2006-01-02")
	case Weekly:
		y, w := d.ISOWeek()
		return fmt.Sprintf("%d-W%02d", y, w)
	default:
		return "total"
	}
}

// ResetsAt 返回周期的下次重置时间（北京时间 0 点；每周一 0 点；每月 1 日 0 点）；累计额度返回零值。
func ResetsAt(p Period, t time.Time) time.Time {
	day := rules.DayOf(t)
	switch p {
	case Daily:
		return day.AddDays(1).Time()
	case Weekly:
		wd := int(day.Date().Weekday())
		if wd == 0 {
			wd = 7
		}
		return day.AddDays(8 - wd).Time()
	case Monthly:
		d := day.Date()
		first := time.Date(d.Year(), d.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		return rules.DayFromDateColumn(first).Time()
	default:
		return time.Time{}
	}
}

// Item 是某类额度的用量。
type Item struct {
	Type     Type
	Used     int // 已用 + 预占
	Limit    *int
	Period   Period
	ResetsAt time.Time
}

// Summary 返回全部额度的用量（3.1c、6.3、4.9）。
func (s *Service) Summary(ctx context.Context, userID uint64) ([]Item, bool, error) {
	rs, member, err := s.Rules(ctx, userID)
	if err != nil {
		return nil, false, err
	}
	now := s.now()
	out := make([]Item, 0, len(AllTypes))
	for _, t := range AllTypes {
		r := rs[t]
		c, err := s.q.GetQuotaCounter(ctx, dbq.GetQuotaCounterParams{OwnerUserID: userID, QuotaType: dbq.QuotaCountersQuotaType(t), PeriodKey: PeriodKey(r.Period, now)})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
		out = append(out, Item{Type: t, Used: int(c.Used + c.Reserved), Limit: r.Limit, Period: r.Period, ResetsAt: ResetsAt(r.Period, now)})
	}
	return out, member, nil
}

// Ref 是扣减关联的业务对象（写进流水，便于排查与对账）。
type Ref struct {
	Type string
	ID   uint64
}

// Charge 是一次扣减。
type Charge struct {
	UserID uint64
	Type   Type
	Amount int
	Ref    Ref
	// Key 是幂等键：同一个键只生效一次（重试、重复提交不重复扣）。
	Key string
}

// Ticket 是预占的凭据，结算或退回时要用：额度的周期在预占时确定，跨天结算也记在预占那天。
type Ticket struct {
	UserID uint64
	Type   Type
	Period string
	Amount int
	Key    string
}

var errExceeded = apperr.New(apperr.QuotaExceeded, "额度不足")

// Remaining 返回某类额度还剩多少（nil 表示不限）。
func (s *Service) Remaining(ctx context.Context, userID uint64, t Type) (*int, error) {
	rs, _, err := s.Rules(ctx, userID)
	if err != nil {
		return nil, err
	}
	r := rs[t]
	if r.Limit == nil {
		return nil, nil
	}
	c, err := s.q.GetQuotaCounter(ctx, dbq.GetQuotaCounterParams{OwnerUserID: userID, QuotaType: dbq.QuotaCountersQuotaType(t), PeriodKey: PeriodKey(r.Period, s.now())})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	left := max(*r.Limit-int(c.Used+c.Reserved), 0)
	return &left, nil
}

// Consume 直接扣减（批改、AI 出题等一次性用量）。额度不足返回 QUOTA_EXCEEDED（detail 带 quota_type、limit、used）。
// q 必须是调用方写业务结果的同一个事务。
func (s *Service) Consume(ctx context.Context, q *dbq.Queries, c Charge) error {
	_, err := s.apply(ctx, q, c, "consume")
	return err
}

// Reserve 预占（资料解析：解析前按页数预占，结算时退回失败页）。
func (s *Service) Reserve(ctx context.Context, q *dbq.Queries, c Charge) (Ticket, error) {
	return s.apply(ctx, q, c, "reserve")
}

func (s *Service) apply(ctx context.Context, q *dbq.Queries, c Charge, action string) (Ticket, error) {
	rs, _, err := s.Rules(ctx, c.UserID)
	if err != nil {
		return Ticket{}, err
	}
	r := rs[c.Type]
	period := PeriodKey(r.Period, s.now())
	t := Ticket{UserID: c.UserID, Type: c.Type, Period: period, Amount: c.Amount, Key: c.Key}
	if c.Amount <= 0 {
		return t, nil
	}
	if dup, err := s.seen(ctx, q, c.UserID, c.Key); err != nil || dup {
		return t, err
	}
	row, err := s.lock(ctx, q, c.UserID, c.Type, period)
	if err != nil {
		return Ticket{}, err
	}
	if r.Limit != nil && int(row.Used+row.Reserved)+c.Amount > *r.Limit {
		return Ticket{}, errExceeded.With("quota_type", string(c.Type)).With("limit", *r.Limit).
			With("used", int(row.Used+row.Reserved)).With("need", c.Amount).
			With("period", string(r.Period)).Wrap(fmt.Errorf("%s 不足", names[c.Type]))
	}
	used, reserved := row.Used, row.Reserved
	if action == "reserve" {
		reserved += uint32(c.Amount)
	} else {
		used += uint32(c.Amount)
	}
	if err := s.write(ctx, q, c.UserID, c.Type, period, used, reserved); err != nil {
		return Ticket{}, err
	}
	return t, s.ledger(ctx, q, c.UserID, c.Type, period, action, c.Amount, c.Ref, c.Key)
}

// Settle 结算预占：实际用量记为已用，多预占的退回（解析失败的页退回额度）。actual 可以大于预占，不再检查上限。
func (s *Service) Settle(ctx context.Context, q *dbq.Queries, t Ticket, actual int, ref Ref) error {
	key := t.Key + ":settle"
	if dup, err := s.seen(ctx, q, t.UserID, key); err != nil || dup {
		return err
	}
	row, err := s.lock(ctx, q, t.UserID, t.Type, t.Period)
	if err != nil {
		return err
	}
	reserved := int(row.Reserved) - t.Amount
	if reserved < 0 {
		reserved = 0
	}
	used := int(row.Used) + max(actual, 0)
	if err := s.write(ctx, q, t.UserID, t.Type, t.Period, uint32(used), uint32(reserved)); err != nil {
		return err
	}
	return s.ledger(ctx, q, t.UserID, t.Type, t.Period, "settle", actual, ref, key)
}

// Refund 退回已用额度（如重新批改不计次时退回）。
func (s *Service) Refund(ctx context.Context, q *dbq.Queries, userID uint64, typ Type, period string, amount int, ref Ref, key string) error {
	if dup, err := s.seen(ctx, q, userID, key); err != nil || dup {
		return err
	}
	row, err := s.lock(ctx, q, userID, typ, period)
	if err != nil {
		return err
	}
	used := max(int(row.Used)-amount, 0)
	if err := s.write(ctx, q, userID, typ, period, uint32(used), row.Reserved); err != nil {
		return err
	}
	return s.ledger(ctx, q, userID, typ, period, "refund", amount, ref, key)
}

func (s *Service) seen(ctx context.Context, q *dbq.Queries, userID uint64, key string) (bool, error) {
	_, err := q.GetQuotaLedgerByKey(ctx, dbq.GetQuotaLedgerByKeyParams{OwnerUserID: userID, IdempotencyKey: key})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// lock 锁住计数行。计数行只在事务外（自动提交）创建：如果在事务里 INSERT IGNORE 再 SELECT FOR UPDATE，
// 并发事务会先各自拿到共享锁再争排他锁，互相死锁（T08 并发测试测出来的）。
// 事务外先用不加锁的读判断行是否已存在，避免同一事务第二次扣同一类额度时，事务外的插入等待本事务持有的行锁。
func (s *Service) lock(ctx context.Context, q *dbq.Queries, userID uint64, t Type, period string) (dbq.QuotaCounter, error) {
	qt := dbq.QuotaCountersQuotaType(t)
	if _, err := s.q.GetQuotaCounter(ctx, dbq.GetQuotaCounterParams{OwnerUserID: userID, QuotaType: qt, PeriodKey: period}); errors.Is(err, sql.ErrNoRows) {
		if err := s.q.EnsureQuotaCounter(ctx, dbq.EnsureQuotaCounterParams{OwnerUserID: userID, QuotaType: qt, PeriodKey: period}); err != nil {
			return dbq.QuotaCounter{}, err
		}
	} else if err != nil {
		return dbq.QuotaCounter{}, err
	}
	return q.LockQuotaCounter(ctx, dbq.LockQuotaCounterParams{OwnerUserID: userID, QuotaType: qt, PeriodKey: period})
}

func (s *Service) write(ctx context.Context, q *dbq.Queries, userID uint64, t Type, period string, used, reserved uint32) error {
	return q.UpdateQuotaCounter(ctx, dbq.UpdateQuotaCounterParams{Used: used, Reserved: reserved, OwnerUserID: userID, QuotaType: dbq.QuotaCountersQuotaType(t), PeriodKey: period})
}

func (s *Service) ledger(ctx context.Context, q *dbq.Queries, userID uint64, t Type, period, action string, amount int, ref Ref, key string) error {
	return q.InsertQuotaLedger(ctx, dbq.InsertQuotaLedgerParams{
		OwnerUserID: userID, QuotaType: dbq.QuotaLedgerQuotaType(t), PeriodKey: period, Action: dbq.QuotaLedgerAction(action),
		Amount: int32(amount), RefType: sql.NullString{String: ref.Type, Valid: ref.Type != ""},
		RefID: sql.NullInt64{Int64: int64(ref.ID), Valid: ref.ID != 0}, IdempotencyKey: key,
	})
}

// KeyFor 生成幂等键：业务前缀 + ID，保证同一业务对象只扣一次。
func KeyFor(prefix string, id uint64) string { return prefix + ":" + strconv.FormatUint(id, 10) }
