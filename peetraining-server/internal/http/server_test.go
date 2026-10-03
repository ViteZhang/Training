package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
	"peetraining-server/internal/logx"
)

type fakePinger struct{ err error }

// fakeTokens 把 "user-<id>" 当作有效令牌。
type fakeTokens struct{}

func (fakeTokens) ParseAccess(token string) (uint64, string, error) {
	if token == "user-1" {
		return 1, "dev-1", nil
	}
	return 0, "", errors.New("invalid")
}

func (f fakePinger) Ping(context.Context) error { return f.err }

func newTestRouter(t *testing.T, mysql, redis Pinger) (*gin.Engine, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	r, err := NewRouter(Deps{
		Logger:  logx.New(&buf, slog.LevelDebug),
		Version: "test-version",
		MySQL:   mysql,
		Redis:   redis,
		Tokens:  fakeTokens{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r, &buf
}

func do(r http.Handler, method, path string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHealthOK(t *testing.T) {
	r, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	w := do(r, http.MethodGet, "/api/v1/health", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d，body = %s", w.Code, w.Body)
	}
	var got gen.Health
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := gen.Health{Status: gen.HealthStatusOk, Version: "test-version", Mysql: gen.HealthStatusOk, Redis: gen.HealthStatusOk}
	if got != want {
		t.Errorf("got %+v，want %+v", got, want)
	}
}

func TestHealthDependencyDown(t *testing.T) {
	cases := map[string]struct {
		mysql, redis Pinger
		wantMySQL    gen.HealthStatus
		wantRedis    gen.HealthStatus
	}{
		"mysql 异常": {fakePinger{errors.New("dial tcp 10.0.0.2:3306: refused")}, fakePinger{}, gen.HealthStatusError, gen.HealthStatusOk},
		"redis 异常": {fakePinger{}, fakePinger{errors.New("timeout")}, gen.HealthStatusOk, gen.HealthStatusError},
		"未配置":      {nil, nil, gen.HealthStatusError, gen.HealthStatusError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := newTestRouter(t, tc.mysql, tc.redis)
			w := do(r, http.MethodGet, "/api/v1/health", nil)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d", w.Code)
			}
			if strings.Contains(w.Body.String(), "10.0.0.2") {
				t.Error("健康检查不应暴露连接细节")
			}
			var got gen.Health
			_ = json.Unmarshal(w.Body.Bytes(), &got)
			if got.Status != gen.HealthStatusError || got.Mysql != tc.wantMySQL || got.Redis != tc.wantRedis {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestNotFoundIsUnifiedError(t *testing.T) {
	r, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/nope"},
		{http.MethodPost, "/api/v1/health"},
	} {
		w := do(r, tc.method, tc.path, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s status = %d", tc.method, tc.path, w.Code)
		}
		var e Error
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if e.Code != CodeNotFound || e.Message == "" {
			t.Errorf("错误响应不符合 { code, message }：%s", w.Body)
		}
	}
}

func TestRequestID(t *testing.T) {
	r, logs := newTestRouter(t, fakePinger{}, fakePinger{})

	w := do(r, http.MethodGet, "/api/v1/health", map[string]string{HeaderRequestID: "client-req-123"})
	if got := w.Header().Get(HeaderRequestID); got != "client-req-123" {
		t.Errorf("应沿用合法的客户端请求 ID，got %q", got)
	}
	if !strings.Contains(logs.String(), `"request_id":"client-req-123"`) {
		t.Errorf("访问日志应带请求 ID：%s", logs)
	}

	w = do(r, http.MethodGet, "/api/v1/health", map[string]string{HeaderRequestID: "bad id\nwith newline"})
	if got := w.Header().Get(HeaderRequestID); got == "" || strings.Contains(got, " ") {
		t.Errorf("非法请求 ID 应被替换，got %q", got)
	}
}

func TestAccessLogHasNoQueryString(t *testing.T) {
	r, logs := newTestRouter(t, fakePinger{}, fakePinger{})
	do(r, http.MethodGet, "/api/v1/health?phone=13812345678", nil)
	if strings.Contains(logs.String(), "13812345678") || strings.Contains(logs.String(), "phone=") {
		t.Errorf("访问日志不应记录查询参数：%s", logs)
	}
	if !strings.Contains(logs.String(), `"route":"/api/v1/health"`) {
		t.Errorf("访问日志应记录路由模板：%s", logs)
	}
}

func TestErrorsMiddleware(t *testing.T) {
	var buf bytes.Buffer
	r := gin.New()
	r.Use(RequestID(logx.New(&buf, slog.LevelDebug)), Recovery(), Errors())
	r.GET("/quota", func(c *gin.Context) {
		_ = c.Error(ErrQuotaExceeded("今天的批改次数用完了").WithDetail(map[string]any{"remaining": 0}))
	})
	r.GET("/boom", func(c *gin.Context) { _ = c.Error(errors.New("db password=secret leaked")) })
	r.GET("/panic", func(c *gin.Context) { panic("oops") })

	w := do(r, http.MethodGet, "/quota", nil)
	if w.Code != http.StatusPaymentRequired || !strings.Contains(w.Body.String(), `"code":"QUOTA_EXCEEDED"`) || !strings.Contains(w.Body.String(), `"remaining":0`) {
		t.Errorf("quota: %d %s", w.Code, w.Body)
	}

	w = do(r, http.MethodGet, "/boom", nil)
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "secret") {
		t.Errorf("未知错误应返回 500 且不暴露内部信息：%d %s", w.Code, w.Body)
	}
	if !strings.Contains(buf.String(), "request failed") {
		t.Errorf("500 应记日志：%s", buf.String())
	}

	w = do(r, http.MethodGet, "/panic", nil)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), `"code":"INTERNAL"`) {
		t.Errorf("panic: %d %s", w.Code, w.Body)
	}
}

func TestErrorHelpers(t *testing.T) {
	cause := errors.New("root cause")
	e := ErrConflict("已有一套进行中的试卷").Wrap(cause)
	if !errors.Is(e, cause) {
		t.Error("Wrap 后应能 errors.Is 到原因")
	}
	if !strings.Contains(e.Error(), "root cause") {
		t.Error("Error() 应包含原因，便于日志排查")
	}
	if AsError(e) != e {
		t.Error("AsError 应原样返回 *Error")
	}
	if AsError(cause).Code != CodeInternal {
		t.Error("未知错误应转为 INTERNAL")
	}
	for _, fn := range []func() *Error{ErrUnauthorized, ErrForbidden, ErrNotFound, ErrInternal, ErrNotImplemented} {
		if fn().Message == "" || fn().Status == 0 {
			t.Errorf("%+v 缺少 message 或 status", fn())
		}
	}
	if ErrTooManyRequests("x").Status != http.StatusTooManyRequests || ErrBadRequest("x").Status != http.StatusBadRequest {
		t.Error("状态码不对")
	}
}
