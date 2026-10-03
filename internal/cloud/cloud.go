// Package cloud 封装短信、AI、OCR、语音、内容安全、支付等外部服务。
//
// 每个子包定义接口与 mock 实现；真实实现在对应任务卡里加（短信 T06、OCR T09、AI T10、
// 内容安全 T08、语音 T20、支付 T25）。按 *_PROVIDER 环境变量切换，本地默认 mock，
// 不需要任何真实密钥就能启动（T01 验收）。
package cloud

import (
	"errors"
	"fmt"

	"peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/asr"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/cloud/pay"
	"peetraining-server/internal/cloud/sms"
	"peetraining-server/internal/config"
)

// Clients 汇总全部外部服务客户端。
type Clients struct {
	SMS sms.Sender
	AI  ai.Client
	// AIFallback 是主用平台失败时的备用（AI_PROVIDER=relay 且配了百炼时为百炼，否则为空）。
	AIFallback ai.Client
	// AIAlt 是对照平台（可为空）。
	AIAlt      ai.Client
	OCR        ocr.Recognizer
	PDF        ocr.PDFParser
	ASR        asr.Transcriber
	Moderation moderation.Checker
	Pay        pay.Gateway
	OSS        oss.Store
}

// ErrUnknownProvider 表示 *_PROVIDER 配置了还没实现的服务商。
var ErrUnknownProvider = errors.New("未知或尚未实现的服务商")

// New 按配置创建全部客户端。生产环境不允许使用 mock 短信，避免验证码发不出去却显示成功。
func New(cfg *config.Config) (*Clients, error) {
	var c Clients
	var errs []error
	pick := func(name, provider string, mock func()) {
		switch provider {
		case config.ProviderMock:
			mock()
		default:
			errs = append(errs, fmt.Errorf("%s_PROVIDER=%q：%w", name, provider, ErrUnknownProvider))
		}
	}
	pick("SMS", cfg.SMS.Provider, func() { c.SMS = sms.NewMock() })
	hasBailian := cfg.AI.BailianBaseURL != "" && cfg.AI.BailianAPIKey != ""
	switch cfg.AI.Provider {
	case "bailian":
		// 百炼的 OpenAI 兼容接口（dev-spec 第七节）。地址与密钥只从环境变量读。
		if !hasBailian {
			errs = append(errs, errors.New("AI_PROVIDER=bailian 需要设置 BAILIAN_BASE_URL 与 BAILIAN_API_KEY"))
		}
		c.AI = ai.NewOpenAI(cfg.AI.BailianBaseURL, cfg.AI.BailianAPIKey)
	case "relay":
		// 中转接口为主；它不可用（网络错误、5xx、超时）时自动改用百炼。
		if cfg.AI.RelayBaseURL == "" || cfg.AI.RelayAPIKey == "" {
			errs = append(errs, errors.New("AI_PROVIDER=relay 需要设置 AI_RELAY_BASE_URL 与 AI_RELAY_API_KEY"))
		}
		c.AI = ai.NewOpenAI(cfg.AI.RelayBaseURL, cfg.AI.RelayAPIKey)
		if hasBailian {
			c.AIFallback = ai.NewOpenAI(cfg.AI.BailianBaseURL, cfg.AI.BailianAPIKey)
		}
	default:
		pick("AI", cfg.AI.Provider, func() { c.AI = ai.NewMock() })
	}
	if cfg.AI.AltBaseURL != "" && cfg.AI.AltAPIKey != "" {
		// 对照平台：评测与故障时备用。
		c.AIAlt = ai.NewOpenAI(cfg.AI.AltBaseURL, cfg.AI.AltAPIKey)
	}
	pick("OCR", cfg.OCR.Provider, func() { c.OCR, c.PDF = ocr.NewMock(), ocr.NewMock() })
	pick("ASR", cfg.ASR.Provider, func() { c.ASR = asr.NewMock() })
	pick("MODERATION", cfg.Moderation.Provider, func() { c.Moderation = moderation.NewMock() })
	switch cfg.Pay.Provider {
	case "real":
		r, err := newRealPay(cfg.Pay)
		if err != nil {
			errs = append(errs, err)
		}
		c.Pay = r
	default:
		// mock 回调只用本机密钥签名，生产环境不创建，避免伪造回调白开会员；生产不配 real 时支付不可用。
		pick("PAY", cfg.Pay.Provider, func() {
			if !cfg.IsProduction() {
				c.Pay = pay.NewMock(cfg.JWTSecret)
			}
		})
	}
	pick("OSS", cfg.OSS.Provider, func() {
		m := oss.NewMock()
		if cfg.OSS.MockBaseURL != "" {
			m.BaseURL = cfg.OSS.MockBaseURL
		}
		c.OSS = m
	})

	if cfg.IsProduction() && cfg.SMS.Provider == config.ProviderMock {
		errs = append(errs, errors.New("生产环境不能使用 mock 短信"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &c, nil
}

// newRealPay 按配置创建真实支付渠道：只配了的渠道才可用；配了一半（缺密钥）时报错。
func newRealPay(p config.PayConfig) (*pay.Real, error) {
	var r pay.Real
	var errs []error
	if p.WechatAppID != "" || p.WechatMchID != "" {
		w, err := pay.NewWechat(pay.WechatConfig{AppID: p.WechatAppID, MchID: p.WechatMchID, SerialNo: p.WechatSerialNo, PrivateKey: p.WechatPrivateKey,
			APIv3Key: p.WechatAPIv3Key, PlatformPublicKey: p.WechatPlatformPublicKey, PlatformSerial: p.WechatPlatformSerial})
		if err != nil {
			errs = append(errs, err)
		}
		r.Wechat = w
	}
	if p.AlipayAppID != "" {
		a, err := pay.NewAlipay(pay.AlipayConfig{AppID: p.AlipayAppID, PrivateKey: p.AlipayPrivateKey, PublicKey: p.AlipayPublicKey})
		if err != nil {
			errs = append(errs, err)
		}
		r.Alipay = a
	}
	if p.AppleIssuerID != "" || p.AppleKeyID != "" {
		a, err := pay.NewApple(pay.AppleConfig{IssuerID: p.AppleIssuerID, KeyID: p.AppleKeyID, PrivateKey: p.ApplePrivateKey, BundleID: p.AppleBundleID})
		if err != nil {
			errs = append(errs, err)
		}
		r.Apple = a
	}
	return &r, errors.Join(errs...)
}
