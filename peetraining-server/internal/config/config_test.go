package config

import (
	"strings"
	"testing"
	"time"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(envFrom(nil))
	if err != nil {
		t.Fatalf("本地默认配置应能加载：%v", err)
	}
	if cfg.AppEnv != EnvLocal || cfg.HTTPAddr != ":8080" {
		t.Errorf("默认值不对：%+v", cfg)
	}
	for name, p := range map[string]string{
		"sms": cfg.SMS.Provider, "ai": cfg.AI.Provider, "ocr": cfg.OCR.Provider,
		"asr": cfg.ASR.Provider, "moderation": cfg.Moderation.Provider, "pay": cfg.Pay.Provider,
	} {
		if p != ProviderMock {
			t.Errorf("%s 默认应为 mock，实际 %q", name, p)
		}
	}
	if cfg.JWTSecret == "" {
		t.Error("本地应有默认 JWT_SECRET")
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v", cfg.ShutdownTimeout)
	}
	if cfg.AI.ModelStrong != "qwen3.8-flash" || cfg.AI.ModelCheap != "qwen3.8-flash" || cfg.AI.BailianThinking {
		t.Errorf("百炼默认统一用 qwen3.8-flash、不开思考：%+v", cfg.AI)
	}
	on, _ := load(envFrom(map[string]string{"AI_BAILIAN_THINKING": "true"}))
	if on == nil || !on.AI.BailianThinking {
		t.Error("AI_BAILIAN_THINKING=true 应打开思考")
	}
}

func TestLoadProductionRequiresExplicitValues(t *testing.T) {
	_, err := load(envFrom(map[string]string{"APP_ENV": "production"}))
	if err == nil {
		t.Fatal("生产环境缺少必填项时应报错")
	}
	for _, key := range []string{"MYSQL_DSN", "REDIS_ADDR", "JWT_SECRET"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("错误信息应提到 %s：%v", key, err)
		}
	}
}

func TestLoadProductionShortSecret(t *testing.T) {
	_, err := load(envFrom(map[string]string{
		"APP_ENV": "production", "MYSQL_DSN": "x", "REDIS_ADDR": "y", "JWT_SECRET": "short",
	}))
	if err == nil || !strings.Contains(err.Error(), "32") {
		t.Fatalf("短密钥应报错，实际：%v", err)
	}
}

func TestLoadProductionOK(t *testing.T) {
	cfg, err := load(envFrom(map[string]string{
		"APP_ENV": "production", "MYSQL_DSN": "x", "REDIS_ADDR": "y",
		"JWT_SECRET":   strings.Repeat("s", 32),
		"SMS_PROVIDER": "aliyun", "SMS_SIGN_NAME": "签名", "SMS_TEMPLATE_CODE": "SMS_1",
		"ALIYUN_ACCESS_KEY_ID": "id", "ALIYUN_ACCESS_KEY_SECRET": "secret",
	}))
	if err != nil {
		t.Fatalf("完整的生产配置应能加载：%v", err)
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction 应为 true")
	}
	if cfg.SMS.TemplateParam != "code" || cfg.SMS.Endpoint != "dysmsapi.aliyuncs.com" {
		t.Errorf("短信默认值不对：%+v", cfg.SMS)
	}
}

func TestLoadSMS(t *testing.T) {
	prod := map[string]string{"APP_ENV": "production", "MYSQL_DSN": "x", "REDIS_ADDR": "y", "JWT_SECRET": strings.Repeat("s", 32)}

	// 生产环境不能用 mock 短信（默认值就是 mock）。
	if _, err := load(envFrom(prod)); err == nil || !strings.Contains(err.Error(), "SMS_PROVIDER") {
		t.Errorf("生产环境 mock 短信应报错：%v", err)
	}

	// aliyun 缺配置时逐项报出来。
	prod["SMS_PROVIDER"] = "aliyun"
	_, err := load(envFrom(prod))
	for _, key := range []string{"ALIYUN_ACCESS_KEY_ID", "ALIYUN_ACCESS_KEY_SECRET", "SMS_SIGN_NAME", "SMS_TEMPLATE_CODE"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("错误信息应提到 %s：%v", key, err)
		}
	}

	// 未知服务商。
	if _, err := load(envFrom(map[string]string{"SMS_PROVIDER": "tencent"})); err == nil || !strings.Contains(err.Error(), "SMS_PROVIDER") {
		t.Errorf("未知短信服务商应报错：%v", err)
	}

	// 显式关闭短信是允许的。
	if _, err := load(envFrom(map[string]string{"SMS_PROVIDER": "disabled"})); err != nil {
		t.Errorf("SMS_PROVIDER=disabled 应能加载：%v", err)
	}

	// 本地也可以接真实短信（自定义变量名与地址）。
	cfg, err := load(envFrom(map[string]string{
		"SMS_PROVIDER": "aliyun", "SMS_SIGN_NAME": "签名", "SMS_TEMPLATE_CODE": "SMS_1",
		"SMS_TEMPLATE_PARAM": "verify_code", "SMS_ENDPOINT": "dysmsapi.example.test",
		"ALIYUN_ACCESS_KEY_ID": "id", "ALIYUN_ACCESS_KEY_SECRET": "secret",
	}))
	if err != nil || cfg.SMS.TemplateParam != "verify_code" || cfg.SMS.Endpoint != "dysmsapi.example.test" {
		t.Errorf("本地 aliyun 短信配置：%+v %v", cfg, err)
	}
}

func TestLoadInvalidValues(t *testing.T) {
	_, err := load(envFrom(map[string]string{
		"APP_ENV": "staging", "REDIS_DB": "x", "SHUTDOWN_TIMEOUT": "soon",
	}))
	if err == nil {
		t.Fatal("非法值应报错")
	}
	for _, key := range []string{"APP_ENV", "REDIS_DB", "SHUTDOWN_TIMEOUT"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("错误信息应提到 %s：%v", key, err)
		}
	}
}

func TestParsePrices(t *testing.T) {
	p, err := parsePrices(" qwen-max=2.4:9.6, qwen-plus=0.8:2 ,")
	if err != nil || p["qwen-max"] != [2]float64{2.4, 9.6} || p["qwen-plus"] != [2]float64{0.8, 2} || len(p) != 2 {
		t.Fatalf("%v %v", p, err)
	}
	for _, bad := range []string{"qwen-max", "qwen-max=2.4", "=1:2", "m=a:1", "m=-1:2"} {
		if _, err := parsePrices(bad); err == nil {
			t.Errorf("%q 应报错", bad)
		}
	}
}
