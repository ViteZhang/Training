package sms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
)

// DefaultAliyunEndpoint 是阿里云国内短信接口地址（发往中国内地手机号）。
const DefaultAliyunEndpoint = "dysmsapi.aliyuncs.com"

// DefaultTemplateParam 是验证码模板里的变量名：阿里云验证码模板通常写成「验证码为：${code}」。
const DefaultTemplateParam = "code"

// 超时（毫秒）：验证码请求在用户等待中，不能长时间挂起。
const (
	aliyunConnectTimeoutMS = 5000
	aliyunReadTimeoutMS    = 10000
)

// AliyunConfig 是阿里云短信配置。凭证复用 ALIYUN_ACCESS_KEY_ID / SECRET（RAM 子账号需有短信发送权限）。
type AliyunConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	SignName        string // 已审核通过的短信签名，如「考研Training」
	TemplateCode    string // 已审核通过的验证码模板，如 SMS_123456789
	TemplateParam   string // 模板里的验证码变量名，默认 code
	Endpoint        string // 默认 dysmsapi.aliyuncs.com
}

// sendSmsAPI 是 SDK 客户端里用到的方法，测试时替换成假实现。
type sendSmsAPI interface {
	SendSmsWithOptions(req *dysmsapi.SendSmsRequest, runtime *util.RuntimeOptions) (*dysmsapi.SendSmsResponse, error)
}

// Aliyun 通过阿里云短信服务（dysmsapi）发送验证码。
type Aliyun struct {
	api           sendSmsAPI
	signName      string
	templateCode  string
	templateParam string
}

// AliyunError 是阿里云返回的业务错误（Code 不为 OK），例如签名或模板不可用、触发流控。
// 不含手机号，可以直接写日志。
type AliyunError struct {
	Code      string
	Message   string
	RequestID string
}

func (e *AliyunError) Error() string {
	return fmt.Sprintf("阿里云短信发送失败：code=%s message=%s request_id=%s", e.Code, e.Message, e.RequestID)
}

// NewAliyun 校验配置并创建客户端。缺任何一项都在启动时报错，而不是等到用户登录时才发现。
func NewAliyun(cfg AliyunConfig) (*Aliyun, error) {
	var missing []string
	for name, v := range map[string]string{
		"ALIYUN_ACCESS_KEY_ID":     cfg.AccessKeyID,
		"ALIYUN_ACCESS_KEY_SECRET": cfg.AccessKeySecret,
		"SMS_SIGN_NAME":            cfg.SignName,
		"SMS_TEMPLATE_CODE":        cfg.TemplateCode,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("SMS_PROVIDER=aliyun 缺少配置：%s", strings.Join(missing, "、"))
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultAliyunEndpoint
	}
	if cfg.TemplateParam == "" {
		cfg.TemplateParam = DefaultTemplateParam
	}
	client, err := dysmsapi.NewClient(&openapi.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.AccessKeySecret),
		Endpoint:        tea.String(cfg.Endpoint),
		ConnectTimeout:  tea.Int(aliyunConnectTimeoutMS),
		ReadTimeout:     tea.Int(aliyunReadTimeoutMS),
	})
	if err != nil {
		return nil, fmt.Errorf("创建阿里云短信客户端：%w", err)
	}
	return newAliyunWithAPI(client, cfg), nil
}

func newAliyunWithAPI(api sendSmsAPI, cfg AliyunConfig) *Aliyun {
	if cfg.TemplateParam == "" {
		cfg.TemplateParam = DefaultTemplateParam
	}
	return &Aliyun{api: api, signName: cfg.SignName, templateCode: cfg.TemplateCode, templateParam: cfg.TemplateParam}
}

// SendCode 发送一条验证码短信。
func (a *Aliyun) SendCode(ctx context.Context, phone, code string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	param, err := json.Marshal(map[string]string{a.templateParam: code})
	if err != nil {
		return err
	}
	req := &dysmsapi.SendSmsRequest{
		PhoneNumbers:  tea.String(phone),
		SignName:      tea.String(a.signName),
		TemplateCode:  tea.String(a.templateCode),
		TemplateParam: tea.String(string(param)),
	}
	// 不自动重试：请求可能已经到达阿里云，重试会让用户收到两条不同的验证码。
	runtime := &util.RuntimeOptions{
		Autoretry:      tea.Bool(false),
		ConnectTimeout: tea.Int(aliyunConnectTimeoutMS),
		ReadTimeout:    tea.Int(aliyunReadTimeoutMS),
	}
	resp, err := a.api.SendSmsWithOptions(req, runtime)
	if err != nil {
		// SDK 错误里可能带请求参数（含手机号），只保留错误码与说明，避免手机号进日志（CLAUDE.md 第 11 条）。
		var sdkErr *tea.SDKError
		if errors.As(err, &sdkErr) {
			return &AliyunError{Code: tea.StringValue(sdkErr.Code), Message: tea.StringValue(sdkErr.Message)}
		}
		// 网络错误（*url.Error）会带完整请求地址，而 RPC 接口把手机号放在查询参数里，只保留底层原因。
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("调用阿里云短信接口失败：%s：%w", urlErr.Op, urlErr.Err)
		}
		return fmt.Errorf("调用阿里云短信接口失败：%w", err)
	}
	if resp == nil || resp.Body == nil {
		return errors.New("阿里云短信接口返回为空")
	}
	if c := tea.StringValue(resp.Body.Code); c != "OK" {
		return &AliyunError{Code: c, Message: tea.StringValue(resp.Body.Message), RequestID: tea.StringValue(resp.Body.RequestId)}
	}
	return nil
}
