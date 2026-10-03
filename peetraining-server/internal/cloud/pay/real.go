package pay

import (
	"context"
	"net/http"
)

// Real 组合已配置的真实渠道；没配置的渠道不可用（下单返回 ErrChannelOff）。
type Real struct {
	Wechat *Wechat
	Alipay *Alipay
	Apple  *Apple
}

func (r *Real) Channels() []Channel {
	var out []Channel
	if r.Wechat != nil {
		out = append(out, ChannelWechat)
	}
	if r.Alipay != nil {
		out = append(out, ChannelAlipay)
	}
	if r.Apple != nil {
		out = append(out, ChannelApple)
	}
	return out
}

func (r *Real) CreatePrepay(ctx context.Context, o Order) (Prepay, error) {
	switch {
	case o.Channel == ChannelWechat && r.Wechat != nil:
		return r.Wechat.Prepay(ctx, o)
	case o.Channel == ChannelAlipay && r.Alipay != nil:
		return r.Alipay.Prepay(ctx, o)
	case o.Channel == ChannelApple && r.Apple != nil:
		// 内购由 StoreKit 发起，服务端只记订单；App 购买时把 appAccountToken 带上。
		return Prepay{Params: map[string]string{}}, nil
	}
	return Prepay{}, ErrChannelOff
}

func (r *Real) VerifyNotification(_ context.Context, ch Channel, body []byte, h http.Header) (Notification, error) {
	switch {
	case ch == ChannelWechat && r.Wechat != nil:
		return r.Wechat.Verify(body, h)
	case ch == ChannelAlipay && r.Alipay != nil:
		return r.Alipay.Verify(body)
	}
	return Notification{}, ErrChannelOff
}

func (r *Real) Ack(ch Channel, ok bool) (int, string, []byte) { return ack(ch, ok) }

func (r *Real) Refund(ctx context.Context, req RefundRequest) error {
	switch {
	case req.Channel == ChannelWechat && r.Wechat != nil:
		return r.Wechat.Refund(ctx, req)
	case req.Channel == ChannelAlipay && r.Alipay != nil:
		return r.Alipay.Refund(ctx, req)
	}
	// App Store 的退款由用户向 Apple 申请，Apple 处理后通知（ADR 0011）。
	return ErrChannelOff
}

func (r *Real) AppleTransaction(ctx context.Context, id string) (AppleTransaction, error) {
	if r.Apple == nil {
		return AppleTransaction{}, ErrChannelOff
	}
	return r.Apple.Transaction(ctx, id)
}
