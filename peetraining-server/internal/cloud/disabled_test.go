package cloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"peetraining-server/internal/cloud/asr"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/config"
)

func TestDisabledProductionServicesFailClosed(t *testing.T) {
	cfg := mockConfig()
	cfg.AppEnv = config.EnvProduction
	cfg.SMS.Provider = config.ProviderDisabled
	cfg.OSS.Provider = config.ProviderDisabled
	cfg.OCR.Provider = config.ProviderDisabled
	cfg.ASR.Provider = config.ProviderDisabled
	cfg.Moderation.Provider = config.ProviderDisabled
	// 暂停支付时，即使服务器上仍有无效的旧私钥，也不应初始化支付渠道。
	cfg.Pay = config.PayConfig{Provider: config.ProviderDisabled, WechatAppID: "wx", WechatPrivateKey: "invalid"}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.Pay != nil {
		t.Fatal("暂停支付时不应创建网关")
	}
	ctx := context.Background()
	check := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrDisabled) {
			t.Errorf("%s：需要返回 ErrDisabled，实际 %v", name, err)
		}
	}
	check("短信", c.SMS.SendCode(ctx, "13800000000", "123456"))
	result, err := c.OCR.Recognize(ctx, ocr.Image{Data: []byte("不可当成识别结果")})
	check("OCR", err)
	if result.Text != "" {
		t.Error("关闭识别时不应返回伪造文本")
	}
	_, err = c.PDF.ParsePDF(ctx, []byte("PDF"))
	check("PDF", err)
	_, err = c.ASR.Transcribe(ctx, asr.Audio{})
	check("语音识别", err)
	verdict, err := c.Moderation.CheckText(ctx, "text")
	check("文本审核", err)
	if verdict.Pass {
		t.Error("关闭审核时不应放行文本")
	}
	verdict, err = c.Moderation.CheckImage(ctx, "image")
	check("图片审核", err)
	if verdict.Pass {
		t.Error("关闭审核时不应放行图片")
	}
	url, err := c.OSS.PresignPut(ctx, "key", "text/plain", 1, time.Minute)
	check("上传签名", err)
	if url.URL != "" {
		t.Error("关闭存储时不应返回上传地址")
	}
	_, err = c.OSS.PresignGet(ctx, "key", "name", time.Minute)
	check("下载签名", err)
	_, err = c.OSS.Head(ctx, "key")
	check("对象元信息", err)
	_, err = c.OSS.Get(ctx, "key")
	check("读取对象", err)
	check("保存对象", c.OSS.Put(ctx, "key", []byte("data"), "text/plain"))
	check("删除对象", c.OSS.Delete(ctx, "key"))
	_, err = c.OSS.DeletePrefix(ctx, "prefix/")
	check("删除前缀", err)
}
