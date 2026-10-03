package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/store"
)

// ---------- 7.3 会员与订单 ----------

// OrderRow 是订单列表的一行。
type OrderRow struct {
	OrderNo      string
	UserID       uint64
	Tier         string
	Channel      string
	AmountCents  int64
	Status       string
	RefundStatus string
	PaidAt       *time.Time
	CreatedAt    time.Time
}

// Orders 列出订单；status 为空时全部（已支付、退款、已关闭等）。
func (s *Service) Orders(ctx context.Context, status string, userID, before uint64) ([]OrderRow, error) {
	rows, err := s.q.AdminListOrders(ctx, dbq.AdminListOrdersParams{Status: status, UserID: int64(userID), BeforeID: int64(before)})
	if err != nil {
		return nil, err
	}
	out := make([]OrderRow, len(rows))
	for i, r := range rows {
		out[i] = OrderRow{OrderNo: r.OrderNo, UserID: uint64(r.OwnerUserID.Int64), Tier: string(r.Tier), Channel: string(r.Channel), AmountCents: int64(r.AmountCents),
			Status: string(r.Status), RefundStatus: str(r.RefundStatus), PaidAt: nullTimePtr(r.PaidAt), CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// TierSales 是一个档位本月的销量。
type TierSales struct {
	Tier    string
	Orders  int
	Revenue int64
}

// OrderSummary 是 7.3 顶部：本月收入、付费用户、付费转化（以导入过资料的用户为分母）、待处理退款、各档位销量。
type OrderSummary struct {
	MonthRevenue   int64
	PaidUsers      int
	ImportedUsers  int
	Conversion     float64
	PendingRefunds int
	Tiers          []TierSales
}

func (s *Service) OrderSummary(ctx context.Context) (OrderSummary, error) {
	ms := monthStart(s.now())
	r, err := s.q.AdminOrderSummary(ctx, ms)
	if err != nil {
		return OrderSummary{}, err
	}
	tiers, err := s.q.AdminSalesByTier(ctx, sql.NullTime{Time: ms, Valid: true})
	if err != nil {
		return OrderSummary{}, err
	}
	out := OrderSummary{MonthRevenue: r.MonthRevenue, PaidUsers: int(r.PaidUsers), ImportedUsers: int(r.ImportedUsers), PendingRefunds: int(r.PendingRefunds)}
	if r.ImportedUsers > 0 {
		out.Conversion = float64(r.PaidUsers) / float64(r.ImportedUsers)
	}
	for _, t := range tiers {
		out.Tiers = append(out.Tiers, TierSales{Tier: string(t.Tier), Orders: int(t.Orders), Revenue: t.Revenue})
	}
	return out, nil
}

// Usage 是退款处理时显示的用户使用情况（PRD 13.3）。
type Usage struct {
	Pages           int
	Gradings        int
	Answers         int
	FailedMaterials int
	DaysSincePaid   int
}

// OrderUsage 返回订单开通后的使用情况，帮助判断「开通 7 天内、且主要功能因我们的问题无法使用」。
func (s *Service) OrderUsage(ctx context.Context, orderNo string) (Usage, error) {
	o, err := s.q.GetOrderForUpdate(ctx, orderNo)
	if errors.Is(err, sql.ErrNoRows) {
		return Usage{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Usage{}, err
	}
	since := o.CreatedAt
	if o.PaidAt.Valid {
		since = o.PaidAt.Time
	}
	u, err := s.q.AdminOrderUsage(ctx, dbq.AdminOrderUsageParams{UserID: o.OwnerUserID.Int64, Since: since})
	if err != nil {
		return Usage{}, err
	}
	return Usage{Pages: int(u.Pages), Gradings: int(u.Gradings), Answers: int(u.Answers), FailedMaterials: int(u.FailedMaterials),
		DaysSincePaid: int(s.now().Sub(since).Hours() / 24)}, nil
}

// Refund 同意退款：渠道退款并收回本单会员（payment.Service.Refund）。
func (s *Service) Refund(ctx context.Context, adminID uint64, orderNo, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return apperr.New(apperr.BadRequest, "请填写退款原因")
	}
	return s.d.Payment.Refund(ctx, orderNo, reason, adminID)
}

// ---------- 7.4 兑换码 ----------

// RedeemSummary 是 7.4 顶部：已生成、已使用、可用、已停用或过期。
type RedeemSummary struct {
	Generated, Used, Available, Inactive int
}

func (s *Service) RedeemSummary(ctx context.Context) (RedeemSummary, error) {
	r, err := s.q.AdminRedeemSummary(ctx, dbq.AdminRedeemSummaryParams{Now: s.now().UTC()})
	return RedeemSummary{Generated: int(r.GeneratedCount), Used: int(r.UsedCount), Available: int(r.AvailableCount), Inactive: int(r.InactiveCount)}, err
}

// Batch 是兑换码批次。
type Batch struct {
	ID        uint64
	Name      string
	Tier      string
	Days      int
	Quantity  int
	Used      int
	ExpiresAt time.Time
	Channel   string
	Status    string
	CreatedAt time.Time
}

func (s *Service) Batches(ctx context.Context, before uint64) ([]Batch, error) {
	rows, err := s.q.AdminListBatches(ctx, dbq.AdminListBatchesParams{BeforeID: int64(before)})
	if err != nil {
		return nil, err
	}
	out := make([]Batch, len(rows))
	for i, r := range rows {
		out[i] = Batch{ID: r.ID, Name: r.Name, Tier: string(r.Tier), Days: int(r.Days.Int16), Quantity: int(r.Quantity), Used: int(r.UsedCount),
			ExpiresAt: r.CodeExpiresAt, Channel: r.Channel, Status: string(r.Status), CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// NewBatch 是新建批次的输入（7.4）：名称、档位、天数（赠送时必填）、数量、码有效期、渠道。
type NewBatch struct {
	Name      string
	Tier      string
	Days      int
	Quantity  int
	ExpiresAt time.Time
	Channel   string
}

// maxBatch 是一批最多生成多少个码。
const maxBatch = 5000

// codeAlphabet 与 membership 一致：8 位大写字母和数字，去掉 0 / O、1 / I（PRD 13.4）。
const codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

func newCode() string {
	b := make([]byte, 8)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		b[i] = codeAlphabet[n.Int64()]
	}
	return string(b)
}

// CreateBatch 生成一批兑换码。库里只存哈希（D28），明文只在这次返回，由后台当场导出（D37）。
func (s *Service) CreateBatch(ctx context.Context, adminID uint64, in NewBatch) (Batch, []string, error) {
	switch {
	case strings.TrimSpace(in.Name) == "":
		return Batch{}, nil, apperr.New(apperr.BadRequest, "请填写批次名称")
	case in.Quantity <= 0 || in.Quantity > maxBatch:
		return Batch{}, nil, apperr.New(apperr.BadRequest, "数量需在 1–5000 之间")
	case !in.ExpiresAt.After(s.now()):
		return Batch{}, nil, apperr.New(apperr.BadRequest, "码有效期要晚于现在")
	case in.Tier == "gift" && in.Days <= 0:
		return Batch{}, nil, apperr.New(apperr.BadRequest, "赠送天数必须大于 0")
	case in.Tier != "gift" && in.Tier != "sprint" && in.Tier != "season" && in.Tier != "monthly":
		return Batch{}, nil, apperr.New(apperr.BadRequest, "未知档位")
	}
	days := sql.NullInt16{Int16: int16(in.Days), Valid: in.Days > 0}
	var id int64
	codes := make([]string, 0, in.Quantity)
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		var err error
		id, err = q.AdminInsertBatch(ctx, dbq.AdminInsertBatchParams{Name: in.Name, Tier: dbq.RedeemBatchesTier(in.Tier), Days: days, Quantity: uint32(in.Quantity),
			CodeExpiresAt: in.ExpiresAt.UTC(), Channel: in.Channel, CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: true}})
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for len(codes) < in.Quantity {
			c := newCode()
			if seen[c] {
				continue
			}
			err := q.AdminInsertCode(ctx, dbq.AdminInsertCodeParams{BatchID: uint64(id), CodeHash: membership.HashCode(c), CodeTail: c[5:]})
			if isDuplicate(err) {
				continue // 与历史批次撞码：换一个
			}
			if err != nil {
				return err
			}
			seen[c] = true
			codes = append(codes, c)
		}
		return nil
	})
	if err != nil {
		return Batch{}, nil, err
	}
	return Batch{ID: uint64(id), Name: in.Name, Tier: in.Tier, Days: in.Days, Quantity: in.Quantity, ExpiresAt: in.ExpiresAt, Channel: in.Channel, Status: "active",
		CreatedAt: s.now()}, codes, nil
}

// DisableBatch 停用批次：未使用的码不能再兑换，已兑换的会员不受影响。
func (s *Service) DisableBatch(ctx context.Context, id uint64) error {
	n, err := s.q.AdminDisableBatch(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.q.AdminGetBatch(ctx, id); errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
	}
	return nil
}

// CodeRow 是批次明细或单码查询的一个码（明文不存，只有末 3 位）。
type CodeRow struct {
	ID        uint64
	Tail      string
	Status    string
	UsedBy    uint64
	UsedAt    *time.Time
	BatchID   uint64
	BatchName string
	Tier      string
	Days      int
	ExpiresAt time.Time
}

// BatchCodes 是批次明细（「导出」导出的是这个：末 3 位、状态、使用人与时间）。
func (s *Service) BatchCodes(ctx context.Context, id uint64) (Batch, []CodeRow, error) {
	b, err := s.q.AdminGetBatch(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Batch{}, nil, apperr.NotFoundErr()
	}
	if err != nil {
		return Batch{}, nil, err
	}
	rows, err := s.q.AdminBatchCodes(ctx, id)
	if err != nil {
		return Batch{}, nil, err
	}
	out := make([]CodeRow, len(rows))
	for i, r := range rows {
		out[i] = CodeRow{ID: r.ID, Tail: r.CodeTail, Status: string(r.Status), UsedBy: uint64(r.UsedBy.Int64), UsedAt: nullTimePtr(r.UsedAt), BatchID: id, BatchName: b.Name,
			Tier: string(b.Tier), Days: int(b.Days.Int16), ExpiresAt: b.CodeExpiresAt}
	}
	return Batch{ID: b.ID, Name: b.Name, Tier: string(b.Tier), Days: int(b.Days.Int16), Quantity: int(b.Quantity), ExpiresAt: b.CodeExpiresAt, Channel: b.Channel,
		Status: string(b.Status), CreatedAt: b.CreatedAt}, out, nil
}

// FindCode 单码查询：显示使用人与时间。
func (s *Service) FindCode(ctx context.Context, code string) (CodeRow, error) {
	c := membership.NormalizeCode(code)
	if c == "" {
		return CodeRow{}, apperr.NotFoundErr()
	}
	r, err := s.q.AdminFindCode(ctx, membership.HashCode(c))
	if errors.Is(err, sql.ErrNoRows) {
		return CodeRow{}, apperr.NotFoundErr()
	}
	if err != nil {
		return CodeRow{}, err
	}
	return CodeRow{ID: r.ID, Tail: r.CodeTail, Status: string(r.Status), UsedBy: uint64(r.UsedBy.Int64), UsedAt: nullTimePtr(r.UsedAt), BatchID: r.BatchID,
		BatchName: r.BatchName, Tier: string(r.Tier), Days: int(r.Days.Int16), ExpiresAt: r.CodeExpiresAt}, nil
}

// VoidCode 作废单码；已兑换的同时收回那段会员（PRD 13.4）。
func (s *Service) VoidCode(ctx context.Context, id uint64) error {
	now := s.now().UTC()
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		c, err := q.AdminGetCodeForUpdate(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		if c.Status == dbq.RedeemCodesStatusVoid {
			return nil
		}
		if _, err := q.AdminVoidCode(ctx, id); err != nil {
			return err
		}
		if c.MembershipID.Valid && c.UsedBy.Valid {
			if err := q.RevokeMembership(ctx, dbq.RevokeMembershipParams{RevokedAt: sql.NullTime{Time: now, Valid: true}, ID: uint64(c.MembershipID.Int64),
				OwnerUserID: uint64(c.UsedBy.Int64)}); err != nil {
				return err
			}
			return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: uint64(c.UsedBy.Int64), Mtype: dbq.MessagesMtypeMembership, Title: "兑换的会员已收回",
				Body:      "你使用的兑换码已被作废，对应的会员时长已收回；如有疑问请在「意见反馈」联系我们",
				DedupeKey: sql.NullString{String: "code_void:" + strconv.FormatInt(c.MembershipID.Int64, 10), Valid: true}})
		}
		return nil
	})
}
