package pay

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func rsaPEM(t *testing.T) (*rsa.PrivateKey, string, string) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	pub, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return k, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub}))
}

func TestMockSignature(t *testing.T) {
	m := NewMock("secret")
	body, _ := json.Marshal(MockNotify{OrderNo: "T1", TransactionID: "X1", AmountCents: 2990})
	h := http.Header{}
	h.Set("X-Mock-Signature", m.Sign(body))
	n, err := m.VerifyNotification(context.Background(), ChannelWechat, body, h)
	if err != nil || n.OrderNo != "T1" || n.AmountCents != 2990 || !n.Paid {
		t.Fatalf("mock 回调：%+v %v", n, err)
	}
	h.Set("X-Mock-Signature", NewMock("other").Sign(body))
	if _, err := m.VerifyNotification(context.Background(), ChannelWechat, body, h); !errors.Is(err, ErrInvalidSignature) {
		t.Error("别的密钥签的回调应拒绝")
	}
}

func TestWechat(t *testing.T) {
	_, merchantPEM, merchantPub := rsaPEM(t)
	platform, _, platformPub := rsaPEM(t)
	apiKey := strings.Repeat("k", 32)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v3/pay/transactions/app":
			_, _ = w.Write([]byte(`{"prepay_id":"wx123"}`))
		case "/v3/refund/domestic/refunds":
			_, _ = w.Write([]byte(`{"status":"PROCESSING"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	wc, err := NewWechat(WechatConfig{AppID: "wxapp", MchID: "1900", SerialNo: "SER", PrivateKey: merchantPEM, APIv3Key: apiKey, PlatformPublicKey: platformPub,
		PlatformSerial: "PSER", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	wc.now = func() time.Time { return now }

	p, err := wc.Prepay(context.Background(), Order{OrderNo: "T1", AmountCents: 5900, Subject: "冲刺卡", NotifyURL: "https://x/notify"})
	if err != nil || p.Params["prepayid"] != "wx123" || p.Params["package"] != "Sign=WXPay" {
		t.Fatalf("下单：%+v %v", p, err)
	}
	if !strings.HasPrefix(gotAuth, `WECHATPAY2-SHA256-RSA2048 mchid="1900"`) {
		t.Errorf("请求签名头：%s", gotAuth)
	}
	// App 调起参数的签名可以用商户公钥验证。
	mpub, _ := ParseRSAPublicKey(merchantPub)
	if err := verifySHA256(mpub, "wxapp\n"+p.Params["timestamp"]+"\n"+p.Params["noncestr"]+"\nwx123\n", p.Params["sign"]); err != nil {
		t.Errorf("App 调起签名：%v", err)
	}

	// 构造一条平台签名、AES-GCM 加密的回调。
	plain := `{"appid":"wxapp","mchid":"1900","out_trade_no":"T1","transaction_id":"4200","trade_state":"SUCCESS","amount":{"total":5900}}`
	block, _ := aes.NewCipher([]byte(apiKey))
	gcm, _ := cipher.NewGCM(block)
	nonceStr := "abcdefghijkl"
	ct := gcm.Seal(nil, []byte(nonceStr), []byte(plain), []byte("transaction"))
	body := `{"event_type":"TRANSACTION.SUCCESS","resource":{"algorithm":"AEAD_AES_256_GCM","ciphertext":"` + base64.StdEncoding.EncodeToString(ct) +
		`","associated_data":"transaction","nonce":"` + nonceStr + `"}}`
	ts := strconv.FormatInt(now.Unix(), 10)
	sig, _ := signSHA256(platform, ts+"\nN1\n"+body+"\n")
	h := http.Header{}
	h.Set("Wechatpay-Timestamp", ts)
	h.Set("Wechatpay-Nonce", "N1")
	h.Set("Wechatpay-Signature", sig)
	h.Set("Wechatpay-Serial", "PSER")
	n, err := wc.Verify([]byte(body), h)
	if err != nil || n.OrderNo != "T1" || n.TransactionID != "4200" || n.AmountCents != 5900 || !n.Paid {
		t.Fatalf("回调：%+v %v", n, err)
	}
	// 篡改报文、过期时间戳都拒绝。
	if _, err := wc.Verify([]byte(strings.Replace(body, "SUCCESS", "SUCCESX", 1)), h); !errors.Is(err, ErrInvalidSignature) {
		t.Error("篡改报文应拒绝")
	}
	wc.now = func() time.Time { return now.Add(10 * time.Minute) }
	if _, err := wc.Verify([]byte(body), h); !errors.Is(err, ErrInvalidSignature) {
		t.Error("过期时间戳应拒绝（防重放）")
	}
	wc.now = func() time.Time { return now }
	if err := wc.Refund(context.Background(), RefundRequest{OrderNo: "T1", RefundNo: "R1", AmountCents: 5900, TotalCents: 5900, Reason: "测试"}); err != nil {
		t.Errorf("退款：%v", err)
	}
}

func TestAlipay(t *testing.T) {
	_, appPEM, appPub := rsaPEM(t)
	ali, _, aliPub := rsaPEM(t)
	a, err := NewAlipay(AlipayConfig{AppID: "2021", PrivateKey: appPEM, PublicKey: aliPub})
	if err != nil {
		t.Fatal(err)
	}
	p, err := a.Prepay(context.Background(), Order{OrderNo: "T2", AmountCents: 16900, Subject: "考季卡", NotifyURL: "https://x/n"})
	if err != nil {
		t.Fatal(err)
	}
	vals, _ := url.ParseQuery(p.Params["order_string"])
	if vals.Get("method") != "alipay.trade.app.pay" || !strings.Contains(vals.Get("biz_content"), `"total_amount":"169.00"`) {
		t.Errorf("orderString：%v", vals)
	}
	pub, _ := ParseRSAPublicKey(appPub)
	params := map[string]string{}
	for k := range vals {
		params[k] = vals.Get(k)
	}
	if err := verifySHA256(pub, signContent(params, false), params["sign"]); err != nil {
		t.Errorf("orderString 签名：%v", err)
	}

	notify := map[string]string{"app_id": "2021", "out_trade_no": "T2", "trade_no": "2026100322001", "total_amount": "169.00", "trade_status": "TRADE_SUCCESS",
		"notify_id": "n1", "sign_type": "RSA2"}
	sig, _ := signSHA256(ali, signContent(notify, true))
	form := url.Values{}
	for k, v := range notify {
		form.Set(k, v)
	}
	form.Set("sign", sig)
	n, err := a.Verify([]byte(form.Encode()))
	if err != nil || n.OrderNo != "T2" || n.AmountCents != 16900 || !n.Paid {
		t.Fatalf("通知：%+v %v", n, err)
	}
	form.Set("total_amount", "0.01")
	if _, err := a.Verify([]byte(form.Encode())); !errors.Is(err, ErrInvalidSignature) {
		t.Error("改金额后验签应失败")
	}
	if code, _, b := ack(ChannelAlipay, true); code != 200 || string(b) != "success" {
		t.Errorf("支付宝应答：%d %s", code, b)
	}
}

func TestApple(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	payload, _ := json.Marshal(map[string]any{"transactionId": "2000", "originalTransactionId": "2000", "bundleId": "cn.dreamelab.training",
		"productId": "cn.dreamelab.training.monthly", "appAccountToken": "6F9619FF-8B86-D011-B42D-00C04FC964FF", "purchaseDate": 1_800_000_000_000, "environment": "Sandbox"})
	jws := "eyJhbGciOiJFUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	var gotToken string
	prod := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404) }))
	defer prod.Close()
	sandbox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.URL.Path != "/inApps/v1/transactions/2000" {
			w.WriteHeader(404)
			return
		}
		_, _ = io.WriteString(w, `{"signedTransactionInfo":"`+jws+`"}`)
	}))
	defer sandbox.Close()
	a, err := NewApple(AppleConfig{IssuerID: "iss", KeyID: "KID", PrivateKey: keyPEM, BundleID: "cn.dreamelab.training", ProductionURL: prod.URL, SandboxURL: sandbox.URL})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := a.Transaction(context.Background(), "2000")
	if err != nil || tx.ProductID != "cn.dreamelab.training.monthly" || tx.AppAccountToken != "6f9619ff-8b86-d011-b42d-00c04fc964ff" || tx.Revoked {
		t.Fatalf("正式环境查不到时查沙盒：%+v %v", tx, err)
	}
	parsed, err := jwt.Parse(gotToken, func(*jwt.Token) (any, error) { return &k.PublicKey, nil }, jwt.WithAudience("appstoreconnect-v1"))
	if err != nil || parsed.Header["kid"] != "KID" {
		t.Errorf("ES256 JWT：%v", err)
	}
	if _, err := a.Transaction(context.Background(), "9999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的交易：%v", err)
	}
}
