package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAI(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		switch got.Model {
		case "busy":
			w.WriteHeader(http.StatusTooManyRequests)
		case "empty":
			_, _ = w.Write([]byte(`{"choices":[]}`))
		case "html":
			_, _ = w.Write([]byte(`<html>`))
		default:
			_, _ = w.Write([]byte(`{"model":"qwen-x","choices":[{"message":{"role":"assistant","content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))
		}
	}))
	defer srv.Close()

	c := NewOpenAI(srv.URL+"/v1/", "k")
	resp, err := c.Complete(context.Background(), Request{Model: "qwen", JSONMode: true, Temperature: 0.2,
		Messages: []Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"ok":true}` || resp.InputTokens != 12 || resp.OutputTokens != 3 || resp.Model != "qwen-x" {
		t.Fatalf("%+v", resp)
	}
	if got.ResponseFormat == nil || got.ResponseFormat.Type != "json_object" || len(got.Messages) != 2 || got.Temperature != 0.2 {
		t.Fatalf("请求：%+v", got)
	}
	if _, err := c.Complete(context.Background(), Request{Model: "busy"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("限流：%v", err)
	}
	for _, m := range []string{"empty", "html"} {
		if _, err := c.Complete(context.Background(), Request{Model: m}); err == nil {
			t.Fatalf("%s 应报错", m)
		}
	}
	bad := NewOpenAI(srv.URL+"/v1", "wrong")
	if _, err := bad.Complete(context.Background(), Request{Model: "x"}); err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("鉴权失败：%v", err)
	}
}
