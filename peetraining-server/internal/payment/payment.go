// Package payment 是会员中心与在线支付（6.5、6.6，PRD 13.2、13.3，ADR 0011）：档位与价格、下单、支付回调开通、
// App Store 内购校验、退款。在线支付受 online_payment 开关控制，默认关闭；关闭时只能用兑换码开通。
//
// 幂等：回调可能重复到达、App 可能重复提交内购校验——开通在一个事务里完成，先锁订单行，已支付的订单直接返回，
// 同一笔支付只开通一次。
package payment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/pay"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/membership"
	"peetraining-server/internal/params"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// FlagChecker 判断开关对某用户是否打开。
type FlagChecker interface {
	Enabled(ctx context.Context, key string, userID uint64) (bool, error)
}

// Deps 是创建服务的依赖。Gateway 为空表示没有可用的支付渠道（生产环境没配商户时），此时下单接口返回 404。
type Deps struct {
	DB      *sql.DB
	Params  *params.Store
	Quota   *quota.Service
	Flags   FlagChecker
	Gateway pay.Gateway
	// NotifyBaseURL 是回调地址前缀，拼上 /pay/notify/{channel}。
	NotifyBaseURL string
	// AppleBundleID 校验内购交易属于本 App。
	AppleBundleID string
	Log           *slog.Logger
	Now           func() time.Time
}

// Service 是会员中心与支付。
type Service struct {
	d   Deps
	q   *dbq.Queries
	now func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{d: d, q: dbq.New(d.DB), now: now}
}

// planConfig 是 rule_params.pricing 的一项（后台 7.8 可改价格与内购商品）。有效期按档位固定（membership.Span），
// 不读这里的 until、days。
type planConfig struct {
	Name           string `json:"name"`
	PriceCents     int64  `json:"cents"`
	AppleProductID string `json:"apple_product_id"`
	Recommended    bool   `json:"recommended"`
	Disabled       bool   `json:"disabled"`
}

// tierOrder 是会员中心的展示顺序（PRD 13.2）。
var tierOrder = map[string]int{"sprint": 0, "season": 1, "monthly": 2}

