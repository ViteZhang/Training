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

// ProviderDisabled 显式关闭尚未接入或暂不开放的云服务，调用时返回不可用。
const ProviderDisabled = "disabled"

// ProviderAliyun 表示使用阿里云的真实服务。
const ProviderAliyun = "aliyun"

// ProviderPDFium 是 PDF 解析用内嵌 PDFium（PDF_PROVIDER）。
const ProviderPDFium = "pdfium"

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
	OCR        OCRConfig
	ASR        ProviderConfig
	ASRBaseURL string
	ASRAPIKey  string
	ASRModel   string
	Moderation ModerationConfig
	Pay        PayConfig

	// ExportFontPath / ExportLatinFontPath 是导出 PDF 用的中文与西文字体（TrueType）；镜像里放在 /app/fonts。
	ExportFontPath      string
	ExportLatinFontPath string

	// AliyunAccessKeyID / Secret 供 OCR、语音、内容安全等阿里云服务共用。
	// AlertWebhookURL 是告警推送的钉钉或企业微信群机器人地址（含令牌，只放环境变量）；不配时告警只写日志。
	AlertWebhookURL       string
	AliyunAccessKeyID     string
	AliyunAccessKeySecret string
}

type OSSConfig struct {
	Provider        string
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	AccessKeySecret string
	// Region 如 cn-beijing；为空时从 Endpoint 推断。
	Region string
	// InternalEndpoint 是服务端读写用的内网地址（同地域 ECS 上填，如 oss-cn-beijing-internal.aliyuncs.com）；预签名地址仍用 Endpoint。
	InternalEndpoint string
	// MockBaseURL 是本地 mock 预签名地址的前缀（真机调试填本机局域网地址），只在 OSS_PROVIDER=mock 时用。
	MockBaseURL string
}

type ProviderConfig struct {
	Provider string
}

// OCRConfig 是文字识别与 PDF 解析配置。
type OCRConfig struct {
	Provider string
	Endpoint string
	// PDFProvider 是 PDF 解析：mock、pdfium（内嵌 PDFium 取文字层，扫描页交给 OCR）或 disabled；
	// 不设置时跟随 OCR_PROVIDER（mock→mock、disabled→disabled、aliyun→pdfium）。
	PDFProvider string
}

// PDF 返回实际使用的 PDF 解析方式：没设置 PDF_PROVIDER 时跟随 OCR_PROVIDER。
func (o OCRConfig) PDF() string {
	switch {
	case o.PDFProvider != "":
		return o.PDFProvider
	case o.Provider == ProviderAliyun:
		return ProviderPDFium
	}
	return o.Provider
}

// ModerationConfig 是内容安全配置。
type ModerationConfig struct {
	Provider     string
	Endpoint     string
	TextService  string
	ImageService string
	// RejectLevel 是判为不通过的最低风险等级：high（默认）、medium、low。
	RejectLevel string
}

// PayConfig 是支付配置（T25，ADR 0011）。PAY_PROVIDER=real 时按各渠道是否配置决定可用渠道；mock 只用于本地与测试。
type PayConfig struct {
	Provider string
	// NotifyBaseURL 是支付平台回调本服务的地址前缀，如 https://training.dreamerlab.cn/api/v1。
	NotifyBaseURL string

	WechatAppID             string
	WechatMchID             string
	WechatSerialNo          string
	WechatPrivateKey        string
	WechatAPIv3Key          string
	WechatPlatformPublicKey string
	WechatPlatformSerial    string

	AlipayAppID      string
	AlipayPrivateKey string
	AlipayPublicKey  string

	AppleIssuerID   string
	AppleKeyID      string
	ApplePrivateKey string
	AppleBundleID   string
}

// SMSConfig 是短信配置。SMS_PROVIDER=aliyun 时凭证复用 ALIYUN_ACCESS_KEY_ID / SECRET。
type SMSConfig struct {
	Provider     string
	SignName     string
	TemplateCode string
	// TemplateParam 是验证码模板里的变量名（模板写 ${code} 时为 code）。
	TemplateParam string
	// Endpoint 是短信接口地址，默认国内短信 dysmsapi.aliyuncs.com。
	Endpoint string
	// TestPhones 是测试账号：获取验证码时不发短信，验证码固定为 000000（SMS_TEST_PHONES）。
	TestPhones map[string]bool
}

// defaultTestPhones 是默认的测试账号 18600010001～18600010010；设 SMS_TEST_PHONES=off 关闭。
const defaultTestPhones = "18600010001,18600010002,18600010003,18600010004,18600010005," +
	"18600010006,18600010007,18600010008,18600010009,18600010010"

// parseTestPhones 解析逗号分隔的手机号；off 表示不设测试账号。
func parseTestPhones(v string) (map[string]bool, error) {
	out := map[string]bool{}
	if v == "off" {
		return out, nil
	}
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) != 11 || strings.Trim(p, "0123456789") != "" {
			return nil, fmt.Errorf("SMS_TEST_PHONES 里的 %q 不是 11 位手机号", p)
		}
		out[p] = true
	}
	return out, nil
}

