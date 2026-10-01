package http

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/logx"
)

// HeaderRequestID 是请求 ID 的请求头与响应头。
const HeaderRequestID = "X-Request-ID"

// 客户端传来的请求 ID 只接受这种格式，防止日志注入。
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestID 给每个请求一个 ID（沿用客户端传来的合法值），写入响应头，并把带 ID 的日志器放进 context。
func RequestID(base *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		c.Header(HeaderRequestID, id)
		c.Set(HeaderRequestID, id)
		l := base.With("request_id", id)
		c.Request = c.Request.WithContext(logx.WithLogger(c.Request.Context(), l))
		c.Next()
	}
}

func newRequestID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// AccessLog 记录每个请求的方法、路由模板、状态码和耗时。
// 只记路由模板（如 /api/v1/banks/:id），不记查询参数和请求体，避免把手机号、作答原文写进日志。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "(unmatched)"
		}
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logx.From(c.Request.Context()).Log(c.Request.Context(), level, "http request",
			"method", c.Request.Method,
			"route", route,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

// Recovery 把 panic 转成 500 统一错误，并记录日志。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logx.From(c.Request.Context()).Error("panic recovered", "panic", r)
				writeError(c, ErrInternal())
				c.Abort()
			}
		}()
		c.Next()
	}
}

// Errors 把处理器通过 c.Error 记下的错误写成统一的 { code, message, detail } 响应。
// 处理器只需 `c.Error(err); return`，不用自己拼错误响应。
func Errors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		e := AsError(c.Errors.Last().Err)
		if e.Status >= http.StatusInternalServerError {
			logx.From(c.Request.Context()).Error("request failed", "err", e)
		}
		writeError(c, e)
	}
}

func writeError(c *gin.Context, e *Error) {
	c.JSON(e.Status, e)
}