func (s *Service) plans(ctx context.Context) (map[string]planConfig, error) {
	var m map[string]planConfig
	if err := s.d.Params.Get(ctx, "pricing", &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Plan 是一个会员档位。Available 为 false 时不能购买（冲刺卡在初试日期未公布时、后台下架时），UnavailableReason 说明原因。
type Plan struct {
	Tier              string
	Name              string
	PriceCents        int64
	AppleProductID    string
	Recommended       bool
	EndsAt            time.Time // 现在购买、叠加后的有效期截止
	Available         bool
	UnavailableReason string
}

// Center 是会员中心（6.5）。
type Center struct {
	PaymentEnabled bool
	Channels       []pay.Channel
	Plans          []Plan
	Benefits       []quota.Benefit
}

// paymentOn 判断在线支付对这个用户是否可用：开关打开且有可用渠道。
func (s *Service) paymentOn(ctx context.Context, userID uint64) (bool, error) {
	if s.d.Gateway == nil || len(s.d.Gateway.Channels()) == 0 {
		return false, nil
	}
	return s.d.Flags.Enabled(ctx, flags.OnlinePayment, userID)
}

// Center 返回档位、价格、预计有效期与权益对比。支付关闭时 App 只显示兑换码入口（6.5）。
func (s *Service) Center(ctx context.Context, userID uint64) (Center, error) {
	on, err := s.paymentOn(ctx, userID)
	if err != nil {
		return Center{}, err
	}
	cfg, err := s.plans(ctx)
	if err != nil {
		return Center{}, err
	}
	benefits, err := s.d.Quota.Benefits(ctx)
	if err != nil {
		return Center{}, err
	}
	out := Center{PaymentEnabled: on, Benefits: benefits}
	if on {
		out.Channels = s.d.Gateway.Channels()
	}
	now := s.now()
	for tier, c := range cfg {
		if _, ok := tierOrder[tier]; !ok {
			continue
		}
		p := Plan{Tier: tier, Name: c.Name, PriceCents: c.PriceCents, AppleProductID: c.AppleProductID, Recommended: c.Recommended, Available: !c.Disabled}
		if c.Disabled {
			p.UnavailableReason = "暂未开放"
		}
		_, end, err := membership.Span(ctx, s.q, membership.Grant{UserID: userID, Tier: tier, Now: now})
		switch {
		case errors.Is(err, membership.ErrNoExamDate):
			p.Available, p.UnavailableReason = false, "初试日期公布后开放"
		case err != nil:
			return Center{}, err
		default:
			p.EndsAt = end
		}
		out.Plans = append(out.Plans, p)
	}
	sort.Slice(out.Plans, func(i, j int) bool { return tierOrder[out.Plans[i].Tier] < tierOrder[out.Plans[j].Tier] })
	return out, nil
}

// Order 是订单（6.6 支付结果轮询它）。
type Order struct {
	OrderNo         string
	Tier            string
	Channel         string
	AmountCents     int64
	Status          string
	PaidAt          *time.Time
	CreatedAt       time.Time
	Prepay          map[string]string // 只在下单时返回
	AppAccountToken string            // 内购：App 购买时传给 StoreKit
	AppleProductID  string
	// MembershipEndsAt 是支付成功后会员叠加到的截止时间。
	MembershipEndsAt *time.Time
}

func notFound() error { return apperr.NotFoundErr() }

// newOrderNo 是 T + 北京时间到秒 + 6 位随机数，共 21 位（微信要求 6～32 位，支付宝 64 位以内）。
func newOrderNo(now time.Time) string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	return fmt.Sprintf("T%s%06d", now.In(time.FixedZone("CST", 8*3600)).Format("20060102150405"), n.Int64())
}

// newUUID 是 v4 UUID（小写），作为 App Store 的 appAccountToken。
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func ownerID(o dbq.Order) uint64 { return uint64(o.OwnerUserID.Int64) }

func (s *Service) view(ctx context.Context, o dbq.Order) (Order, error) {
	v := Order{OrderNo: o.OrderNo, Tier: string(o.Tier), Channel: string(o.Channel), AmountCents: int64(o.AmountCents), Status: string(o.Status),
		CreatedAt: o.CreatedAt, AppAccountToken: o.AppAccountToken.String}
	if o.PaidAt.Valid {
		t := o.PaidAt.Time
		v.PaidAt = &t
	}
	if o.Channel == dbq.OrdersChannelAppleIap {
		cfg, err := s.plans(ctx)
		if err != nil {
			return Order{}, err
		}
		v.AppleProductID = cfg[string(o.Tier)].AppleProductID
	}
	if o.Status == dbq.OrdersStatusPaid {
		if latest, err := s.q.GetLatestMembershipEnd(ctx, dbq.GetLatestMembershipEndParams{OwnerUserID: ownerID(o), EndsAt: s.now().UTC()}); err == nil {
			t := latest.EndsAt
			v.MembershipEndsAt = &t
		}
	}
	return v, nil
}

// CreateOrder 下单（6.5「立即开通」）。同一个幂等键返回同一个订单；冲刺卡、考季卡要求初试日期已配置。
func (s *Service) CreateOrder(ctx context.Context, userID uint64, tier string, channel pay.Channel, idemKey string) (Order, error) {
	on, err := s.paymentOn(ctx, userID)
	if err != nil {
		return Order{}, err
	}
	if !on {
		return Order{}, notFound()
	}
	if !hasChannel(s.d.Gateway.Channels(), channel) {
		return Order{}, apperr.New(apperr.BadRequest, "这个支付方式暂不可用").With("reason", "channel_off")
	}
	cfg, err := s.plans(ctx)
	if err != nil {
		return Order{}, err
	}
	plan, ok := cfg[tier]
	if _, known := tierOrder[tier]; !ok || !known || plan.Disabled || plan.PriceCents <= 0 {
		return Order{}, apperr.New(apperr.BadRequest, "这个会员档位暂未开放").With("reason", "plan_off")
	}
	now := s.now()
	if _, _, err := membership.Span(ctx, s.q, membership.Grant{UserID: userID, Tier: tier, Now: now}); errors.Is(err, membership.ErrNoExamDate) {
		return Order{}, apperr.New(apperr.Conflict, "今年的初试日期还没公布，这个档位暂时不能购买").With("reason", "no_exam_date")
	} else if err != nil {
		return Order{}, err
	}
	owner := sql.NullInt64{Int64: int64(userID), Valid: true}
	idem := sql.NullString{String: idemKey, Valid: idemKey != ""}
	if idem.Valid {
		if o, err := s.q.GetOrderByIdem(ctx, dbq.GetOrderByIdemParams{OwnerUserID: owner, IdempotencyKey: idem}); err == nil {
			return s.prepay(ctx, o, plan)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Order{}, err
		}
	}
	token := sql.NullString{}
	if channel == pay.ChannelApple {
		token = sql.NullString{String: newUUID(), Valid: true}
	}
	no := newOrderNo(now)
	id, err := s.q.InsertOrder(ctx, dbq.InsertOrderParams{OrderNo: no, OwnerUserID: owner, Tier: dbq.OrdersTier(tier), Channel: dbq.OrdersChannel(channel),
		AmountCents: uint32(plan.PriceCents), AppAccountToken: token, IdempotencyKey: idem, CreatedAt: now.UTC()})
	if err != nil {
		return Order{}, err
	}
	o, err := s.q.GetMyOrder(ctx, dbq.GetMyOrderParams{OrderNo: no, OwnerUserID: owner})
	if err != nil {
		return Order{}, fmt.Errorf("读取新订单 %d：%w", id, err)
	}
	return s.prepay(ctx, o, plan)
}

func hasChannel(cs []pay.Channel, c pay.Channel) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

// prepay 向渠道要调起支付的参数。同一订单号重复下单，微信、支付宝都返回同一笔交易，所以幂等重试可以再调一次。
func (s *Service) prepay(ctx context.Context, o dbq.Order, plan planConfig) (Order, error) {
	v, err := s.view(ctx, o)
	if err != nil || o.Status != dbq.OrdersStatusCreated {
		return v, err
	}
	p, err := s.d.Gateway.CreatePrepay(ctx, pay.Order{OrderNo: o.OrderNo, Channel: pay.Channel(o.Channel), AmountCents: int64(o.AmountCents),
		Subject: "考研Training " + plan.Name, NotifyURL: s.d.NotifyBaseURL + "/pay/notify/" + string(o.Channel)})
	if err != nil {
		s.d.Log.ErrorContext(ctx, "支付下单失败", "order_no", o.OrderNo, "channel", o.Channel, "err", err)
		return Order{}, apperr.New(apperr.Conflict, "下单失败，请稍后再试或换一种支付方式").With("reason", "prepay_failed")
	}
	v.Prepay = p.Params
	return v, nil
}

// GetOrder 返回自己的订单（6.6 轮询支付结果）。
func (s *Service) GetOrder(ctx context.Context, userID uint64, orderNo string) (Order, error) {
	o, err := s.q.GetMyOrder(ctx, dbq.GetMyOrderParams{OrderNo: orderNo, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, notFound()
	}
	if err != nil {
		return Order{}, err
	}
	return s.view(ctx, o)
}

// HandleNotify 处理微信、支付宝的异步通知：验签 → 开通。返回给支付平台的应答；应答成功后平台不再重发。
func (s *Service) HandleNotify(ctx context.Context, ch pay.Channel, body []byte, h http.Header) (int, string, []byte) {
	if s.d.Gateway == nil {
		return http.StatusNotFound, "text/plain", nil
	}
	n, err := s.d.Gateway.VerifyNotification(ctx, ch, body, h)
	if err != nil {
		s.d.Log.WarnContext(ctx, "支付回调验签失败", "channel", ch, "err", err)
		return s.d.Gateway.Ack(ch, false)
	}
	if !n.Paid {
		// 关闭、未支付等状态不处理，应答成功避免平台反复重发。
		return s.d.Gateway.Ack(ch, true)
	}
	sum := sha256.Sum256(body)
	if err := s.fulfil(ctx, n.OrderNo, ch, n.TransactionID, n.AmountCents, true, hex.EncodeToString(sum[:])); err != nil {
		s.d.Log.ErrorContext(ctx, "支付回调开通失败", "channel", ch, "order_no", n.OrderNo, "err", err)
		return s.d.Gateway.Ack(ch, false)
	}
	return s.d.Gateway.Ack(ch, true)
}

// errAmountMismatch 是回调金额与订单金额不一致：不开通，记日志等人工核对。
var errAmountMismatch = errors.New("支付金额与订单金额不一致")

// fulfil 在一个事务里开通：锁订单 → 已支付直接返回（重复回调）→ 核对渠道与金额 → 叠加会员 → 标记已支付 → 发消息。
func (s *Service) fulfil(ctx context.Context, orderNo string, ch pay.Channel, txn string, amount int64, checkAmount bool, notifyHash string) error {
	now := s.now()
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, orderNo)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("订单 %s 不存在", orderNo)
		}
		if err != nil {
			return err
		}
		if string(o.Channel) != string(ch) {
			return fmt.Errorf("订单渠道 %s 与回调渠道 %s 不一致", o.Channel, ch)
		}
		if o.Status != dbq.OrdersStatusCreated {
			if o.TransactionID.String != txn {
				// 同一订单被另一笔交易再次支付：不重复开通，记下来人工退款。
				s.d.Log.ErrorContext(ctx, "订单重复支付", "order_no", orderNo, "status", o.Status)
			}
			return nil
		}
		if checkAmount && amount != int64(o.AmountCents) {
			return errAmountMismatch
		}
		var mid sql.NullInt64
		if o.OwnerUserID.Valid {
			g, err := membership.Apply(ctx, q, membership.Grant{UserID: ownerID(o), Tier: string(o.Tier), Source: "order", SourceRef: o.ID, Now: now})
			if errors.Is(err, membership.ErrNoExamDate) {
				// 下单时校验过初试日期，到这里没有只可能是后台删了日期：先按月卡时长开通，避免付了钱没会员。
				g, err = membership.Apply(ctx, q, membership.Grant{UserID: ownerID(o), Tier: "monthly", Source: "order", SourceRef: o.ID, Now: now})
			}
			if err != nil {
				return err
			}
			mid = sql.NullInt64{Int64: int64(g.ID), Valid: true}
		}
		if _, err := q.MarkOrderPaid(ctx, dbq.MarkOrderPaidParams{TransactionID: sql.NullString{String: txn, Valid: txn != ""},
			NotifyHash: sql.NullString{String: notifyHash, Valid: notifyHash != ""}, MembershipID: mid, PaidAt: sql.NullTime{Time: now.UTC(), Valid: true}, ID: o.ID}); err != nil {
			return err
		}
		if !o.OwnerUserID.Valid {
			return nil
		}
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: ownerID(o), Mtype: dbq.MessagesMtypeMembership, Title: "会员已开通",
			Body: "支付成功，会员时长已叠加到你的会员里；会员不自动续费", DedupeKey: sql.NullString{String: "order:" + o.OrderNo, Valid: true}})
	})
}

