package http

import (
	"fmt"
	"net/http"
	"testing"

	"peetraining-server/internal/cloud"
)

func TestDisabledServiceResponse(t *testing.T) {
	err := AsError(fmt.Errorf("upstream: %w", cloud.ErrDisabled))
	if err.Status != http.StatusServiceUnavailable || err.Code != CodeServiceUnavailable {
		t.Fatalf("关闭的依赖应返回 503 SERVICE_UNAVAILABLE，实际 %+v", err)
	}
}
