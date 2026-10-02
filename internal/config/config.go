// Package config 从环境变量读取全部配置。
//
// 密钥只从环境变量读取，不写入仓库与日志（CLAUDE.md 必须遵守第 10 条）。
// 本地默认全部云服务走 mock，不需要任何真实密钥就能启动。
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// 运行环境。只有本地与生产两套（docs/tech-plan.md「环境与发布」）。
const (
	EnvLocal      = "local"
	EnvTest       = "test"
	EnvProduction = "production"
)

// ProviderMock 是所有云服务的本地默认实现。
const ProviderMock = "mock"

type Config struct {
	AppEnv   string
	HTTPAddr string
	// ShutdownTimeout 是优雅停机时等待进行中的请求与任务的最长时间。
	ShutdownTimeout time.Duration

	MySQLDSN      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTSecret string

	OSS OSSConfig

	SMS        SMSConfig
	AI         AIConfig
	OCR        ProviderConfig
	ASR        ProviderConfig
	Moderation ProviderConfig
	Pay        ProviderConfig

	// AliyunAccessKeyID / Secret 供 OCR、语音、内容安全等阿里云服务共用。
	AliyunAccessKeyID     string
	AliyunAccessKeySecret string
}

type OSSConfig struct {
	Provider        string
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	// MockBaseURL 是本地 mock 预签名地址的前缀（真机调试填本机局域网地址），只在 OSS_PROVIDER=mock 时用。
	MockBaseURL string
}

type ProviderConfig struct {
	Provider string
}

type SMSConfig struct {
	Provider     string
	SignName     string
	TemplateCode string
}

type AIConfig struct {
	Provider       string
	BailianBaseURL string
	BailianAPIKey  string
	AltBaseURL     string
	AltAPIKey      string
	// ModelStrong / ModelCheap 是两档模型的默认名（Q02 定之前的开发值；后台 7.8 可按能力覆盖）。
	ModelStrong string
	ModelCheap  string
}

// Load 从环境变量读取配置并校验。
func Load() (*Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (*Config, error) {
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}

	cfg := &Config{
		AppEnv:   get("APP_ENV", EnvLocal),
		HTTPAddr: get("HTTP_ADDR", ":8080"),

		MySQLDSN:      get("MYSQL_DSN", "training:training@tcp(127.0.0.1:3306)/training"),
		RedisAddr:     get("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: get("REDIS_PASSWORD", ""),

		JWTSecret: get("JWT_SECRET", ""),

		OSS: OSSConfig{
			Provider:        get("OSS_PROVIDER", ProviderMock),
			Endpoint:        get("OSS_ENDPOINT", ""),
			Bucket:          get("OSS_BUCKET", ""),
			AccessKeyID:     get("OSS_ACCESS_KEY_ID", ""),
			AccessKeySecret: get("OSS_ACCESS_KEY_SECRET", ""),
			MockBaseURL:     get("OSS_MOCK_BASE_URL", ""),
		},
		SMS: SMSConfig{
			Provider:     get("SMS_PROVIDER", ProviderMock),
			SignName:     get("SMS_SIGN_NAME", ""),
			TemplateCode: get("SMS_TEMPLATE_CODE", ""),
		},
		AI: AIConfig{
			Provider:       get("AI_PROVIDER", ProviderMock),
			BailianBaseURL: get("BAILIAN_BASE_URL", ""),
			BailianAPIKey:  get("BAILIAN_API_KEY", ""),
			AltBaseURL:     get("AI_ALT_BASE_URL", ""),
			AltAPIKey:      get("AI_ALT_API_KEY", ""),
			ModelStrong:    get("AI_MODEL_STRONG", "qwen-plus"),
			ModelCheap:     get("AI_MODEL_CHEAP", "qwen-turbo"),
		},
		OCR:        ProviderConfig{Provider: get("OCR_PROVIDER", ProviderMock)},
		ASR:        ProviderConfig{Provider: get("ASR_PROVIDER", ProviderMock)},
		Moderation: ProviderConfig{Provider: get("MODERATION_PROVIDER", ProviderMock)},
		Pay:        ProviderConfig{Provider: get("PAY_PROVIDER", ProviderMock)},

		AliyunAccessKeyID:     get("ALIYUN_ACCESS_KEY_ID", ""),
		AliyunAccessKeySecret: get("ALIYUN_ACCESS_KEY_SECRET", ""),
	}

	var errs []error

	redisDB, err := strconv.Atoi(get("REDIS_DB", "0"))
	if err != nil {
		errs = append(errs, fmt.Errorf("REDIS_DB 必须是整数：%w", err))
	}
	cfg.RedisDB = redisDB

	cfg.ShutdownTimeout, err = time.ParseDuration(get("SHUTDOWN_TIMEOUT", "30s"))
	if err != nil {
		errs = append(errs, fmt.Errorf("SHUTDOWN_TIMEOUT 格式错误：%w", err))
	}

	switch cfg.AppEnv {
	case EnvLocal, EnvTest, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("APP_ENV 只能是 %s、%s 或 %s，当前为 %q", EnvLocal, EnvTest, EnvProduction, cfg.AppEnv))
	}

	if cfg.AppEnv == EnvProduction {
		// 生产环境必须显式配置，避免误用本地默认值。
		for key, val := range map[string]string{
			"MYSQL_DSN":  getenv("MYSQL_DSN"),
			"REDIS_ADDR": getenv("REDIS_ADDR"),
			"JWT_SECRET": cfg.JWTSecret,
		} {
			if strings.TrimSpace(val) == "" {
				errs = append(errs, fmt.Errorf("生产环境必须设置 %s", key))
			}
		}
		if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
			errs = append(errs, errors.New("JWT_SECRET 至少 32 个字符"))
		}
	} else if cfg.JWTSecret == "" {
		// 本地与测试用固定值，方便启动；生产不会走到这里。
		cfg.JWTSecret = "local-dev-secret-do-not-use-in-production"
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

// IsProduction 报告是否运行在生产环境。
func (c *Config) IsProduction() bool { return c.AppEnv == EnvProduction }
