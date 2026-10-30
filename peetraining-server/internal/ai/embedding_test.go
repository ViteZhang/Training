package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"peetraining-server/internal/config"
	"testing"
)

func TestEmbeddingProtocolAndValidation(t *testing.T) {
	for _, mode := range []string{"valid", "dimensions", "duplicate", "missing", "wrong-model", "zero", "error", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/embeddings" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("协议错误")
				}
				var req map[string]any
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					t.Error("请求无效")
				}
				if len(req) != 4 || req["dimensions"] != float64(256) || req["encoding_format"] != "float" {
					t.Error(req)
				}
				model := "qwen3.7-text-embedding-flash"
				if mode == "wrong-model" {
					model = "other"
				}
				dim := 256
				if mode == "dimensions" {
					dim = 512
				}
				v := make([]float64, dim)
				if mode != "zero" {
					v[0] = 1
				}
				rows := []any{map[string]any{"index": 1, "embedding": v}, map[string]any{"index": 0, "embedding": v}}
				if mode == "duplicate" {
					rows[1] = rows[0]
				}
				if mode == "missing" {
					rows = rows[:1]
				}
				if mode == "error" {
					w.WriteHeader(429)
					_, _ = w.Write([]byte("secret original text"))
					return
				}
				if mode == "oversize" {
					_, _ = w.Write(make([]byte, (4<<20)+1))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "data": rows})
			}))
			defer srv.Close()
			e := NewEmbedder(config.TextbookConfig{EmbeddingProvider: "openai", EmbeddingBaseURL: srv.URL, EmbeddingAPIKey: "secret", EmbeddingModel: "qwen3.7-text-embedding-flash", EmbeddingDimensions: 256})
			out, err := e.Embed(context.Background(), []string{"教材原文", "用户问题"})
			if (err == nil) != (mode == "valid") {
				t.Fatalf("%v", err)
			}
			if mode == "valid" && len(out) != 2 {
				t.Fatal(out)
			}
		})
	}
}
func TestEmbeddingProfileIsolation(t *testing.T) {
	c := config.TextbookConfig{EmbeddingProvider: "mock", EmbeddingDimensions: 256, EmbeddingModel: "test"}
	a := NewEmbedder(c).Profile()
	c.EmbeddingAPIKey = "rotated"
	if NewEmbedder(c).Profile().ID() != a.ID() {
		t.Fatal("轮换密钥不能改变索引配置")
	}
	c.EmbeddingDimensions = 512
	if NewEmbedder(c).Profile().ID() == a.ID() {
		t.Fatal("维度变化必须隔离")
	}
}
