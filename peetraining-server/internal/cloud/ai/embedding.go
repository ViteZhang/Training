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

type EmbeddingResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
	Error json.RawMessage `json:"error"`
}

// Embed 不带聊天模型特有字段。错误只返回状态类别，不回显供应商响应、正文或凭证。
func (c *OpenAI) Embed(ctx context.Context, model string, dimensions int, input []string) (EmbeddingResponse, error) {
	var out EmbeddingResponse
	data, err := json.Marshal(struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		Dimensions     int      `json:"dimensions"`
		EncodingFormat string   `json:"encoding_format"`
	}{model, input, dimensions, "float"})
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/embeddings", bytes.NewReader(data))
	if err != nil {
		return out, errors.New("embedding: 地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return out, errors.New("embedding: 请求失败或超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("embedding: HTTP %d", resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return out, errors.New("embedding: 响应超限或无法读取")
	}
	if json.Unmarshal(data, &out) != nil || len(out.Error) > 0 && string(out.Error) != "null" {
		return out, errors.New("embedding: 响应格式错误")
	}
	return out, nil
}
