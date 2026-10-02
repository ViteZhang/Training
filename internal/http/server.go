// Package http 负责路由、处理器与中间件。
//
// 接口签名由 oapi-codegen 按 api/openapi.yaml 生成在 internal/gen，这里只写实现。
package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/auth"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/material"
	"peetraining-server/internal/plan"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/profile"
	"peetraining-server/internal/quota"
)

// APIPrefix 是 App 接口的路由前缀；后台接口在 APIPrefix + "/admin" 下。
const APIPrefix = "/api/v1"

// Pinger 是健康检查依赖的最小接口，MySQL 与 Redis 客户端都满足。
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps 是处理器需要的依赖。后续卡片在这里加业务服务。
type Deps struct {
	Logger   *slog.Logger
	Version  string
	AppName  string
	MySQL    Pinger
	Redis    Pinger
	Auth     *auth.Service
	Flags    *flags.Service
	Profile  *profile.Service
	Material *material.Service
	Quota    *quota.Service
	Importer *importer.Service
	Bank     *bank.Service
	Plan     *plan.Service
	Practice *practice.Service
	// DevOSS 不为空时注册本地 mock OSS 的直传入口 PUT /dev/oss/*key（只在非生产环境）。
	DevOSS *oss.Mock
	// Tokens 校验访问令牌；为空时用 Auth（测试里可以换成假的）。
	Tokens TokenParser
}

// Handlers 实现 gen.ServerInterface。
type Handlers struct {
	deps Deps
}

var _ gen.ServerInterface = (*Handlers)(nil)

// NewRouter 组装中间件与全部路由。
func NewRouter(deps Deps) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.ContextWithFallback = true
	// 只有一层 Nginx 反向代理；客户端 IP 取 Nginx 设置的 X-Real-IP，不信任任意 X-Forwarded-For。
	r.TrustedPlatform = "X-Real-IP"
	_ = r.SetTrustedProxies(nil)

	r.Use(RequestID(deps.Logger), AccessLog(), Recovery(), Errors())

	r.NoRoute(func(c *gin.Context) { _ = c.Error(ErrNotFound()) })
	r.NoMethod(func(c *gin.Context) { _ = c.Error(ErrNotFound()) })
	r.HandleMethodNotAllowed = false

	if deps.DevOSS != nil {
		r.PUT("/dev/oss/*key", devOSSUpload(deps.DevOSS))
	}

	api := r.Group(APIPrefix)
	tokens := deps.Tokens
	if tokens == nil {
		tokens = deps.Auth
	}
	validator, err := RequestValidator()
	if err != nil {
		return nil, err
	}
	api.Use(Identify(tokens), validator)
	if deps.Flags != nil {
		api.Use(FeatureGate(deps.Flags, flagGuards))
	}
	gen.RegisterHandlersWithOptions(api, &Handlers{deps: deps}, gen.GinServerOptions{
		ErrorHandler: func(c *gin.Context, err error, status int) {
			// 生成代码在参数解析失败时调用这里，统一成 BAD_REQUEST。
			_ = c.Error(ErrBadRequest("请求参数不正确").Wrap(err).WithDetail(map[string]any{"reason": err.Error()}))
		},
	})
	return r, nil
}

// NewServer 包一层 net/http.Server，供 app 包做优雅停机。
func NewServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
}
