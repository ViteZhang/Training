package http

import (
	"context"
	"encoding/json"
	"net/http"
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
	w := do(r, http.MethodGet, "/api/v1/subjects/1/knowledge-tree", nil)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d，body = %s", w.Code, w.Body)
	}
	var e Error
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if e.Code != CodeNotImplemented {
		t.Errorf("code = %s", e.Code)
	}
}

// 路径参数格式错误统一为 400 BAD_REQUEST。
func TestBadPathParamIs400(t *testing.T) {
	r, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	w := do(r, http.MethodGet, "/api/v1/subjects/abc/knowledge-tree", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d，body = %s", w.Code, w.Body)
	}
}

func TestAIFailedError(t *testing.T) {
	if e := ErrAIFailed(); e.Status != http.StatusBadGateway || e.Code != CodeAIFailed {
		t.Errorf("%+v", e)
	}
}
