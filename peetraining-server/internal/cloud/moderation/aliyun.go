package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	green "github.com/alibabacloud-go/green-20220302/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
)

// 默认值：内容安全增强版（green 2022-03-02），华北 2。
const (
	DefaultAliyunEndpoint = "green-cip.cn-beijing.aliyuncs.com"
	DefaultTextService    = "comment_detection_pro" // 公聊评论内容检测（专业版），适合用户上传的长文本
	DefaultImageService   = "baselineCheck"         // 通用基线检测
	DefaultRejectLevel    = "high"
)

// textChunk 是每次送审的字数，低于各文本服务的单次上限（专业版 2000 字），留出余量。
const textChunk = 1500

// URLSigner 给图片对象生成短时效的下载地址，供内容安全服务拉取（oss.Store 的 PresignGet）。
type URLSigner func(ctx context.Context, objectKey string, ttl time.Duration) (string, error)

// AliyunConfig 是阿里云内容安全配置。凭证复用 ALIYUN_ACCESS_KEY_ID / SECRET。
type AliyunConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	Endpoint        string
	TextService     string
	ImageService    string
	// RejectLevel 是判为不通过的最低风险等级：high（默认，只拦高风险）、medium、low。
	// 文科专业课资料常涉及政治、历史、法律，拦得太严会误伤正常资料，所以默认只拦 high。
	RejectLevel string
	SignURL     URLSigner
}

type greenAPI interface {
	TextModerationPlusWithContext(ctx context.Context, req *green.TextModerationPlusRequest, rt *dara.RuntimeOptions) (*green.TextModerationPlusResponse, error)
	ImageModerationWithContext(ctx context.Context, req *green.ImageModerationRequest, rt *dara.RuntimeOptions) (*green.ImageModerationResponse, error)
}

// Aliyun 调用阿里云内容安全增强版审核文本与图片。
type Aliyun struct {
	api          greenAPI
	textService  string
	imageService string
	rejectRank   int
	signURL      URLSigner
}

var _ Checker = (*Aliyun)(nil)

var riskRank = map[string]int{"none": 0, "low": 1, "medium": 2, "high": 3}

// NewAliyun 校验配置并创建客户端。
func NewAliyun(cfg AliyunConfig) (*Aliyun, error) {
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, errors.New("MODERATION_PROVIDER=aliyun 需要设置 ALIYUN_ACCESS_KEY_ID 与 ALIYUN_ACCESS_KEY_SECRET")
	}
	if cfg.SignURL == nil {
		return nil, errors.New("MODERATION_PROVIDER=aliyun 需要 OSS 可用（图片审核要用对象的下载地址），请先设置 OSS_PROVIDER=aliyun")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultAliyunEndpoint
	}
	c, err := green.NewClient(&openapiutil.Config{
		AccessKeyId:     dara.String(cfg.AccessKeyID),
		AccessKeySecret: dara.String(cfg.AccessKeySecret),
		Endpoint:        dara.String(cfg.Endpoint),
		ConnectTimeout:  dara.Int(5000),
		ReadTimeout:     dara.Int(15000),
	})
	if err != nil {
		return nil, fmt.Errorf("创建阿里云内容安全客户端：%w", err)
	}
	return newAliyunWithAPI(c, cfg)
}

func newAliyunWithAPI(api greenAPI, cfg AliyunConfig) (*Aliyun, error) {
	if cfg.TextService == "" {
		cfg.TextService = DefaultTextService
	}
	if cfg.ImageService == "" {
		cfg.ImageService = DefaultImageService
	}
	if cfg.RejectLevel == "" {
		cfg.RejectLevel = DefaultRejectLevel
	}
	rank, ok := riskRank[cfg.RejectLevel]
	if !ok || rank == 0 {
		return nil, fmt.Errorf("MODERATION_REJECT_LEVEL 只能是 high、medium 或 low，当前为 %q", cfg.RejectLevel)
	}
	return &Aliyun{api: api, textService: cfg.TextService, imageService: cfg.ImageService, rejectRank: rank, signURL: cfg.SignURL}, nil
}

func runtime() *dara.RuntimeOptions {
	return &dara.RuntimeOptions{Autoretry: dara.Bool(true), MaxAttempts: dara.Int(2), ConnectTimeout: dara.Int(5000), ReadTimeout: dara.Int(15000)}
}

