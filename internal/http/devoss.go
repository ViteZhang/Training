package http

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/cloud/oss"
)

// devOSSUpload 模拟 OSS 预签名直传，让本地与真机调试不需要真实 OSS。只在非生产环境注册。
func devOSSUpload(m *oss.Mock) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimPrefix(c.Param("key"), "/")
		if !m.VerifyUpload(key, c.Query("exp"), c.Query("sig")) {
			_ = c.Error(ErrForbidden())
			return
		}
		data, err := io.ReadAll(io.LimitReader(c.Request.Body, 60<<20))
		if err != nil {
			_ = c.Error(ErrBadRequest("上传失败").Wrap(err))
			return
		}
		if err := m.Put(c.Request.Context(), key, data, c.GetHeader("Content-Type")); err != nil {
			_ = c.Error(err)
			return
		}
		c.Status(http.StatusOK)
	}
}
