package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"peetraining-server/internal/cloud/oss"
)

// Qwen 调用语音识别大模型，使用独立模型配置，不套用文字/视觉模型参数。
type Qwen struct {
	baseURL, key, model string
	store               oss.Store
	client              *http.Client
}

func NewQwen(baseURL, key, model string, store oss.Store) (*Qwen, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || key == "" || store == nil {
		return nil, errors.New("语音模型需要 ASR_BASE_URL、ASR_API_KEY 和文件存储配置")
	}
	if model == "" {
		model = "qwen3-asr-flash"
	}
	return &Qwen{baseURL: strings.TrimRight(baseURL, "/"), key: key, model: model, store: store,
		client: &http.Client{Timeout: 165 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (q *Qwen) ModelName() string { return q.model }

func (q *Qwen) Transcribe(ctx context.Context, audio Audio) (string, error) {
	info, err := q.store.Head(ctx, audio.ObjectKey)
	if err != nil || !info.Exists || info.Size <= 0 || info.Size > 10<<20 {
		return "", errors.New("录音不存在、为空或超过 10MB")
	}
	p, err := q.store.PresignGet(ctx, audio.ObjectKey, "", 10*time.Minute)
	if err != nil {
		return "", errors.New("无法读取录音")
	}
	body := map[string]any{
		"model":    q.model,
		"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": p.URL}}}}},
		"stream":   false, "asr_options": map[string]any{"enable_itn": false},
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", errors.New("无法创建语音识别请求")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, q.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", errors.New("无法创建语音识别请求")
	}
	req.Header.Set("Authorization", "Bearer "+q.key)
	req.Header.Set("Content-Type", "application/json")
	r, err := q.client.Do(req)
	if err != nil {
		return "", errors.New("语音模型连接失败，请重试")
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return "", errors.New("语音模型暂时不可用，请重试")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 512<<10)).Decode(&result); err != nil || len(result.Choices) != 1 {
		return "", errors.New("语音模型返回结果无效")
	}
	text := strings.TrimSpace(result.Choices[0].Message.Content)
	if text == "" || len([]rune(text)) > 20000 {
		return "", errors.New("没有识别到有效语音，请重新录制")
	}
	return text, nil
}
