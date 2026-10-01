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
		"JWT_SECRET": strings.Repeat("s", 32),
	}))
	if err != nil {
		t.Fatalf("完整的生产配置应能加载：%v", err)
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction 应为 true")
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
