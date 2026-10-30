package asr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peetraining-server/internal/cloud/oss"
)

func TestQwenRequestAndFailures(t *testing.T) {
	store := oss.NewMock()
	store.Seed("u/1/voice/a.m4a", []byte("audio"))
	for _, tc := range []struct {
		name     string
		status   int
		response string
		want     string
	}{
		{"transcribe", 200, `{"choices":[{"message":{"content":"这是考生原话。"}}]}`, "这是考生原话。"},
		{"empty", 200, `{"choices":[{"message":{"content":"  "}}]}`, ""},
		{"malformed", 200, `{"choices":[]}`, ""},
		{"upstream", 504, `secret signed-url answer`, ""},
		{"redirect", 302, ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("wrong endpoint or auth")
				}
				var body struct {
					Model    string `json:"model"`
					Stream   bool   `json:"stream"`
					Messages []struct {
						Role    string `json:"role"`
						Content []struct {
							Type  string `json:"type"`
							Audio struct {
								Data string `json:"data"`
							} `json:"input_audio"`
						} `json:"content"`
					} `json:"messages"`
					Options struct {
						ITN bool `json:"enable_itn"`
					} `json:"asr_options"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Model != "qwen3-asr-flash" || body.Stream || body.Options.ITN || len(body.Messages) != 1 || len(body.Messages[0].Content) != 1 || body.Messages[0].Content[0].Type != "input_audio" || !strings.Contains(body.Messages[0].Content[0].Audio.Data, "sig=") {
					t.Errorf("must send only signed audio to ASR: %+v", body)
				}
				if tc.status == 302 {
					w.Header().Set("Location", "http://127.0.0.1:1/do-not-follow")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			q, err := NewQwen(server.URL, "test-key", "", store)
			if err != nil {
				t.Fatal(err)
			}
			result, err := q.Transcribe(context.Background(), Audio{ObjectKey: "u/1/voice/a.m4a", Format: "m4a"})
			if tc.want != "" {
				if err != nil || result != tc.want {
					t.Fatalf("%q %v", result, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "test-key") {
				t.Fatalf("must sanitize failure: %v", err)
			}
		})
	}
	q, _ := NewQwen("http://localhost", "test-key", "", store)
	if _, err := q.Transcribe(context.Background(), Audio{ObjectKey: "missing"}); err == nil {
		t.Fatal("must reject missing recording before network call")
	}
}
