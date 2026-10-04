package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
)

// ctxAdmin 是后台会话识别出的账号（admin.Admin）。
const ctxAdmin = "admin.account"

// AdminAuthenticator 按后台会话令牌找账号（admin.Service 实现）。
type AdminAuthenticator interface {
	Authenticate(ctx context.Context, token string) (admin.Admin, error)
	Audit(ctx context.Context, adminID uint64, action, targetType, targetID string, detail map[string]any, ip string) error
}

// adminPrefix 是后台接口的路由前缀。
const adminPrefix = APIPrefix + "/admin/"

// IdentifyAdmin 解析后台接口请求里的会话令牌，有效就记下账号；是否必须登录由 RequestValidator 按契约（adminAuth）判断。
func IdentifyAdmin(a AdminAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, adminPrefix) {
			c.Next()
			return
		}
		if token, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer "); ok && token != "" {
			if acc, err := a.Authenticate(c.Request.Context(), token); err == nil {
				c.Set(ctxAdmin, acc)
			} else if !errors.Is(err, admin.ErrNoSession) {
				_ = c.Error(err)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func currentAdmin(c *gin.Context) admin.Admin {
	v, _ := c.Get(ctxAdmin)
	a, _ := v.(admin.Admin)
	return a
}

// AdminRoles 从契约读每个后台接口的 x-roles：「方法 路由模板」→ 角色。"*" 表示任何登录的后台账号。
func AdminRoles() (map[string][]admin.Role, error) {
	spec, err := gen.GetSwagger()
	if err != nil {
		return nil, err
	}
	out := map[string][]admin.Role{}
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/admin/") {
			continue
		}
		for method, op := range item.Operations() {
			raw, ok := op.Extensions["x-roles"]
			if !ok {
				continue
			}
			list, _ := raw.([]any)
			var roles []admin.Role
			for _, r := range list {
				roles = append(roles, admin.Role(fmt.Sprint(r)))
			}
			// gin 的路由模板用 :param。
			tpl := APIPrefix + strings.NewReplacer("{", ":", "}", "").Replace(path)
			out[method+" "+tpl] = roles
		}
	}
	return out, nil
}

// passwordFree 是必须改密码时仍能访问的接口。
var passwordFree = map[string]bool{
	"GET " + APIPrefix + "/admin/me": true, "POST " + APIPrefix + "/admin/me/password": true, "POST " + APIPrefix + "/admin/auth/logout": true,
}

// sensitiveReads 是要记审计日志的读接口：完整手机号与授权内容（授权内容另记 content_access_logs）。
var sensitiveReads = []string{"/phone", "/content", "/material"}

// AdminGate 按契约声明的角色放行后台接口（没声明的一律拒绝），并给所有写操作与敏感读记审计日志（PRD 10.1、dev-spec 第十节）。
func AdminGate(roles map[string][]admin.Role, audit AdminAuthenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.Request.Method + " " + c.FullPath()
		if !strings.HasPrefix(c.FullPath(), adminPrefix) || strings.HasPrefix(c.FullPath(), adminPrefix+"auth/login") || strings.HasPrefix(c.FullPath(), adminPrefix+"auth/verify") {
			c.Next()
			return
		}
		a := currentAdmin(c)
		need, declared := roles[key]
		switch {
		case a.ID == 0:
			_ = c.Error(ErrUnauthorized())
			c.Abort()
			return
		case !declared:
			_ = c.Error(ErrForbidden())
			c.Abort()
			return
		case a.MustChangePassword && !passwordFree[key]:
			_ = c.Error(ErrForbidden().WithDetail(map[string]any{"reason": "must_change_password"}))
			c.Abort()
			return
		case !slices.Contains(need, "*") && !a.Has(need...):
			_ = c.Error(ErrForbidden())
			c.Abort()
			return
		}
		c.Next()
		write := c.Request.Method != http.MethodGet
		sensitive := slices.ContainsFunc(sensitiveReads, func(s string) bool { return strings.HasSuffix(c.FullPath(), s) })
		if (write || sensitive) && c.Writer.Status() < 400 {
			targetType, targetID := "", ""
			if len(c.Params) > 0 {
				targetType = strings.TrimSuffix(c.Params[0].Key, "Id")
				targetType = strings.TrimSuffix(targetType, "No")
				targetID = c.Params[0].Value
			}
			action := c.Request.Method + " " + strings.TrimPrefix(c.FullPath(), APIPrefix)
			if err := audit.Audit(c.Request.Context(), a.ID, action, targetType, targetID, nil, c.ClientIP()); err != nil {
				_ = c.Error(err)
			}
		}
	}
}
