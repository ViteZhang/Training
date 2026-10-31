package vector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQdrantProtocolScopesAndCompletion(t *testing.T) {
	scope := Scope{"1", "2", "3", "4"}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("api-key") != "private-key" {
			t.Error("未附私有服务凭证")
		}
		result := any(true)
		switch {
		case r.Method == "GET":
			result = map[string]any{"status": "green", "config": map[string]any{"params": map[string]any{"vectors": map[string]any{"size": 256, "distance": "Cosine"}}}}
		case strings.HasSuffix(r.URL.Path, "/points"):
			var in struct {
				Points []Point `json:"points"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if r.URL.Query().Get("wait") != "true" || len(in.Points) != 1 || len(in.Points[0].Vector) != 256 || in.Points[0].Payload["generation"] != "4" {
				t.Error("写入协议", in)
			}
			result = map[string]string{"status": "completed"}
		case strings.HasSuffix(r.URL.Path, "/count"), strings.HasSuffix(r.URL.Path, "/search"):
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			filter, _ := json.Marshal(in["filter"])
			for key, value := range scope.payload() {
				if !strings.Contains(string(filter), `"key":"`+key+`"`) || !strings.Contains(string(filter), `"value":"`+value+`"`) {
					t.Error("缺少范围过滤", string(filter))
				}
			}
			if strings.HasSuffix(r.URL.Path, "/count") {
				result = map[string]int{"count": 1}
			} else {
				result = []Hit{{ID: 1, Score: .9, Payload: scope.payload()}}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "status": "ok"})
	}))
	defer srv.Close()
	q := New(srv.URL, "private-key")
	ctx := context.Background()
	name := "tb_0123456789abcdef_4"
	if err := q.Ensure(ctx, name, 256); err != nil {
		t.Fatal(err)
	}
	if err := q.Upsert(ctx, name, scope, 1, make([]float64, 256)); err != nil {
		t.Fatal(err)
	}
	if err := q.Verify(ctx, name, 256, scope, 1); err != nil {
		t.Fatal(err)
	}
	hits, err := q.Search(ctx, name, scope, make([]float64, 256), 20)
	if err != nil || len(hits) != 1 {
		t.Fatal(hits, err)
	}
	if calls != 6 {
		t.Fatal(calls)
	}
	if q.Ensure(ctx, "../invalid", 256) == nil {
		t.Fatal("集合名未验证")
	}
}
func TestQdrantRejectsWrongScopeOrConfig(t *testing.T) {
	for _, mode := range []string{"wrong-scope", "wrong-dimension", "incomplete-write", "incomplete-count", "error"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var result any
				switch {
				case r.Method == "GET":
					result = map[string]any{"status": "green", "config": map[string]any{"params": map[string]any{"vectors": map[string]any{"size": 512, "distance": "Cosine"}}}}
				case strings.HasSuffix(r.URL.Path, "/search"):
					result = []Hit{{ID: 1, Payload: map[string]string{"book": "wrong"}}}
				case strings.HasSuffix(r.URL.Path, "/count"):
					result = map[string]int{"count": 0}
				default:
					result = map[string]string{"status": "acknowledged"}
				}
				if mode == "error" {
					w.WriteHeader(500)
					_, _ = w.Write([]byte("secret"))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "status": "ok"})
			}))
			defer srv.Close()
			q := New(srv.URL, "")
			ctx := context.Background()
			name := "tb_0123456789abcdef_4"
			var err error
			switch mode {
			case "wrong-scope":
				_, err = q.Search(ctx, name, Scope{"1", "2", "3", "4"}, []float64{1}, 20)
			case "incomplete-write":
				err = q.Upsert(ctx, name, Scope{}, 1, []float64{1})
			case "incomplete-count":
				err = q.Verify(ctx, name, 512, Scope{}, 1)
			default:
				err = q.Ensure(ctx, name, 256)
			}
			if err == nil {
				t.Fatal("错误响应被接受")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("响应内容泄漏")
			}
		})
	}
}
