package http

import (
	"context"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/flags"
)

// flagGuards 是受功能开关控制的接口：「方法 路由模板」→ 开关名。开关对当前用户关闭时返回 404（ADR 0009），
// 而不是报错，App 也不显示入口。支付（T25）、官方题库（T30）、口述背诵（T20）、语音作答（T18）、
// 扫描版 PDF（T09）、邀请（T26）的接口加入契约时在这里登记。
var flagGuards = map[string]string{}

// FlagChecker 判断开关对某用户是否打开（flags.Service 实现）。
type FlagChecker interface {
	Enabled(ctx context.Context, key string, userID uint64) (bool, error)
}

// FeatureGate 按 flagGuards 拦截关闭的功能。
func FeatureGate(fl FlagChecker, guards map[string]string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key, ok := guards[c.Request.Method+" "+c.FullPath()]
		if !ok {
			c.Next()
			return
		}
		on, err := fl.Enabled(c.Request.Context(), key, currentUser(c))
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if !on {
			_ = c.Error(ErrNotFound())
			c.Abort()
			return
		}
		c.Next()
	}
}

var _ FlagChecker = (*flags.Service)(nil)
