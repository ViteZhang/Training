package pay

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// WechatConfig 是微信支付 APIv3 的商户配置（全部来自环境变量 WECHAT_PAY_*）。
type WechatConfig struct {
	AppID      string
	MchID      string
	SerialNo   string // 商户 API 证书序列号
	PrivateKey string // 商户 API 私钥（PEM）
	APIv3Key   string // APIv3 密钥（32 字节），解密回调用
	// PlatformPublicKey 是微信支付平台公钥或平台证书（PEM），回调验签用；PlatformSerial 是它的序列号或公钥 ID。
	PlatformPublicKey string
	PlatformSerial    string
	BaseURL           string // 默认 https://api.mch.weixin.qq.com
}

// Wechat 是微信支付 App 支付。
type Wechat struct {
	cfg      WechatConfig
	key      *rsa.PrivateKey
	platform *rsa.PublicKey
	http     *http.Client
	now      func() time.Time
}

// notifyMaxSkew 是回调时间戳允许的偏差，超出视为重放。
const notifyMaxSkew = 5 * time.Minute

func NewWechat(cfg WechatConfig) (*Wechat, error) {
	if cfg.AppID == "" || cfg.MchID == "" || cfg.SerialNo == "" || len(cfg.APIv3Key) != 32 {
		return nil, errors.New("微信支付配置不完整（WECHAT_PAY_APP_ID、MCH_ID、SERIAL_NO、APIV3_KEY 32 字节）")
	}
	k, err := ParseRSAPrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("微信支付商户私钥：%w", err)
	}
	pk, err := ParseRSAPublicKey(cfg.PlatformPublicKey)
	if err != nil {
		return nil, fmt.Errorf("微信支付平台公钥：%w", err)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.mch.weixin.qq.com"
	}
	return &Wechat{cfg: cfg, key: k, platform: pk, http: &http.Client{Timeout: 15 * time.Second}, now: time.Now}, nil
}

func nonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// authorization 是 APIv3 请求签名：METHOD\nURL\n时间戳\n随机串\n请求体\n。
func (w *Wechat) authorization(method, path string, body []byte) (string, error) {
	ts, ns := strconv.FormatInt(w.now().Unix(), 10), nonce()
	sig, err := signSHA256(w.key, method+"\n"+path+"\n"+ts+"\n"+ns+"\n"+string(body)+"\n")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`, w.cfg.MchID, ns, sig, ts, w.cfg.SerialNo), nil
}

func (w *Wechat) call(ctx context.Context, method, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	auth, err := w.authorization(method, path, body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, w.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := w.http.Do(req)
	if err != nil {
		return fmt.Errorf("微信支付请求：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("微信支付返回 %d：%s", resp.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Prepay 下 App 支付单，并按 App 调起支付的格式签名（appid\n时间戳\n随机串\nprepay_id\n）。
func (w *Wechat) Prepay(ctx context.Context, o Order) (Prepay, error) {
	var res struct {
		PrepayID string `json:"prepay_id"`
	}
	err := w.call(ctx, http.MethodPost, "/v3/pay/transactions/app", map[string]any{
		"appid": w.cfg.AppID, "mchid": w.cfg.MchID, "description": o.Subject, "out_trade_no": o.OrderNo, "notify_url": o.NotifyURL,
		"amount": map[string]any{"total": o.AmountCents, "currency": "CNY"},
	}, &res)
	if err != nil {
		return Prepay{}, err
	}
	ts, ns := strconv.FormatInt(w.now().Unix(), 10), nonce()
	sig, err := signSHA256(w.key, w.cfg.AppID+"\n"+ts+"\n"+ns+"\n"+res.PrepayID+"\n")
	if err != nil {
		return Prepay{}, err
	}
	return Prepay{Params: map[string]string{"appid": w.cfg.AppID, "partnerid": w.cfg.MchID, "prepayid": res.PrepayID, "package": "Sign=WXPay",
		"noncestr": ns, "timestamp": ts, "sign": sig}}, nil
}

// Verify 校验回调：平台公钥验签（时间戳\n随机串\n报文\n）、时间戳防重放，再用 APIv3 密钥 AES-256-GCM 解密资源。
func (w *Wechat) Verify(body []byte, h http.Header) (Notification, error) {
	ts := h.Get("Wechatpay-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return Notification{}, ErrInvalidSignature
	}
	if d := w.now().Sub(time.Unix(sec, 0)); d > notifyMaxSkew || d < -notifyMaxSkew {
		return Notification{}, fmt.Errorf("%w：时间戳过期", ErrInvalidSignature)
	}
	if w.cfg.PlatformSerial != "" && h.Get("Wechatpay-Serial") != "" && h.Get("Wechatpay-Serial") != w.cfg.PlatformSerial {
		return Notification{}, fmt.Errorf("%w：平台证书序列号不匹配", ErrInvalidSignature)
	}
	if err := verifySHA256(w.platform, ts+"\n"+h.Get("Wechatpay-Nonce")+"\n"+string(body)+"\n", h.Get("Wechatpay-Signature")); err != nil {
		return Notification{}, err
	}
	var env struct {
		EventType string `json:"event_type"`
		Resource  struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			AssociatedData string `json:"associated_data"`
			Nonce          string `json:"nonce"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return Notification{}, fmt.Errorf("%w：回调格式错误", ErrInvalidSignature)
	}
	plain, err := decryptAESGCM([]byte(w.cfg.APIv3Key), env.Resource.Nonce, env.Resource.AssociatedData, env.Resource.Ciphertext)
	if err != nil {
		return Notification{}, fmt.Errorf("%w：解密失败", ErrInvalidSignature)
	}
	var tx struct {
		AppID         string `json:"appid"`
		MchID         string `json:"mchid"`
		OutTradeNo    string `json:"out_trade_no"`
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		Amount        struct {
			Total int64 `json:"total"`
		} `json:"amount"`
	}
	if err := json.Unmarshal(plain, &tx); err != nil {
		return Notification{}, fmt.Errorf("%w：资源格式错误", ErrInvalidSignature)
	}
	if tx.MchID != w.cfg.MchID || tx.AppID != w.cfg.AppID {
		return Notification{}, fmt.Errorf("%w：商户不匹配", ErrInvalidSignature)
	}
	return Notification{OrderNo: tx.OutTradeNo, TransactionID: tx.TransactionID, AmountCents: tx.Amount.Total, Paid: tx.TradeState == "SUCCESS"}, nil
}

func decryptAESGCM(key []byte, nonce, aad, ciphertextB64 string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("nonce 长度不对")
	}
	return gcm.Open(nil, []byte(nonce), ct, []byte(aad))
}

// Refund 申请退款（同步返回受理结果，到账由微信异步完成）。
func (w *Wechat) Refund(ctx context.Context, r RefundRequest) error {
	return w.call(ctx, http.MethodPost, "/v3/refund/domestic/refunds", map[string]any{
		"out_trade_no": r.OrderNo, "out_refund_no": r.RefundNo, "reason": r.Reason,
		"amount": map[string]any{"refund": r.AmountCents, "total": r.TotalCents, "currency": "CNY"},
	}, nil)
}
