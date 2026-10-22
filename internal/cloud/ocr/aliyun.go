package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ocrapi "github.com/alibabacloud-go/ocr-api-20210707/v3/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
)

// DefaultAliyunEndpoint 是阿里云文字识别（ocr-api 2021-07-07）的地址：统一识别接口部署在华东 1（杭州），可用 OCR_ENDPOINT 覆盖。
const DefaultAliyunEndpoint = "ocr-api.cn-hangzhou.aliyuncs.com"

// lowConfidenceProb 是判为低置信度的阈值（接口返回 0–100），低于它的文字行在原文查看里标出，提醒用户核对。
const lowConfidenceProb = 85

// ErrBadImage 表示图片本身不能识别（格式不支持、尺寸或大小超限、内容为空）；重试也不会好。
var ErrBadImage = errors.New("图片无法识别")

// AliyunConfig 是阿里云文字识别配置。凭证复用 ALIYUN_ACCESS_KEY_ID / SECRET。
type AliyunConfig struct {
	AccessKeyID     string
	AccessKeySecret string
	Endpoint        string
}

type ocrAPI interface {
	RecognizeAdvancedWithOptions(req *ocrapi.RecognizeAdvancedRequest, rt *util.RuntimeOptions) (*ocrapi.RecognizeAdvancedResponse, error)
	RecognizeHandwritingWithOptions(req *ocrapi.RecognizeHandwritingRequest, rt *util.RuntimeOptions) (*ocrapi.RecognizeHandwritingResponse, error)
}

// Aliyun 用阿里云「通用文字识别 · 高精版」识别印刷体，「手写体识别」识别手写答案。
// 选型待 ADR 0007 的样本对比；接口换成教育场景时只改这里。
type Aliyun struct {
	api ocrAPI
}

var _ Recognizer = (*Aliyun)(nil)

// NewAliyun 校验配置并创建客户端。
func NewAliyun(cfg AliyunConfig) (*Aliyun, error) {
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		return nil, errors.New("OCR_PROVIDER=aliyun 需要设置 ALIYUN_ACCESS_KEY_ID 与 ALIYUN_ACCESS_KEY_SECRET")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultAliyunEndpoint
	}
	c, err := ocrapi.NewClient(&openapi.Config{
		AccessKeyId:     tea.String(cfg.AccessKeyID),
		AccessKeySecret: tea.String(cfg.AccessKeySecret),
		Endpoint:        tea.String(cfg.Endpoint),
	})
	if err != nil {
		return nil, fmt.Errorf("创建阿里云文字识别客户端：%w", err)
	}
	return &Aliyun{api: c}, nil
}

func runtime() *util.RuntimeOptions {
	return &util.RuntimeOptions{Autoretry: tea.Bool(true), MaxAttempts: tea.Int(2), ConnectTimeout: tea.Int(5000), ReadTimeout: tea.Int(30000)}
}

// Recognize 识别一张图片。图片必须带字节（调用方已从 OSS 读出），不走公网地址，免得图片对外可见。
func (a *Aliyun) Recognize(ctx context.Context, img Image) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(img.Data) == 0 {
		return Result{}, fmt.Errorf("%w：图片为空", ErrBadImage)
	}
	var data *string
	var code, msg, reqID *string
	var err error
	if img.Handwriting {
		var r *ocrapi.RecognizeHandwritingResponse
		r, err = a.api.RecognizeHandwritingWithOptions(&ocrapi.RecognizeHandwritingRequest{
			Body: bytes.NewReader(img.Data), NeedRotate: tea.Bool(true), NeedSortPage: tea.Bool(true),
		}, runtime())
		if err == nil && r.Body != nil {
			data, code, msg, reqID = r.Body.Data, r.Body.Code, r.Body.Message, r.Body.RequestId
		}
	} else {
		var r *ocrapi.RecognizeAdvancedResponse
		r, err = a.api.RecognizeAdvancedWithOptions(&ocrapi.RecognizeAdvancedRequest{
			Body: bytes.NewReader(img.Data), NeedRotate: tea.Bool(true), NeedSortPage: tea.Bool(true),
		}, runtime())
		if err == nil && r.Body != nil {
			data, code, msg, reqID = r.Body.Data, r.Body.Code, r.Body.Message, r.Body.RequestId
		}
	}
	if err != nil {
		return Result{}, sdkError(err)
	}
	if c := tea.StringValue(code); c != "" && c != "200" && c != "OK" {
		return Result{}, fmt.Errorf("阿里云文字识别失败：code=%s message=%s request_id=%s", c, tea.StringValue(msg), tea.StringValue(reqID))
	}
	return parseResult(tea.StringValue(data))
}

// parseResult 把接口返回的 Data（JSON 字符串）转成文字：按阅读顺序每个文字行一行，低置信度的行记下位置。
func parseResult(data string) (Result, error) {
	if data == "" {
		return Result{}, nil
	}
	var d struct {
		Content string `json:"content"`
		Words   []struct {
			Word string  `json:"word"`
			Prob float64 `json:"prob"`
		} `json:"prism_wordsInfo"`
	}
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return Result{}, fmt.Errorf("阿里云文字识别：返回格式不对：%w", err)
	}
	if len(d.Words) == 0 {
		return Result{Text: strings.TrimSpace(d.Content)}, nil
	}
	var b strings.Builder
	var spans []Span
	pos := 0
	for _, w := range d.Words {
		line := strings.TrimSpace(w.Word)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
			pos++
		}
		n := utf8.RuneCountInString(line)
		if w.Prob > 0 && w.Prob < lowConfidenceProb {
			spans = append(spans, Span{Start: pos, End: pos + n})
		}
		b.WriteString(line)
		pos += n
	}
	return Result{Text: b.String(), LowConfidence: spans}, nil
}

// sdkError 区分图片本身的问题（ErrBadImage，不重试）与服务、网络错误（重试）；只保留错误码与说明。
func sdkError(err error) error {
	var se *tea.SDKError
	if errors.As(err, &se) {
		c := tea.StringValue(se.Code)
		lc := strings.ToLower(c)
		if strings.Contains(lc, "image") || strings.Contains(lc, "nocontent") || strings.Contains(lc, "emptycontent") {
			return fmt.Errorf("%w：code=%s message=%s", ErrBadImage, c, tea.StringValue(se.Message))
		}
		return fmt.Errorf("阿里云文字识别失败：code=%s message=%s", c, tea.StringValue(se.Message))
	}
	return fmt.Errorf("调用阿里云文字识别失败：%w", err)
}
