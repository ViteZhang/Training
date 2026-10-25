package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAI 调用 OpenAI 兼容的 Chat Completions 接口（阿里云百炼、火山方舟等国内平台都提供）。
//
// 用流式返回（stream=true）：导入时模型一次要输出上万字（知识点要逐字带原文），非流式要等全部生成完才有响应头，
// 慢一点的模型就会超时；流式只要模型在持续输出就一直等，连续 IdleTimeout 没有新内容才算超时，单次调用最长 MaxDuration。
type OpenAI struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// IdleTimeout 是连续没有收到任何数据的最长时间（含等待响应头）。
	IdleTimeout time.Duration
	// MaxDuration 是单次调用的总时长上限。
	MaxDuration time.Duration
}

// 默认超时：模型开始输出前可能要先思考一段时间，空闲上限给到 90 秒；整段输出最长 10 分钟。
const (
	defaultIdleTimeout = 90 * time.Second
	defaultMaxDuration = 10 * time.Minute
)

// NewOpenAI 创建客户端。baseURL 形如 https://dashscope.aliyuncs.com/compatible-mode/v1。
func NewOpenAI(baseURL, apiKey string) *OpenAI {
	return &OpenAI{
		BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey,
		// 不设 http.Client.Timeout：它包含读完整个响应体的时间，会截断长输出；超时由 IdleTimeout 与 MaxDuration 控制。
		HTTP:        &http.Client{},
		IdleTimeout: defaultIdleTimeout, MaxDuration: defaultMaxDuration,
	}
}

// ErrRateLimited 表示平台限流或暂时不可用，调用方可以稍后重试。
var ErrRateLimited = errors.New("ai: 平台限流或暂时不可用")

// ErrIdleTimeout 表示模型长时间没有输出任何内容。
var ErrIdleTimeout = errors.New("ai: 模型长时间没有输出")

type chatRequest struct {
	Model         string        `json:"model"`
	Messages      []chatMessage `json:"messages"`
	Temperature   float64       `json:"temperature"`
	Stream        bool          `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type apiError struct {
	Message string `json:"message"`
	Code    any    `json:"code"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage usage     `json:"usage"`
	Error *apiError `json:"error"`
}

// chatChunk 是流式返回的一段。
type chatChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *usage    `json:"usage"`
	Error *apiError `json:"error"`
}

func (c *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	body := chatRequest{Model: req.Model, Temperature: req.Temperature, Stream: true}
	body.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, chatMessage(m))
	}
	if req.JSONMode {
		body.ResponseFormat = &struct {
			Type string `json:"type"`
		}{Type: "json_object"}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	maxDur, idle := c.MaxDuration, c.IdleTimeout
	if maxDur <= 0 {
		maxDur = defaultMaxDuration
	}
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, maxDur)
	defer cancel()
	// 空闲计时器：每收到一段数据就重置；到点取消请求。
	idleCtx, idleCancel := context.WithCancelCause(ctx)
	defer idleCancel(nil)
	timer := time.AfterFunc(idle, func() { idleCancel(ErrIdleTimeout) })
	defer timer.Stop()

	hreq, err := http.NewRequestWithContext(idleCtx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "text/event-stream")
	hreq.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(hreq)
	if err != nil {
		return Response{}, c.wrapErr(idleCtx, err)
	}
	defer resp.Body.Close()
	timer.Reset(idle)

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return Response{}, fmt.Errorf("%w：HTTP %d", ErrRateLimited, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		// 出错时、或接口不支持流式时返回的是整段 JSON。
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return Response{}, c.wrapErr(idleCtx, err)
		}
		return parseWhole(resp.StatusCode, raw)
	}
	return c.readStream(idleCtx, resp.Body, func() { timer.Reset(idle) })
}

// wrapErr 把超时换成可读的原因；不带请求内容，避免考生原文进日志。
func (c *OpenAI) wrapErr(ctx context.Context, err error) error {
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, ErrIdleTimeout):
		return fmt.Errorf("%w（超过 %s）", ErrIdleTimeout, c.IdleTimeout)
	case errors.Is(cause, context.DeadlineExceeded):
		return fmt.Errorf("ai: 单次调用超过 %s 仍未完成", c.MaxDuration)
	}
	return fmt.Errorf("ai: 请求失败：%w", err)
}

func parseWhole(status int, raw []byte) (Response, error) {
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("ai: 返回不是 JSON（HTTP %d）", status)
	}
	if status != http.StatusOK || out.Error != nil {
		msg := ""
		if out.Error != nil {
			msg = out.Error.Message
		}
		return Response{}, fmt.Errorf("ai: HTTP %d：%s", status, msg)
	}
	if len(out.Choices) == 0 {
		return Response{}, errors.New("ai: 返回没有内容")
	}
	return Response{
		Content: out.Choices[0].Message.Content, Model: out.Model,
		InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens,
	}, nil
}

// readStream 读 SSE：每行「data: {...}」是一段，「data: [DONE]」结束。
func (c *OpenAI) readStream(ctx context.Context, r io.Reader, touch func()) (Response, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var out Response
	var content strings.Builder
	done := false
	for sc.Scan() {
		touch()
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue // 空行、注释（心跳）、event: 行
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			done = true
			break
		}
		var ch chatChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			return Response{}, errors.New("ai: 流式返回格式不对")
		}
		if ch.Error != nil {
			return Response{}, fmt.Errorf("ai: 生成中断：%s", ch.Error.Message)
		}
		if ch.Model != "" {
			out.Model = ch.Model
		}
		for _, choice := range ch.Choices {
			content.WriteString(choice.Delta.Content)
		}
		if ch.Usage != nil {
			out.InputTokens, out.OutputTokens = ch.Usage.PromptTokens, ch.Usage.CompletionTokens
		}
	}
	if err := sc.Err(); err != nil {
		return Response{}, c.wrapErr(ctx, err)
	}
	if !done && ctx.Err() != nil {
		return Response{}, c.wrapErr(ctx, ctx.Err())
	}
	if content.Len() == 0 {
		return Response{}, errors.New("ai: 返回没有内容")
	}
	out.Content = content.String()
	return out, nil
}
