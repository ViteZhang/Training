package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if got.EnableThinking != nil {
		t.Fatalf("没设 Thinking 时不发 enable_thinking（中转接口不认）：%v", *got.EnableThinking)
	}
	off := false
	c.Thinking = &off
	if _, err := c.Complete(context.Background(), Request{Model: "qwen"}); err != nil || got.EnableThinking == nil || *got.EnableThinking {
		t.Fatalf("百炼关思考应发 enable_thinking=false：%v %v", err, got.EnableThinking)
	}
	c.Thinking = nil
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

func sse(w http.ResponseWriter, lines ...string) {
	for _, l := range lines {
		_, _ = w.Write([]byte("data: " + l + "\n\n"))
		w.(http.Flusher).Flush()
	}
}

func TestOpenAIStream(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		switch got.Model {
		case "slow":
			// 总共 600ms，但每 100ms 都有输出：不算空闲超时。
			for i := 0; i < 6; i++ {
				time.Sleep(100 * time.Millisecond)
				sse(w, `{"choices":[{"delta":{"content":"段"}}]}`)
			}
			sse(w, "[DONE]")
		case "stuck":
			sse(w, `{"choices":[{"delta":{"content":"{"}}]}`)
			time.Sleep(time.Second)
		case "broken":
			sse(w, `{"choices":[{"delta":{"content":"{"}}]}`, `{"error":{"message":"内部错误"}}`)
		default:
			_, _ = w.Write([]byte(": ping\n\n"))
			sse(w,
				`{"model":"qwen-x","choices":[{"delta":{"role":"assistant","content":""}}]}`,
				`{"model":"qwen-x","choices":[{"delta":{"content":"{\"ok\":"}}]}`,
				`{"model":"qwen-x","choices":[{"delta":{"content":"true}"}}]}`,
				`{"model":"qwen-x","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
				"[DONE]")
		}
	}))
	defer srv.Close()

	c := NewOpenAI(srv.URL, "k")
	c.IdleTimeout = 300 * time.Millisecond
	resp, err := c.Complete(context.Background(), Request{Model: "qwen", JSONMode: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"ok":true}` || resp.InputTokens != 12 || resp.OutputTokens != 3 || resp.Model != "qwen-x" {
		t.Fatalf("%+v", resp)
	}
	if !got.Stream || got.StreamOptions == nil || !got.StreamOptions.IncludeUsage {
		t.Fatalf("应请求流式返回：%+v", got)
	}
	if resp, err := c.Complete(context.Background(), Request{Model: "slow"}); err != nil || resp.Content != "段段段段段段" {
		t.Fatalf("持续输出不应超时：%v %+v", err, resp)
	}
	if _, err := c.Complete(context.Background(), Request{Model: "stuck"}); !errors.Is(err, ErrIdleTimeout) {
		t.Fatalf("长时间没有输出应报空闲超时：%v", err)
	}
	if _, err := c.Complete(context.Background(), Request{Model: "broken"}); err == nil || !strings.Contains(err.Error(), "内部错误") {
		t.Fatalf("生成中断应报错：%v", err)
	}
	c.MaxDuration = 350 * time.Millisecond
	if _, err := c.Complete(context.Background(), Request{Model: "slow"}); err == nil || !strings.Contains(err.Error(), "单次调用超过") {
		t.Fatalf("超过总时长应报错：%v", err)
	}
}

func TestOpenAIMultimodalContent(t *testing.T) {
	var got chatRequest
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer srv.Close()
	image := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}
	c := NewOpenAI(srv.URL, "test")
	_, err := c.Complete(context.Background(), Request{Messages: []Message{
		{Role: "system", Content: "literal transcription"},
		{Role: "user", Content: "two pages", Images: []Image{{Data: image}, {Data: image}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].Content != "literal transcription" {
		t.Fatalf("原有文字请求格式改变：%+v", got.Messages[0])
	}
	parts, ok := got.Messages[1].Content.([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("多图消息：%+v", got.Messages[1])
	}
	if parts[0].(map[string]any)["text"] != "two pages" {
		t.Fatal("缺少文字指令")
	}
	for _, p := range parts[1:] {
		part := p.(map[string]any)
		img := part["image_url"].(map[string]any)
		if part["type"] != "image_url" || img["detail"] != "high" || img["url"] != "data:image/jpeg;base64,/9j/4AAQ" {
			t.Fatalf("图片没有按私有字节传输：%+v", part)
		}
	}
	_, err = c.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Images: []Image{{Data: []byte("not an image")}}}}})
	if !errors.Is(err, ErrBadImage) || hits != 1 {
		t.Fatalf("无效图片必须在调用平台前拒绝：hits=%d err=%v", hits, err)
	}
}

func TestImageMIME(t *testing.T) {
	for _, tc := range []struct {
		data []byte
		want string
	}{
		{[]byte("\x89PNG\r\n\x1a\n"), "image/png"},
		{[]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), "image/webp"},
		{[]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), "image/heic"},
		{[]byte("\x00\x00\x00\x18ftypmif1\x00\x00\x00\x00heicmif1"), "image/heic"},
		{[]byte("\x00\x00\x00\x18ftypavif\x00\x00\x00\x00mif1avif"), ""},
		{[]byte("not-an-image"), ""},
	} {
		if got := imageMIME(tc.data); got != tc.want {
			t.Fatalf("MIME=%q want=%q", got, tc.want)
		}
	}
}
