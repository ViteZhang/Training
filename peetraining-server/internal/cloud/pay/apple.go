package pay

import (
	"context"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AppleConfig 是 App Store Server API 的密钥（APPLE_IAP_*，在 App Store Connect「用户和访问 → 集成」里生成）。
type AppleConfig struct {
	IssuerID   string
	KeyID      string
	PrivateKey string // .p8 私钥（PEM）
	BundleID   string
	// ProductionURL / SandboxURL 默认是 Apple 的正式与沙盒地址；测试时替换。
	ProductionURL string
	SandboxURL    string
}

// Apple 按交易号向 App Store Server API 查询交易。交易信息直接取自 Apple 的 HTTPS 接口，不经过 App，
// 所以不再校验 JWS 证书链（ADR 0011）。
type Apple struct {
	cfg  AppleConfig
	key  *ecdsa.PrivateKey
	http *http.Client
	now  func() time.Time
}

func NewApple(cfg AppleConfig) (*Apple, error) {
	if cfg.IssuerID == "" || cfg.KeyID == "" || cfg.BundleID == "" {
		return nil, errors.New("内购配置不完整（APPLE_IAP_ISSUER_ID、KEY_ID、BUNDLE_ID）")
	}
	k, err := jwt.ParseECPrivateKeyFromPEM([]byte(strings.ReplaceAll(cfg.PrivateKey, `\n`, "\n")))
	if err != nil {
		return nil, fmt.Errorf("内购私钥：%w", err)
	}
	if cfg.ProductionURL == "" {
		cfg.ProductionURL = "https://api.storekit.itunes.apple.com"
	}
	if cfg.SandboxURL == "" {
		cfg.SandboxURL = "https://api.storekit-sandbox.itunes.apple.com"
	}
	return &Apple{cfg: cfg, key: k, http: &http.Client{Timeout: 15 * time.Second}, now: time.Now}, nil
}

// token 是 App Store Server API 的 ES256 JWT（有效期 20 分钟以内）。
func (a *Apple) token() (string, error) {
	now := a.now()
	t := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": a.cfg.IssuerID, "iat": now.Unix(), "exp": now.Add(15 * time.Minute).Unix(), "aud": "appstoreconnect-v1", "bid": a.cfg.BundleID,
	})
	t.Header["kid"] = a.cfg.KeyID
	return t.SignedString(a.key)
}

// Transaction 先查正式环境，交易不存在时再查沙盒（TestFlight 与审核用沙盒）。
func (a *Apple) Transaction(ctx context.Context, transactionID string) (AppleTransaction, error) {
	tx, err := a.lookup(ctx, a.cfg.ProductionURL, transactionID)
	if errors.Is(err, ErrNotFound) {
		tx, err = a.lookup(ctx, a.cfg.SandboxURL, transactionID)
	}
	return tx, err
}

func (a *Apple) lookup(ctx context.Context, base, id string) (AppleTransaction, error) {
	tok, err := a.token()
	if err != nil {
		return AppleTransaction{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/inApps/v1/transactions/"+url.PathEscape(id), nil)
	if err != nil {
		return AppleTransaction{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := a.http.Do(req)
	if err != nil {
		return AppleTransaction{}, fmt.Errorf("内购查询请求：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return AppleTransaction{}, ErrNotFound
	}
	if resp.StatusCode >= 300 {
		return AppleTransaction{}, fmt.Errorf("内购查询返回 %d：%s", resp.StatusCode, raw)
	}
	var res struct {
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return AppleTransaction{}, err
	}
	return decodeJWSTransaction(res.SignedTransactionInfo)
}

// decodeJWSTransaction 取 JWS 的载荷（第二段）。
func decodeJWSTransaction(jws string) (AppleTransaction, error) {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 {
		return AppleTransaction{}, errors.New("内购交易格式错误")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AppleTransaction{}, err
	}
	var p struct {
		TransactionID         string `json:"transactionId"`
		OriginalTransactionID string `json:"originalTransactionId"`
		BundleID              string `json:"bundleId"`
		ProductID             string `json:"productId"`
		AppAccountToken       string `json:"appAccountToken"`
		PurchaseDate          int64  `json:"purchaseDate"`
		RevocationDate        int64  `json:"revocationDate"`
		Environment           string `json:"environment"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return AppleTransaction{}, err
	}
	return AppleTransaction{TransactionID: p.TransactionID, OriginalTransactionID: p.OriginalTransactionID, BundleID: p.BundleID, ProductID: p.ProductID,
		AppAccountToken: strings.ToLower(p.AppAccountToken), PurchaseDate: time.UnixMilli(p.PurchaseDate), Revoked: p.RevocationDate > 0, Environment: p.Environment}, nil
}
