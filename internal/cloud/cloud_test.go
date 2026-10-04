package cloud

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/moderation"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/pay"
	"peetraining-server/internal/cloud/sms"
	"peetraining-server/internal/config"
)

func mockConfig() *config.Config {
	p := config.ProviderConfig{Provider: config.ProviderMock}
	return &config.Config{
		AppEnv: config.EnvLocal,
		SMS:    config.SMSConfig{Provider: config.ProviderMock},
		AI:     config.AIConfig{Provider: config.ProviderMock},
		OCR:    p, ASR: p, Moderation: p, Pay: config.PayConfig{Provider: config.ProviderMock},
		OSS: config.OSSConfig{Provider: config.ProviderMock},
	}
}

func TestNewAllMock(t *testing.T) {
	c, err := New(mockConfig())
	if err != nil {
		t.Fatal(err)
	}
	if c.SMS == nil || c.AI == nil || c.OCR == nil || c.ASR == nil || c.Moderation == nil || c.Pay == nil || c.OSS == nil {
		t.Fatalf("有客户端为空：%+v", c)
	}
}

func TestNewUnknownProvider(t *testing.T) {
	cfg := mockConfig()
	cfg.AI.Provider = "bailian"
	cfg.Pay.Provider = "nope"
	_, err := New(cfg)
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("应返回 ErrUnknownProvider，实际 %v", err)
	}
}

func TestNewBailian(t *testing.T) {
	cfg := mockConfig()
	cfg.AI.Provider = "bailian"
	if _, err := New(cfg); err == nil {
		t.Fatal("缺少百炼地址与密钥应报错")
	}
	cfg.AI = config.AIConfig{Provider: "bailian", BailianBaseURL: "https://example.test/v1", BailianAPIKey: "k", AltBaseURL: "https://alt.test/v1", AltAPIKey: "k2"}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.AI.(*ai.OpenAI); !ok || c.AIAlt == nil {
		t.Fatalf("应使用 OpenAI 兼容客户端：%T %v", c.AI, c.AIAlt)
	}
}

func TestNewProductionRejectsMockSMS(t *testing.T) {
	cfg := mockConfig()
	cfg.AppEnv = config.EnvProduction
	if _, err := New(cfg); err == nil {
		t.Fatal("生产环境使用 mock 短信应报错")
	}
}

func TestMocks(t *testing.T) {
	ctx := context.Background()

	s := sms.NewMock()
	_ = s.SendCode(ctx, "13812345678", "123456")
	if code, ok := s.LastCode("13812345678"); !ok || code != "123456" {
		t.Errorf("sms mock: %q %v", code, ok)
	}

	a := ai.NewMock()
	a.Set("grading", `{"score":3}`)
	resp, _ := a.Complete(ctx, ai.Request{Capability: "grading"})
	if resp.Content != `{"score":3}` || len(a.Calls()) != 1 {
		t.Errorf("ai mock: %+v", resp)
	}
	resp, _ = a.Complete(ctx, ai.Request{Capability: "other"})
	if resp.Content != "{}" {
		t.Errorf("ai mock 默认应返回 {}：%q", resp.Content)
	}

	r, _ := ocr.NewMock().Recognize(ctx, ocr.Image{Data: []byte("名词解释：【意境】是")})
	if r.Text != "名词解释：意境是" || len(r.LowConfidence) != 1 || r.LowConfidence[0] != (ocr.Span{Start: 5, End: 7}) {
		t.Errorf("ocr mock: %+v", r)
	}

	v, _ := moderation.NewMock().CheckText(ctx, "正常内容")
	if !v.Pass {
		t.Error("moderation mock 默认应通过")
	}
	v, _ = moderation.NewMock().CheckText(ctx, "x "+moderation.MockBlockWord)
	if v.Pass || v.Reason == "" {
		t.Error("含拦截词应不通过并给原因")
	}

	if _, err := pay.NewMock("s").VerifyNotification(ctx, pay.ChannelWechat, []byte(`{"order_no":"o1"}`), http.Header{}); !errors.Is(err, pay.ErrInvalidSignature) {
		t.Error("缺签名应验签失败")
	}
}

func TestPayProduction(t *testing.T) {
	cfg := mockConfig()
	cfg.AppEnv = config.EnvProduction
	cfg.SMS.Provider = "aliyun"
	cfg.Pay.Provider = config.ProviderMock
	c, err := New(cfg)
	if err == nil && c.Pay != nil {
		t.Error("生产环境不使用 mock 支付")
	}
	cfg = mockConfig()
	cfg.Pay = config.PayConfig{Provider: "real", WechatAppID: "wx"}
	if _, err := New(cfg); err == nil {
		t.Error("微信支付只配了一半应报错")
	}
	cfg.Pay = config.PayConfig{Provider: "real"}
	if c, err := New(cfg); err != nil || c.Pay == nil || len(c.Pay.Channels()) != 0 {
		t.Errorf("没配渠道时没有可用渠道：%v", err)
	}
}

func TestMocksRespectCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := New(mockConfig())
	if _, err := c.AI.Complete(ctx, ai.Request{}); err == nil {
		t.Error("ai")
	}
	if _, err := c.OCR.Recognize(ctx, ocr.Image{}); err == nil {
		t.Error("ocr")
	}
	if _, err := c.Moderation.CheckText(ctx, ""); err == nil {
		t.Error("moderation")
	}
	if _, err := c.Pay.CreatePrepay(ctx, pay.Order{}); err == nil {
		t.Error("pay")
	}
}
