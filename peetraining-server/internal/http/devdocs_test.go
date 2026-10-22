package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"peetraining-server/internal/logx"
)

func TestDevDocs(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "关闭文档"
		if enabled {
			name = "开启本地文档"
		}
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			r, err := NewRouter(Deps{
				Logger:  logx.New(&logs, slog.LevelDebug),
				MySQL:   fakePinger{},
				Redis:   fakePinger{},
				Tokens:  fakeTokens{},
				DevDocs: enabled,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/docs", "/docs/", "/openapi.json"} {
				w := do(r, http.MethodGet, path, nil)
				if !enabled {
					if w.Code != http.StatusNotFound {
						t.Fatalf("文档关闭时 %s 返回 %d", path, w.Code)
					}
					continue
				}
				if w.Code != http.StatusOK {
					t.Fatalf("%s 返回 %d：%s", path, w.Code, w.Body)
				}
				if path != "/openapi.json" {
					if !strings.Contains(w.Header().Get("Content-Type"), "text/html") || !strings.Contains(w.Body.String(), "SwaggerUIBundle") {
						t.Fatal("没有返回可加载的 Swagger UI 页面")
					}
					continue
				}
				var spec struct {
					OpenAPI string `json:"openapi"`
					Servers []struct {
						URL string `json:"url"`
					} `json:"servers"`
					Paths map[string]json.RawMessage `json:"paths"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
					t.Fatal(err)
				}
				if spec.OpenAPI != "3.0.3" || len(spec.Servers) != 1 || spec.Servers[0].URL != APIPrefix {
					t.Fatal("文档的 API 版本或请求前缀不正确")
				}
				for _, path := range []string{"/health", "/bootstrap", "/auth/login"} {
					if _, ok := spec.Paths[path]; !ok {
						t.Fatalf("契约缺少路径 %s", path)
					}
				}
			}
			if w := do(r, http.MethodGet, "/api/v1/health", nil); w.Code != http.StatusOK {
				t.Fatal("文档路由影响了原有健康检查")
			}
		})
	}
}
