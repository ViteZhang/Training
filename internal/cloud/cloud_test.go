package cloud

import (
	"context"
	"errors"
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
		OCR:    p, ASR: p, Moderation: p, Pay: p,
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

	g := pay.NewMock()
	if _, err := g.VerifyNotification(ctx, pay.ChannelWechat, []byte("o1|t1|5900"), nil); !errors.Is(err, pay.ErrInvalidSignature) {
		t.Error("缺签名应验签失败")
	}
	n, err := g.VerifyNotification(ctx, pay.ChannelWechat, []byte("o1|t1|5900"), map[string]string{"X-Mock-Sign": "ok"})
	if err != nil || n.OrderNo != "o1" || n.AmountCents != 5900 || !n.Paid {
		t.Errorf("pay mock: %+v %v", n, err)
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