type AIConfig struct {
	Provider       string
	BailianBaseURL string
	BailianAPIKey  string
	AltBaseURL     string
	AltAPIKey      string
	// ModelStrong / ModelCheap 是百炼两档模型的默认名（Q02 定之前的开发值；后台 7.8 可按能力覆盖）。
	ModelStrong string
	ModelCheap  string
	// BailianThinking 对应 AI_BAILIAN_THINKING：百炼 qwen3 系列是否先思考再回答。默认关（导入、批改按规则出 JSON，
	// 思考只会慢几倍、多花 token）；评测证明某个模型开思考更准时再打开。
	BailianThinking bool
	// PreferStrong 为 true 时所有能力都用高阶模型（便宜档也用 ModelStrong / RelayModelStrong）。
	PreferStrong bool
	// AI_PROVIDER=relay 时的主用平台（OpenAI 兼容的中转接口）；百炼配置齐全时作为它失败时的备用。
	RelayBaseURL     string
	RelayAPIKey      string
	RelayModelStrong string
	RelayModelCheap  string
	// Prices 是各模型的价格：元 / 百万 token（输入、输出），来自 AI_PRICES，例如 "qwen3.8-flash=0.3:3"。
	// 用于 ai_calls 记成本（7.8 成本看板）和评测的每千次成本；价格以平台当时的公开价为准，不写死在代码里。
	Prices map[string][2]float64
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

		ExportFontPath:      get("EXPORT_FONT_PATH", "/app/fonts/DroidSansFallbackFull.ttf"),
		ExportLatinFontPath: get("EXPORT_LATIN_FONT_PATH", "/app/fonts/DejaVuSans.ttf"),

		OSS: OSSConfig{
			Provider:         get("OSS_PROVIDER", ProviderMock),
			Endpoint:         get("OSS_ENDPOINT", ""),
			Bucket:           get("OSS_BUCKET", ""),
			AccessKeyID:      get("OSS_ACCESS_KEY_ID", ""),
			AccessKeySecret:  get("OSS_ACCESS_KEY_SECRET", ""),
			MockBaseURL:      get("OSS_MOCK_BASE_URL", ""),
			Region:           get("OSS_REGION", ""),
			InternalEndpoint: get("OSS_INTERNAL_ENDPOINT", ""),
		},
		SMS: SMSConfig{
			Provider:      get("SMS_PROVIDER", ProviderMock),
			SignName:      get("SMS_SIGN_NAME", ""),
			TemplateCode:  get("SMS_TEMPLATE_CODE", ""),
			TemplateParam: get("SMS_TEMPLATE_PARAM", "code"),
			Endpoint:      get("SMS_ENDPOINT", "dysmsapi.aliyuncs.com"),
		},
		AI: AIConfig{
			Provider:        get("AI_PROVIDER", ProviderMock),
			BailianBaseURL:  get("BAILIAN_BASE_URL", ""),
			BailianAPIKey:   get("BAILIAN_API_KEY", ""),
			AltBaseURL:      get("AI_ALT_BASE_URL", ""),
			AltAPIKey:       get("AI_ALT_API_KEY", ""),
			ModelStrong:     get("AI_MODEL_STRONG", "qwen3.8-flash"),
			ModelCheap:      get("AI_MODEL_CHEAP", "qwen3.8-flash"),
			BailianThinking: get("AI_BAILIAN_THINKING", "false") == "true",
			// 优先用高阶模型（默认开启），省钱时设 AI_PREFER_STRONG=false 让便宜档能力用便宜模型。
			PreferStrong:     get("AI_PREFER_STRONG", "true") != "false",
			RelayBaseURL:     get("AI_RELAY_BASE_URL", ""),
			RelayAPIKey:      get("AI_RELAY_API_KEY", ""),
			RelayModelStrong: get("AI_RELAY_MODEL_STRONG", "gpt-5.5"),
			RelayModelCheap:  get("AI_RELAY_MODEL_CHEAP", "gpt-5.5"),
		},
		OCR: OCRConfig{
			Provider:    get("OCR_PROVIDER", ProviderMock),
			Endpoint:    get("OCR_ENDPOINT", ""),
			PDFProvider: get("PDF_PROVIDER", ""),
		},
		ASR:        ProviderConfig{Provider: get("ASR_PROVIDER", ProviderMock)},
		ASRBaseURL: get("ASR_BASE_URL", get("BAILIAN_BASE_URL", "")),
		ASRAPIKey:  get("ASR_API_KEY", get("BAILIAN_API_KEY", "")),
		ASRModel:   get("ASR_MODEL", "qwen3-asr-flash"),
		Moderation: ModerationConfig{
			Provider:     get("MODERATION_PROVIDER", ProviderMock),
			Endpoint:     get("MODERATION_ENDPOINT", ""),
			TextService:  get("MODERATION_TEXT_SERVICE", ""),
			ImageService: get("MODERATION_IMAGE_SERVICE", ""),
			RejectLevel:  get("MODERATION_REJECT_LEVEL", ""),
		},
		Pay: PayConfig{
			Provider:                get("PAY_PROVIDER", ProviderMock),
			NotifyBaseURL:           get("PAY_NOTIFY_BASE_URL", "https://training.dreamerlab.cn/api/v1"),
			WechatAppID:             get("WECHAT_PAY_APP_ID", ""),
			WechatMchID:             get("WECHAT_PAY_MCH_ID", ""),
			WechatSerialNo:          get("WECHAT_PAY_SERIAL_NO", ""),
			WechatPrivateKey:        get("WECHAT_PAY_PRIVATE_KEY", ""),
			WechatAPIv3Key:          get("WECHAT_PAY_APIV3_KEY", ""),
			WechatPlatformPublicKey: get("WECHAT_PAY_PLATFORM_PUBLIC_KEY", ""),
			WechatPlatformSerial:    get("WECHAT_PAY_PLATFORM_SERIAL", ""),
			AlipayAppID:             get("ALIPAY_APP_ID", ""),
			AlipayPrivateKey:        get("ALIPAY_PRIVATE_KEY", ""),
			AlipayPublicKey:         get("ALIPAY_PUBLIC_KEY", ""),
			AppleIssuerID:           get("APPLE_IAP_ISSUER_ID", ""),
			AppleKeyID:              get("APPLE_IAP_KEY_ID", ""),
			ApplePrivateKey:         get("APPLE_IAP_PRIVATE_KEY", ""),
			AppleBundleID:           get("APPLE_IAP_BUNDLE_ID", "cn.dreamerlab.training"),
		},

		AlertWebhookURL:       get("ALERT_WEBHOOK_URL", ""),
		AliyunAccessKeyID:     get("ALIYUN_ACCESS_KEY_ID", ""),
		AliyunAccessKeySecret: get("ALIYUN_ACCESS_KEY_SECRET", ""),
	}

	// OSS 可以用单独的 AccessKey；不设置时复用 ALIYUN_ACCESS_KEY_ID / SECRET。
	if cfg.OSS.AccessKeyID == "" && cfg.OSS.AccessKeySecret == "" {
		cfg.OSS.AccessKeyID, cfg.OSS.AccessKeySecret = cfg.AliyunAccessKeyID, cfg.AliyunAccessKeySecret
	}

	var errs []error
	prices, err := parsePrices(get("AI_PRICES", ""))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.AI.Prices = prices

	cfg.SMS.TestPhones, err = parseTestPhones(get("SMS_TEST_PHONES", defaultTestPhones))
	if err != nil {
		errs = append(errs, err)
	}

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

	switch cfg.SMS.Provider {
	case ProviderMock:
		if cfg.AppEnv == EnvProduction {
			// mock 不真正发短信，生产用它会让用户收不到验证码。
			errs = append(errs, errors.New("生产环境不能使用 SMS_PROVIDER=mock，请设置为 aliyun"))
		}
	case ProviderAliyun:
		// 缺一项都发不出短信：启动时就报出来，而不是等用户登录时才失败。
		for _, kv := range [][2]string{
			{"ALIYUN_ACCESS_KEY_ID", cfg.AliyunAccessKeyID},
			{"ALIYUN_ACCESS_KEY_SECRET", cfg.AliyunAccessKeySecret},
			{"SMS_SIGN_NAME", cfg.SMS.SignName},
			{"SMS_TEMPLATE_CODE", cfg.SMS.TemplateCode},
		} {
			if kv[1] == "" {
				errs = append(errs, fmt.Errorf("SMS_PROVIDER=aliyun 需要设置 %s", kv[0]))
			}
		}
	case ProviderDisabled:
		// 显式关闭短信：发验证码时返回不可用（cloud.disabledService）。
	default:
		errs = append(errs, fmt.Errorf("SMS_PROVIDER 只能是 %s、%s 或 %s，当前为 %q", ProviderMock, ProviderAliyun, ProviderDisabled, cfg.SMS.Provider))
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

// parsePrices 解析 AI_PRICES："模型=输入:输出,…"，单位元 / 百万 token。
func parsePrices(v string) (map[string][2]float64, error) {
	out := map[string][2]float64{}
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		model, rest, ok := strings.Cut(item, "=")
		in, outp, ok2 := strings.Cut(rest, ":")
		a, err1 := strconv.ParseFloat(strings.TrimSpace(in), 64)
		b, err2 := strconv.ParseFloat(strings.TrimSpace(outp), 64)
		if !ok || !ok2 || err1 != nil || err2 != nil || a < 0 || b < 0 || strings.TrimSpace(model) == "" {
			return nil, fmt.Errorf("AI_PRICES 格式应为「模型=输入价:输出价」（元 / 百万 token），用逗号分隔：%q", item)
		}
		out[strings.TrimSpace(model)] = [2]float64{a, b}
	}
	return out, nil
}
