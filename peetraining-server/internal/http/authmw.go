package http

import (
	"context"
	"errors"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gin-gonic/gin"
	ginmiddleware "github.com/oapi-codegen/gin-middleware"

	"peetraining-server/internal/gen"
)

// 上下文键：鉴权通过后写入当前用户与设备。
const (
	ctxUserID   = "auth.user_id"
	ctxDeviceID = "auth.device_id"
)

// TokenParser 校验访问令牌，返回用户 ID 与设备 ID。
type TokenParser interface {
	ParseAccess(token string) (uint64, string, error)
}

var errNoToken = errors.New("缺少访问令牌")

// RequestValidator 按 api/openapi.yaml 校验每个请求：路径、参数、请求体（必填、格式、枚举、长度），以及鉴权。
//
// 鉴权跟随契约里每个接口的 security：
//   - 默认需要登录；
//   - 标了 security: [] 的接口不需要登录；
//   - 标了 [{}, bearerAuth] 的接口登录可选（如 /bootstrap），带了有效令牌就识别用户。
//
// 契约是唯一依据，新增接口不用改这里。
func RequestValidator() (gin.HandlerFunc, error) {
	spec, err := gen.GetSwagger()
	if err != nil {
		return nil, err
	}
	// 契约里的 servers 是 /api/v1，与路由前缀一致，按完整路径匹配。
	spec.Servers = openapi3.Servers{{URL: APIPrefix}}
	return ginmiddleware.OapiRequestValidatorWithOptions(spec, &ginmiddleware.Options{
		SilenceServersWarning: true,
		Options: openapi3filter.Options{
			MultiError: false,
			// 令牌已由 Identify 中间件解析：这里只判断有没有识别出用户。
			// 登录可选的接口（[{}, bearerAuth]）由空要求直接通过，不会走到这里。
			// 后台接口（adminAuth）只认后台会话，App 的用户令牌拿不到后台接口；反之亦然。
			AuthenticationFunc: func(ctx context.Context, in *openapi3filter.AuthenticationInput) error {
				c := ginmiddleware.GetGinContext(ctx)
				if c == nil {
					return errNoToken
				}
				key := ctxUserID
				if in.SecuritySchemeName == "adminAuth" {
					key = ctxAdmin
				}
				if _, ok := c.Get(key); !ok {
					return errNoToken
				}
				return nil
			},
		},
		ErrorHandler: func(c *gin.Context, message string, status int) {
			var e *Error
			// 鉴权失败时中间件给的状态码是 400，按错误信息识别出来改成 401。
			if strings.Contains(message, "security requirements failed") {
				status = 401
			}
			switch status {
			case 401, 403:
				e = ErrUnauthorized()
			case 404:
				e = ErrNotFound()
			case 405:
				e = ErrNotFound()
			default:
				e = ErrBadRequest("请求参数不正确").WithDetail(map[string]any{"reason": message})
			}
			c.AbortWithStatusJSON(e.Status, e)
		},
	}), nil
}

// Identify 解析请求里的访问令牌：有效就记下当前用户与设备，无效或没带则什么也不做，
// 是否必须登录由 RequestValidator 按契约判断。
func Identify(tokens TokenParser) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if ok && token != "" {
			if uid, did, err := tokens.ParseAccess(token); err == nil {
				c.Set(ctxUserID, uid)
				c.Set(ctxDeviceID, did)
			}
		}
		c.Next()
	}
}

// currentUser 返回当前登录用户；接口要求登录时一定有值（校验中间件已拦截）。
func currentUser(c *gin.Context) uint64 {
	v, _ := c.Get(ctxUserID)
	id, _ := v.(uint64)
	return id
}

func currentDevice(c *gin.Context) string {
	v, _ := c.Get(ctxDeviceID)
	s, _ := v.(string)
	return s
}
