package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type fakeFlags map[uint64]bool

func (f fakeFlags) Enabled(_ context.Context, _ string, uid uint64) (bool, error) { return f[uid], nil }

// 功能开关关闭时接口返回 404；对指定用户打开时只有该用户能访问（ADR 0009）。
func TestFeatureGate(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if c.GetHeader("X-User") == "7" {
			c.Set(ctxUserID, uint64(7))
		}
	}, Errors(), FeatureGate(fakeFlags{7: true}, map[string]string{"POST /orders": "online_payment"}))
	r.POST("/orders", func(c *gin.Context) { c.Status(http.StatusCreated) })
	r.GET("/open", func(c *gin.Context) { c.Status(http.StatusOK) })

	call := func(method, path, user string) int {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-User", user)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if code := call(http.MethodPost, "/orders", "1"); code != http.StatusNotFound {
		t.Errorf("关闭时应 404，got %d", code)
	}
	if code := call(http.MethodPost, "/orders", "7"); code != http.StatusCreated {
		t.Errorf("对指定用户打开，got %d", code)
	}
	if code := call(http.MethodGet, "/open", "1"); code != http.StatusOK {
		t.Errorf("不受开关控制的接口，got %d", code)
	}
}
