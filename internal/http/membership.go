package http

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"peetraining-server/internal/cloud/pay"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/payment"
	"peetraining-server/internal/quota"
)

// 会员中心与在线支付（T25）：6.5 会员中心、6.6 支付结果、支付平台回调。

func init() {
	// 在线支付开关关闭时下单与订单接口返回 404（ADR 0009）。会员中心不拦：关闭时 App 只显示兑换码入口；
	// 支付回调没有登录用户，也不拦（开关只对部分用户打开时，他们的订单回调同样要处理）。
	flagGuards["POST "+APIPrefix+"/orders"] = "online_payment"
	flagGuards["GET "+APIPrefix+"/orders/:orderNo"] = "online_payment"
	flagGuards["POST "+APIPrefix+"/orders/:orderNo/apple-verify"] = "online_payment"
}

func toGenQuotaRule(r quota.Rule) gen.QuotaRule {
	out := gen.QuotaRule{Period: gen.QuotaRulePeriod(r.Period)}
	if r.Limit == nil {
		out.Limit = nullable.NewNullNullable[int]()
	} else {
		out.Limit = nullable.NewNullableWithValue(*r.Limit)
	}
	return out
}

func (h *Handlers) GetMembershipCenter(c *gin.Context) {
	ctx := c.Request.Context()
	ct, err := h.deps.Payment.Center(ctx, currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	me, err := h.deps.Auth.GetMe(ctx, currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.MembershipCenter{PaymentEnabled: ct.PaymentEnabled, Channels: []gen.PayChannel{}, Plans: make([]gen.MembershipPlan, len(ct.Plans)),
		Benefits: make([]gen.MembershipBenefit, len(ct.Benefits)), Membership: gen.MembershipStatus{IsMember: me.Membership.IsMember}}
	if me.Membership.IsMember {
		out.Membership.Tier = ptr(gen.MembershipStatusTier(me.Membership.Tier))
		out.Membership.EndsAt = ptr(me.Membership.EndsAt)
	}
	for _, ch := range ct.Channels {
		out.Channels = append(out.Channels, gen.PayChannel(ch))
	}
	for i, p := range ct.Plans {
		g := gen.MembershipPlan{Tier: gen.PlanTier(p.Tier), Name: p.Name, PriceCents: p.PriceCents, Recommended: p.Recommended, Available: p.Available}
		if p.AppleProductID != "" {
			g.AppleProductId = ptr(p.AppleProductID)
		}
		if !p.EndsAt.IsZero() {
			g.EndsAt = ptr(p.EndsAt)
		}
		if p.UnavailableReason != "" {
			g.UnavailableReason = ptr(p.UnavailableReason)
		}
		out.Plans[i] = g
	}
	for i, b := range ct.Benefits {
		out.Benefits[i] = gen.MembershipBenefit{QuotaType: gen.QuotaType(b.Type), Name: b.Name, Free: toGenQuotaRule(b.Free), Member: toGenQuotaRule(b.Member)}
	}
	c.JSON(http.StatusOK, out)
}

func toGenOrder(o payment.Order) gen.Order {
	out := gen.Order{OrderNo: o.OrderNo, Tier: gen.PlanTier(o.Tier), Channel: gen.PayChannel(o.Channel), AmountCents: o.AmountCents,
		Status: gen.OrderStatus(o.Status), CreatedAt: o.CreatedAt, PaidAt: o.PaidAt, MembershipEndsAt: o.MembershipEndsAt}
	if o.Prepay != nil {
		out.Prepay = &o.Prepay
	}
	if o.AppleProductID != "" {
		out.AppleProductId = ptr(o.AppleProductID)
	}
	if o.AppAccountToken != "" {
		out.AppAccountToken = ptr(o.AppAccountToken)
	}
	return out
}

func (h *Handlers) CreateOrder(c *gin.Context) {
	var body gen.CreateOrderJSONBody
	if !bind(c, &body) {
		return
	}
	key := ""
	if body.IdempotencyKey != nil {
		key = *body.IdempotencyKey
	}
	o, err := h.deps.Payment.CreateOrder(c.Request.Context(), currentUser(c), string(body.Tier), pay.Channel(body.Channel), key)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenOrder(o))
}

func (h *Handlers) GetOrder(c *gin.Context, orderNo string) {
	o, err := h.deps.Payment.GetOrder(c.Request.Context(), currentUser(c), orderNo)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenOrder(o))
}

func (h *Handlers) VerifyAppleOrder(c *gin.Context, orderNo string) {
	var body gen.VerifyAppleOrderJSONBody
	if !bind(c, &body) {
		return
	}
	o, err := h.deps.Payment.AppleVerify(c.Request.Context(), currentUser(c), orderNo, body.TransactionId)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenOrder(o))
}

// PayNotify 把原始报文与请求头交给渠道验签（签名覆盖原文，不能先解析再序列化），应答按渠道格式原样返回。
func (h *Handlers) PayNotify(c *gin.Context, channel gen.PayNotifyParamsChannel) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 64<<10))
	if err != nil {
		_ = c.Error(ErrBadRequest("读取回调失败").Wrap(err))
		return
	}
	code, ct, b := h.deps.Payment.HandleNotify(c.Request.Context(), pay.Channel(channel), body, c.Request.Header)
	if code == http.StatusNotFound {
		_ = c.Error(ErrNotFound())
		return
	}
	c.Data(code, ct, b)
}

// devMockPay 是本地「模拟支付成功」入口：mock 渠道下替用户的订单发一条签名回调（只在非生产环境注册）。
// 前面要挂 Identify：只能替自己的订单模拟。
func devMockPay(svc *payment.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if currentUser(c) == 0 {
			_ = c.Error(ErrUnauthorized())
			return
		}
		code, ct, b, ok := svc.MockNotify(c.Request.Context(), currentUser(c), c.Param("orderNo"))
		if !ok {
			_ = c.Error(ErrNotFound())
			return
		}
		c.Data(code, ct, b)
	}
}
