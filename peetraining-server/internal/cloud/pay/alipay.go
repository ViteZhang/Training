package pay

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AlipayConfig 是支付宝开放平台应用配置（ALIPAY_*）。
type AlipayConfig struct {
	AppID      string
	PrivateKey string // 应用私钥（RSA2）
	PublicKey  string // 支付宝公钥（验签用）
	Gateway    string // 默认 https://openapi.alipay.com/gateway.do
}

// Alipay 是支付宝 App 支付。
type Alipay struct {
	cfg  AlipayConfig
	key  *rsa.PrivateKey
	pub  *rsa.PublicKey
	http *http.Client
	now  func() time.Time
}

func NewAlipay(cfg AlipayConfig) (*Alipay, error) {
	if cfg.AppID == "" {
		return nil, errors.New("支付宝配置不完整（ALIPAY_APP_ID）")
	}
	k, err := ParseRSAPrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("支付宝应用私钥：%w", err)
	}
	pk, err := ParseRSAPublicKey(cfg.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("支付宝公钥：%w", err)
	}
	if cfg.Gateway == "" {
		cfg.Gateway = "https://openapi.alipay.com/gateway.do"
	}
	return &Alipay{cfg: cfg, key: k, pub: pk, http: &http.Client{Timeout: 15 * time.Second}, now: time.Now}, nil
}

var cst = time.FixedZone("CST", 8*3600)

// signContent 是待签名串：除 sign（与通知里的 sign_type）外的非空参数按键排序，用 & 连接 k=v（值不编码）。
func signContent(params map[string]string, skipSignType bool) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || (skipSignType && k == "sign_type") || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + params[k]
	}
	return strings.Join(parts, "&")
}

func yuan(cents int64) string { return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64) }

func (a *Alipay) common(method string, biz any, notifyURL string) (map[string]string, error) {
	b, err := json.Marshal(biz)
	if err != nil {
		return nil, err
	}
	p := map[string]string{"app_id": a.cfg.AppID, "method": method, "format": "JSON", "charset": "utf-8", "sign_type": "RSA2",
		"timestamp": a.now().In(cst).Format("2006-01-02 15:04:05"), "version": "1.0", "biz_content": string(b)}
	if notifyURL != "" {
		p["notify_url"] = notifyURL
	}
	sig, err := signSHA256(a.key, signContent(p, false))
	if err != nil {
		return nil, err
	}
	p["sign"] = sig
	return p, nil
}

// Prepay 生成 App 支付的 orderString（App 直接交给支付宝 SDK）。
func (a *Alipay) Prepay(_ context.Context, o Order) (Prepay, error) {
	p, err := a.common("alipay.trade.app.pay", map[string]string{"out_trade_no": o.OrderNo, "total_amount": yuan(o.AmountCents), "subject": o.Subject,
		"product_code": "QUICK_MSECURITY_PAY", "timeout_express": "30m"}, o.NotifyURL)
	if err != nil {
		return Prepay{}, err
	}
	v := url.Values{}
	for k, s := range p {
		v.Set(k, s)
	}
	return Prepay{Params: map[string]string{"order_string": v.Encode()}}, nil
}

// Verify 校验异步通知：支付宝公钥验签（去掉 sign 与 sign_type），核对 app_id。
func (a *Alipay) Verify(body []byte) (Notification, error) {
	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return Notification{}, fmt.Errorf("%w：通知格式错误", ErrInvalidSignature)
	}
	p := map[string]string{}
	for k := range vals {
		p[k] = vals.Get(k)
	}
	if err := verifySHA256(a.pub, signContent(p, true), p["sign"]); err != nil {
		return Notification{}, err
	}
	if p["app_id"] != a.cfg.AppID {
		return Notification{}, fmt.Errorf("%w：应用不匹配", ErrInvalidSignature)
	}
	amount, err := strconv.ParseFloat(p["total_amount"], 64)
	if err != nil {
		return Notification{}, fmt.Errorf("%w：金额格式错误", ErrInvalidSignature)
	}
	st := p["trade_status"]
	return Notification{OrderNo: p["out_trade_no"], TransactionID: p["trade_no"], AmountCents: int64(math.Round(amount * 100)),
		Paid: st == "TRADE_SUCCESS" || st == "TRADE_FINISHED"}, nil
}

// Refund 调 alipay.trade.refund（同步返回结果）。
func (a *Alipay) Refund(ctx context.Context, r RefundRequest) error {
	p, err := a.common("alipay.trade.refund", map[string]string{"out_trade_no": r.OrderNo, "refund_amount": yuan(r.AmountCents),
		"out_request_no": r.RefundNo, "refund_reason": r.Reason}, "")
	if err != nil {
		return err
	}
	form := url.Values{}
	for k, s := range p {
		form.Set(k, s)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.Gateway, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("支付宝请求：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var res struct {
		Resp struct {
			Code    string `json:"code"`
			Msg     string `json:"msg"`
			SubMsg  string `json:"sub_msg"`
			SubCode string `json:"sub_code"`
		} `json:"alipay_trade_refund_response"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("支付宝返回格式错误：%w", err)
	}
	if res.Resp.Code != "10000" {
		return fmt.Errorf("支付宝退款失败：%s %s", res.Resp.SubCode, res.Resp.SubMsg)
	}
	return nil
}
