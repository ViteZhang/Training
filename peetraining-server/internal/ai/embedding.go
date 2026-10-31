package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/config"
)

// EmbeddingProfile 不包含凭证；模型、维度、端点或预处理变动必须产生新索引代次。
type EmbeddingProfile struct {
	Provider   string `json:"provider"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	Preprocess string `json:"preprocess"`
}

func (p EmbeddingProfile) ID() string {
	data, _ := json.Marshal(p)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

type Embedder struct {
	audit   *Engine
	profile EmbeddingProfile
	client  *cloudai.OpenAI
}

func NewEmbedder(c config.TextbookConfig) *Embedder {
	client := cloudai.NewOpenAI(c.EmbeddingBaseURL, c.EmbeddingAPIKey)
	client.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Embedder{profile: EmbeddingProfile{c.EmbeddingProvider, strings.TrimRight(c.EmbeddingBaseURL, "/"), c.EmbeddingModel, c.EmbeddingDimensions, "verbatim-v1"}, client: client}
}
func (e *Embedder) WithAudit(engine *Engine) *Embedder { e.audit = engine; return e }
func (e *Embedder) Profile() EmbeddingProfile          { return e.profile }
func (e *Embedder) Ready() bool {
	return e != nil && (e.profile.Provider == "mock" || e.profile.Provider == "openai")
}
func (e *Embedder) Embed(ctx context.Context, input []string) (result [][]float64, retErr error) {
	start := time.Now()
	inputTokens := 0
	defer func() {
		if e != nil && e.audit != nil {
			kind := ""
			if retErr != nil {
				kind = "embedding_failed"
			}
			e.audit.record(ctx, Def{Name: "textbook_embedding"}, Meta{Model: e.profile.Model, Version: e.profile.Preprocess}, 0, cloudai.Response{InputTokens: inputTokens}, start, kind, false)
		}
	}()

	if !e.Ready() {
		return nil, errors.New("embedding: 未配置")
	}
	if len(input) < 1 || len(input) > 16 {
		return nil, errors.New("embedding: 批次超限")
	}
	for _, v := range input {
		if strings.TrimSpace(v) == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > 2000 {
			return nil, errors.New("embedding: 文本为空或超限")
		}
	}
	if e.profile.Provider == "mock" {
		out := make([][]float64, len(input))
		for i, v := range input {
			out[i] = make([]float64, e.profile.Dimensions)
			h := sha256.Sum256([]byte(v))
			for j := range out[i] {
				out[i][j] = (float64(h[j%len(h)]) - 127.5) / 128
			}
		}
		return out, nil
	}
	resp, err := e.client.Embed(ctx, e.profile.Model, e.profile.Dimensions, input)
	if err != nil {
		return nil, err
	}
	inputTokens = resp.Usage.PromptTokens
	if resp.Model != e.profile.Model || len(resp.Data) != len(input) {
		return nil, errors.New("embedding: 模型或数量不一致")
	}
	out := make([][]float64, len(input))
	for _, v := range resp.Data {
		if v.Index < 0 || v.Index >= len(out) || out[v.Index] != nil || len(v.Embedding) != e.profile.Dimensions {
			return nil, errors.New("embedding: 序号或维度不一致")
		}
		norm := 0.0
		for _, f := range v.Embedding {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, errors.New("embedding: 非有限数值")
			}
			norm += f * f
		}
		if norm == 0 || math.IsInf(norm, 0) {
			return nil, errors.New("embedding: 无效向量")
		}
		out[v.Index] = v.Embedding
	}
	return out, nil
}