// AppleVerify 校验 App Store 内购交易并开通（iOS 购买成功后 App 提交交易号）。交易必须属于本 App、商品与订单档位一致、
// appAccountToken 与订单一致、未被撤销；同一交易号只能开通一个订单。
func (s *Service) AppleVerify(ctx context.Context, userID uint64, orderNo, transactionID string) (Order, error) {
	on, err := s.paymentOn(ctx, userID)
	if err != nil {
		return Order{}, err
	}
	if !on {
		return Order{}, notFound()
	}
	o, err := s.q.GetMyOrder(ctx, dbq.GetMyOrderParams{OrderNo: orderNo, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, notFound()
	}
	if err != nil {
		return Order{}, err
	}
	if o.Channel != dbq.OrdersChannelAppleIap {
		return Order{}, apperr.New(apperr.BadRequest, "这不是 App Store 订单")
	}
	if o.Status != dbq.OrdersStatusCreated {
		return s.view(ctx, o)
	}
	tx, err := s.d.Gateway.AppleTransaction(ctx, transactionID)
	if errors.Is(err, pay.ErrNotFound) {
		return Order{}, apperr.New(apperr.BadRequest, "没有查到这笔购买，请稍后在会员中心点「恢复购买」").With("reason", "transaction_not_found")
	}
	if err != nil {
		return Order{}, err
	}
	cfg, err := s.plans(ctx)
	if err != nil {
		return Order{}, err
	}
	switch {
	case tx.BundleID != s.d.AppleBundleID:
		return Order{}, apperr.New(apperr.BadRequest, "购买凭证不属于本 App").With("reason", "bundle_mismatch")
	case tx.ProductID != cfg[string(o.Tier)].AppleProductID:
		return Order{}, apperr.New(apperr.BadRequest, "购买的商品与订单不一致").With("reason", "product_mismatch")
	case tx.AppAccountToken != o.AppAccountToken.String:
		return Order{}, apperr.New(apperr.BadRequest, "购买凭证与订单不一致").With("reason", "token_mismatch")
	case tx.Revoked:
		return Order{}, apperr.New(apperr.BadRequest, "这笔购买已退款或撤销").With("reason", "revoked")
	}
	if other, err := s.q.GetOrderByTransaction(ctx, dbq.GetOrderByTransactionParams{Channel: dbq.OrdersChannelAppleIap,
		TransactionID: sql.NullString{String: tx.TransactionID, Valid: true}}); err == nil && other.ID != o.ID {
		return Order{}, apperr.New(apperr.Conflict, "这笔购买已经开通过会员").With("reason", "transaction_used")
	}
	// 金额以 App Store 商品价格为准，不在这里核对（商品价格在 App Store Connect 里与 pricing 保持一致）。
	if err := s.fulfil(ctx, o.OrderNo, pay.ChannelApple, tx.TransactionID, 0, false, ""); err != nil {
		return Order{}, err
	}
	return s.GetOrder(ctx, userID, orderNo)
}

// Refund 全额退款并立即收回本单开通的会员（PRD 13.3）。由后台 7.3 调用（T28）；App Store 订单由用户向 Apple 申请，
// 这里只收回会员、记退款单。先调渠道退款（同一退款单号重试是幂等的），成功后再改库。
func (s *Service) Refund(ctx context.Context, orderNo, reason string, adminID uint64) error {
	o, err := s.q.GetOrderForUpdate(ctx, orderNo)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound()
	}
	if err != nil {
		return err
	}
	if o.Status != dbq.OrdersStatusPaid {
		return apperr.New(apperr.Conflict, "只有已支付的订单可以退款")
	}
	refundNo := "R" + o.OrderNo
	if o.Channel != dbq.OrdersChannelAppleIap {
		if err := s.d.Gateway.Refund(ctx, pay.RefundRequest{Channel: pay.Channel(o.Channel), OrderNo: o.OrderNo, RefundNo: refundNo,
			AmountCents: int64(o.AmountCents), TotalCents: int64(o.AmountCents), Reason: reason}); err != nil {
			return fmt.Errorf("渠道退款：%w", err)
		}
	}
	now := s.now().UTC()
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		o, err := q.GetOrderForUpdate(ctx, orderNo)
		if err != nil {
			return err
		}
		n, err := q.MarkOrderRefunded(ctx, o.ID)
		if err != nil || n == 0 {
			return err
		}
		if o.MembershipID.Valid && o.OwnerUserID.Valid {
			if err := q.RevokeMembership(ctx, dbq.RevokeMembershipParams{RevokedAt: sql.NullTime{Time: now, Valid: true}, ID: uint64(o.MembershipID.Int64),
				OwnerUserID: ownerID(o)}); err != nil {
				return err
			}
		}
		if _, err := q.InsertRefund(ctx, dbq.InsertRefundParams{OrderID: o.ID, RefundNo: sql.NullString{String: refundNo, Valid: true},
			AmountCents: sql.NullInt32{Int32: int32(o.AmountCents), Valid: true}, Reason: reason, HandledBy: sql.NullInt64{Int64: int64(adminID), Valid: adminID != 0},
			HandledAt: sql.NullTime{Time: now, Valid: true}, CreatedAt: now}); err != nil {
			return err
		}
		if !o.OwnerUserID.Valid {
			return nil
		}
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: ownerID(o), Mtype: dbq.MessagesMtypeMembership, Title: "退款已受理",
			Body: "订单已退款，本单开通的会员时长已收回；款项按原支付方式退回", DedupeKey: sql.NullString{String: "refund:" + o.OrderNo, Valid: true}})
	})
}

// MockNotify 构造一条 mock 渠道的支付成功回调（本地「模拟支付成功」入口与测试用）。网关不是 mock 时返回 false。
func (s *Service) MockNotify(ctx context.Context, userID uint64, orderNo string) (int, string, []byte, bool) {
	m, ok := s.d.Gateway.(*pay.Mock)
	if !ok {
		return 0, "", nil, false
	}
	o, err := s.q.GetMyOrder(ctx, dbq.GetMyOrderParams{OrderNo: orderNo, OwnerUserID: sql.NullInt64{Int64: int64(userID), Valid: true}})
	if err != nil {
		return http.StatusNotFound, "text/plain", nil, true
	}
	body := MockBody(o.OrderNo, "MOCK"+o.OrderNo, int64(o.AmountCents))
	h := http.Header{}
	h.Set("X-Mock-Signature", m.Sign(body))
	code, ct, b := s.HandleNotify(ctx, pay.Channel(o.Channel), body, h)
	return code, ct, b, true
}

// MockBody 是 mock 回调体。
func MockBody(orderNo, transactionID string, amountCents int64) []byte {
	b, _ := json.Marshal(pay.MockNotify{OrderNo: orderNo, TransactionID: transactionID, AmountCents: amountCents})
	return b
}
