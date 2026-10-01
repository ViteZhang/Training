// Package pay 对接微信支付、支付宝与 App Store 内购（默认关闭，T25）。
package pay

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Channel 是支付渠道。
type Channel string

const (
	ChannelWechat Channel = "wechat"
	ChannelAlipay Channel = "alipay"
	ChannelApple  Channel = "apple_iap"
)

// Order 是下单请求。金额以分为单位。
type Order struct {
	OrderNo     string
	Channel     Channel
	AmountCents int64
	Subject     string
}

// Prepay 是返回给 App 调起支付所需的参数。
type Prepay struct {
	Params map[string]string
}

// Notification 是验签通过后的支付结果。
type Notification struct {
	OrderNo       string
	TransactionID string
	AmountCents   int64
	Paid          bool
}

// Gateway 下单并验证回调。回调可能重复到达，幂等由调用方按 TransactionID 保证。
type Gateway interface {
	CreatePrepay(ctx context.Context, o Order) (Prepay, error)
	VerifyNotification(ctx context.Context, ch Channel, body []byte, headers map[string]string) (Notification, error)
}

// ErrInvalidSignature 表示回调验签失败。
var ErrInvalidSignature = errors.New("支付回调验签失败")

// Mock 下单直接返回固定参数；回调体格式为 "订单号|交易号|金额分"，签名头 X-Mock-Sign 必须为 ok。
type Mock struct{}

func NewMock() Mock { return Mock{} }

func (Mock) CreatePrepay(ctx context.Context, o Order) (Prepay, error) {
	if err := ctx.Err(); err != nil {
		return Prepay{}, err
	}
	return Prepay{Params: map[string]string{"mock_order_no": o.OrderNo, "channel": string(o.Channel)}}, nil
}

func (Mock) VerifyNotification(ctx context.Context, _ Channel, body []byte, headers map[string]string) (Notification, error) {
	if err := ctx.Err(); err != nil {
		return Notification{}, err
	}
	if headers["X-Mock-Sign"] != "ok" {
		return Notification{}, ErrInvalidSignature
	}
	parts := strings.Split(string(body), "|")
	if len(parts) != 3 {
		return Notification{}, fmt.Errorf("mock 回调格式错误：%w", ErrInvalidSignature)
	}
	var amount int64
	if _, err := fmt.Sscan(parts[2], &amount); err != nil {
		return Notification{}, fmt.Errorf("mock 回调金额错误：%w", ErrInvalidSignature)
	}
	return Notification{OrderNo: parts[0], TransactionID: parts[1], AmountCents: amount, Paid: true}, nil
}