// CheckText 分段送审；任意一段不通过整段文本不通过。
func (a *Aliyun) CheckText(ctx context.Context, text string) (Verdict, error) {
	for _, chunk := range chunks(text, textChunk) {
		params, _ := json.Marshal(map[string]string{"content": chunk})
		resp, err := a.api.TextModerationPlusWithContext(ctx, &green.TextModerationPlusRequest{
			Service: dara.String(a.textService), ServiceParameters: dara.String(string(params)),
		}, runtime())
		if err != nil {
			return Verdict{}, sdkError("文本审核", err)
		}
		b := resp.Body
		if b == nil || dara.Int32Value(b.Code) != 200 || b.Data == nil {
			return Verdict{}, apiError("文本审核", b)
		}
		if v := a.verdict(dara.StringValue(b.Data.RiskLevel), textReasons(b.Data.Result)); !v.Pass {
			return v, nil
		}
	}
	return Verdict{Pass: true}, nil
}

// CheckImage 把对象的短时效下载地址交给内容安全服务拉取审核，图片不经过本服务中转。
func (a *Aliyun) CheckImage(ctx context.Context, objectKey string) (Verdict, error) {
	u, err := a.signURL(ctx, objectKey, 10*time.Minute)
	if err != nil {
		return Verdict{}, err
	}
	params, _ := json.Marshal(map[string]string{"imageUrl": u})
	resp, err := a.api.ImageModerationWithContext(ctx, &green.ImageModerationRequest{
		Service: dara.String(a.imageService), ServiceParameters: dara.String(string(params)),
	}, runtime())
	if err != nil {
		return Verdict{}, sdkError("图片审核", err)
	}
	b := resp.Body
	if b == nil || dara.Int32Value(b.Code) != 200 || b.Data == nil {
		code, msg := int32(0), ""
		if b != nil {
			code, msg = dara.Int32Value(b.Code), dara.StringValue(b.Msg)
		}
		return Verdict{}, fmt.Errorf("阿里云图片审核失败：code=%d message=%s", code, msg)
	}
	var reasons []string
	for _, r := range b.Data.Result {
		if d := dara.StringValue(r.Description); d != "" && dara.StringValue(r.Label) != "nonLabel" {
			reasons = append(reasons, d)
		}
	}
	return a.verdict(dara.StringValue(b.Data.RiskLevel), reasons), nil
}

func (a *Aliyun) verdict(level string, reasons []string) Verdict {
	if riskRank[strings.ToLower(level)] < a.rejectRank {
		return Verdict{Pass: true}
	}
	reason := "含有不适合传播的内容"
	if len(reasons) > 0 {
		reason = "疑似" + strings.Join(reasons, "、")
	}
	return Verdict{Pass: false, Reason: reason}
}

func textReasons(rs []*green.TextModerationPlusResponseBodyDataResult) []string {
	var out []string
	for _, r := range rs {
		if d := dara.StringValue(r.Description); d != "" && dara.StringValue(r.Label) != "nonLabel" {
			out = append(out, d)
		}
	}
	return out
}

func apiError(what string, b *green.TextModerationPlusResponseBody) error {
	if b == nil {
		return fmt.Errorf("阿里云%s返回为空", what)
	}
	return fmt.Errorf("阿里云%s失败：code=%d message=%s request_id=%s", what, dara.Int32Value(b.Code), dara.StringValue(b.Message), dara.StringValue(b.RequestId))
}

// sdkError 只保留错误码与说明：SDK 错误里可能带请求参数（送审原文），不能进日志（CLAUDE.md 第 11 条）。
func sdkError(what string, err error) error {
	// SDK 默认把错误转换成 tea.SDKError（DisableSDKError 未设置时），两种都要认。
	var te *tea.SDKError
	if errors.As(err, &te) {
		return fmt.Errorf("阿里云%s失败：code=%s message=%s", what, tea.StringValue(te.Code), tea.StringValue(te.Message))
	}
	var se *dara.SDKError
	if errors.As(err, &se) {
		return fmt.Errorf("阿里云%s失败：code=%s message=%s", what, dara.StringValue(se.Code), dara.StringValue(se.Message))
	}
	return fmt.Errorf("调用阿里云%s失败：%w", what, err)
}

// chunks 按字数切分，尽量在换行处断开。
func chunks(s string, n int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for utf8.RuneCountInString(s) > n {
		r := []rune(s)
		cut := n
		if i := strings.LastIndex(string(r[:n]), "\n"); i > 0 {
			cut = utf8.RuneCountInString(string(r[:n])[:i]) + 1
		}
		out = append(out, string(r[:cut]))
		s = string(r[cut:])
	}
	return append(out, s)
}
