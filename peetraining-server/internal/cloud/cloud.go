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
	SMS        sms.Sender
	AI         ai.Client
	OCR        ocr.Recognizer
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
	pick("AI", cfg.AI.Provider, func() { c.AI = ai.NewMock() })
	pick("OCR", cfg.OCR.Provider, func() { c.OCR = ocr.NewMock() })
	pick("ASR", cfg.ASR.Provider, func() { c.ASR = asr.NewMock() })
	pick("MODERATION", cfg.Moderation.Provider, func() { c.Moderation = moderation.NewMock() })
	pick("PAY", cfg.Pay.Provider, func() { c.Pay = pay.NewMock() })
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
