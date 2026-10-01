package ai

import (
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
type OpenAI struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewOpenAI 创建客户端。baseURL 形如 https://dashscope.aliyuncs.com/compatible-mode/v1。
func NewOpenAI(baseURL, apiKey string) *OpenAI {
	return &OpenAI{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, HTTP: &http.Client{Timeout: 3 * time.Minute}}
}

// ErrRateLimited 表示平台限流或暂时不可用，调用方可以稍后重试。
var ErrRateLimited = errors.New("ai: 平台限流或暂时不可用")

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func (c *OpenAI) Complete(ctx context.Context, req Request) (Response, error) {
	body := chatRequest{Model: req.Model, Temperature: req.Temperature}
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
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(hreq)
	if err != nil {
		return Response{}, fmt.Errorf("ai: 请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return Response{}, fmt.Errorf("%w：HTTP %d", ErrRateLimited, resp.StatusCode)
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("ai: 返回不是 JSON（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || out.Error != nil {
		msg := ""
		if out.Error != nil {
			msg = out.Error.Message
		}
		// 不把请求内容写进错误，避免考生原文进日志。
		return Response{}, fmt.Errorf("ai: HTTP %d：%s", resp.StatusCode, msg)
	}
	if len(out.Choices) == 0 {
		return Response{}, errors.New("ai: 返回没有内容")
	}
	return Response{
		Content: out.Choices[0].Message.Content, Model: out.Model,
		InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens,
	}, nil
}
