package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"peetraining-server/internal/gen"
)

// 契约本身必须是合法的 OpenAPI 3（前端按它生成客户端）。
func TestOpenAPISpecIsValid(t *testing.T) {
	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml 校验失败：%v", err)
	}
}

// 契约里有、还没实现的接口返回 501 NOT_IMPLEMENTED（T03 验收）。
func TestUnimplementedReturns501(t *testing.T) {
	r, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	w := do(r, http.MethodGet, "/api/v1/subjects/1/knowledge-tree", map[string]string{"Authorization": "Bearer user-1"})
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d，body = %s", w.Code, w.Body)
	}
	var e Error
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if e.Code != CodeNotImplemented {
		t.Errorf("code = %s", e.Code)
	}
}

// 需要登录的接口：没带令牌或令牌无效返回 401。
func TestAuthRequired(t *testing.T) {
	r, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	for _, h := range []map[string]string{nil, {"Authorization": "Bearer bad"}, {"Authorization": "Basic x"}} {
		w := do(r, http.MethodGet, "/api/v1/me", h)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), CodeUnauthorized) {
			t.Errorf("%v → %d %s", h, w.Code, w.Body)
		}
	}
}

// 请求体按契约校验：缺必填字段、格式不对返回 400。
func TestBodyValidation(t *testing.T) {
	r, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	for _, body := range []string{`{"purpose":"login"}`, `{"phone":"12345","purpose":"login"}`, `{"phone":"13812345678","purpose":"nope"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/sms-codes", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s → %d %s", body, w.Code, w.Body)
		}
	}
}

// 路径参数格式错误统一为 400 BAD_REQUEST。
func TestBadPathParamIs400(t *testing.T) {
	r, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	w := do(r, http.MethodGet, "/api/v1/subjects/abc/knowledge-tree", map[string]string{"Authorization": "Bearer user-1"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d，body = %s", w.Code, w.Body)
	}
}

func TestAIFailedError(t *testing.T) {
	if e := ErrAIFailed(); e.Status != http.StatusBadGateway || e.Code != CodeAIFailed {
		t.Errorf("%+v", e)
	}
}
