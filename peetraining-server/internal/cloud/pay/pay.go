// Package pay 对接微信支付、支付宝与 App Store 内购（默认关闭，T25，ADR 0011）。
//
// 微信支付用 APIv3（RSA-SHA256 请求签名、回调用平台公钥验签、AES-256-GCM 解密）；支付宝用 RSA2（App 支付 orderString、
// 异步通知验签）；App Store 用 App Store Server API 按交易号查询（ES256 JWT 鉴权，交易信息直接取自 Apple 的 HTTPS 接口）。
// 只用标准库与已有的 golang-jwt，不引入第三方支付 SDK。
package pay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
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
	// NotifyURL 是异步通知地址（微信、支付宝）。
	NotifyURL string
}

// Prepay 是返回给 App 调起支付所需的参数（微信：appid、partnerid、prepayid 等；支付宝：order_string）。
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

// RefundRequest 是退款请求。
type RefundRequest struct {
	Channel     Channel
	OrderNo     string
	RefundNo    string
	AmountCents int64
	TotalCents  int64
	Reason      string
}

// AppleTransaction 是 App Store 返回的一笔交易（从 Apple 的 HTTPS 接口取得）。
type AppleTransaction struct {
	TransactionID         string
	OriginalTransactionID string
	BundleID              string
	ProductID             string
	AppAccountToken       string
	PurchaseDate          time.Time
	Revoked               bool
	Environment           string
}

// Gateway 下单、验证回调、退款、查询内购交易。回调可能重复到达，幂等由调用方按订单状态与交易号保证。
type Gateway interface {
	// Channels 是已配置可用的渠道。
	Channels() []Channel
	CreatePrepay(ctx context.Context, o Order) (Prepay, error)
	VerifyNotification(ctx context.Context, ch Channel, body []byte, headers http.Header) (Notification, error)
	// Ack 是回复给支付平台的应答：成功时平台不再重发。
	Ack(ch Channel, ok bool) (status int, contentType string, body []byte)
	Refund(ctx context.Context, r RefundRequest) error
	AppleTransaction(ctx context.Context, transactionID string) (AppleTransaction, error)
}

// 错误。
var (
	ErrInvalidSignature = errors.New("支付回调验签失败")
	ErrChannelOff       = errors.New("这个支付渠道没有配置")
	ErrNotFound         = errors.New("交易不存在")
)

// ack 是各渠道的标准应答。
func ack(ch Channel, ok bool) (int, string, []byte) {
	switch ch {
	case ChannelAlipay:
		if ok {
			return http.StatusOK, "text/plain", []byte("success")
		}
		return http.StatusOK, "text/plain", []byte("fail")
	default:
		if ok {
			return http.StatusOK, "application/json", []byte(`{"code":"SUCCESS","message":"成功"}`)
		}
		return http.StatusInternalServerError, "application/json", []byte(`{"code":"FAIL","message":"失败"}`)
	}
}

// Mock 是本地与测试用的渠道：回调体是 JSON {order_no, transaction_id, amount_cents}，签名头 X-Mock-Signature 为
// HMAC-SHA256(secret, body)。内购交易号格式为「商品 ID|appAccountToken|序号」。生产环境不使用（cloud.New 不创建）。
type Mock struct {
	secret []byte
}

func NewMock(secret string) *Mock { return &Mock{secret: []byte("mock-pay:" + secret)} }

// Sign 计算 mock 回调签名（本地「模拟支付成功」入口与测试用）。
func (m *Mock) Sign(body []byte) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// MockNotify 是 mock 回调体。
type MockNotify struct {
	OrderNo       string `json:"order_no"`
	TransactionID string `json:"transaction_id"`
	AmountCents   int64  `json:"amount_cents"`
}

func (m *Mock) Channels() []Channel { return []Channel{ChannelWechat, ChannelAlipay, ChannelApple} }

func (m *Mock) CreatePrepay(ctx context.Context, o Order) (Prepay, error) {
	if err := ctx.Err(); err != nil {
		return Prepay{}, err
	}
	return Prepay{Params: map[string]string{"mock": "1", "order_no": o.OrderNo, "channel": string(o.Channel)}}, nil
}

func (m *Mock) VerifyNotification(ctx context.Context, _ Channel, body []byte, headers http.Header) (Notification, error) {
	if err := ctx.Err(); err != nil {
		return Notification{}, err
	}
	if !hmac.Equal([]byte(headers.Get("X-Mock-Signature")), []byte(m.Sign(body))) {
		return Notification{}, ErrInvalidSignature
	}
	var n MockNotify
	if err := json.Unmarshal(body, &n); err != nil || n.OrderNo == "" || n.TransactionID == "" {
		return Notification{}, fmt.Errorf("mock 回调格式错误：%w", ErrInvalidSignature)
	}
	return Notification{OrderNo: n.OrderNo, TransactionID: n.TransactionID, AmountCents: n.AmountCents, Paid: true}, nil
}

func (m *Mock) Ack(ch Channel, ok bool) (int, string, []byte) { return ack(ch, ok) }

func (m *Mock) Refund(ctx context.Context, _ RefundRequest) error { return ctx.Err() }

func (m *Mock) AppleTransaction(ctx context.Context, transactionID string) (AppleTransaction, error) {
	if err := ctx.Err(); err != nil {
		return AppleTransaction{}, err
	}
	parts := strings.Split(transactionID, "|")
	if len(parts) != 3 {
		return AppleTransaction{}, ErrNotFound
	}
	// 真实交易号是十几位数字；mock 的输入比 orders.transaction_id 长，取哈希缩短。
	sum := sha256.Sum256([]byte(transactionID))
	id := "MOCK" + hex.EncodeToString(sum[:8])
	return AppleTransaction{TransactionID: id, OriginalTransactionID: id, BundleID: "cn.dreamelab.training", ProductID: parts[0],
		AppAccountToken: parts[1], PurchaseDate: time.Now(), Environment: "Sandbox"}, nil
}
