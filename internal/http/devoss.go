package http

import (
	"io"
	"net/http"
	"net/url"
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

// devOSSDownload 是本地 mock OSS 的下载入口（导出题库的短时效链接），校验与上传相同的签名。
func devOSSDownload(m *oss.Mock) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimPrefix(c.Param("key"), "/")
		if !m.VerifyUpload(key, c.Query("exp"), c.Query("sig")) {
			_ = c.Error(ErrForbidden())
			return
		}
		r, err := m.Get(c.Request.Context(), key)
		if err != nil {
			_ = c.Error(ErrNotFound())
			return
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			_ = c.Error(err)
			return
		}
		if name := c.Query("name"); name != "" {
			c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
		}
		c.Data(http.StatusOK, "application/octet-stream", data)
	}
}
