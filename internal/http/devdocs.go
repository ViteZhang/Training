package http

import (
	_ "embed"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
)

// 页面随二进制发布，不依赖 GoLand 的工作目录。
//
//go:embed devdocs.html
var devDocsHTML []byte

func registerDevDocs(r *gin.Engine) error {
	// 使用与请求校验器相同的内嵌契约，确保文档和正在运行的代码一致。
	spec, err := gen.GetSwagger()
	if err != nil {
		return fmt.Errorf("加载本地 OpenAPI 文档：%w", err)
	}
	document, err := spec.MarshalJSON()
	if err != nil {
		return fmt.Errorf("生成本地 OpenAPI 文档：%w", err)
	}
	r.GET("/openapi.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", document)
	})
	page := func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", devDocsHTML)
	}
	r.GET("/docs", page)
	r.GET("/docs/", page)
	return nil
}
