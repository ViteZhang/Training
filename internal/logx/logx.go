// Package logx 提供 JSON 结构化日志。
//
// 日志不记录手机号明文（CLAUDE.md 必须遵守第 11 条）：所有字符串字段里的
// 中国大陆手机号都会被脱敏为 138****5678。作答原文与资料原文由调用方保证不写入日志。
package logx

import (
	"context"
	"io"
	"log/slog"
	"regexp"
)

// phonePattern 匹配前后不紧挨数字的 11 位手机号。
var phonePattern = regexp.MustCompile(`(^|[^0-9])(1[3-9][0-9])([0-9]{4})([0-9]{4})($|[^0-9])`)

// MaskPhones 把字符串里的手机号替换为脱敏形式。
func MaskPhones(s string) string {
	// 连续的两个号码共用分隔符时，一次替换会漏掉第二个，所以替换到不再变化为止。
	for {
		next := phonePattern.ReplaceAllString(s, "${1}${2}****${4}${5}")
		if next == s {
			return s
		}
		s = next
	}
}

// New 创建写到 w 的 JSON 日志器。
func New(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redact,
	}))
}

func redact(_ []string, a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		if s := a.Value.String(); s != "" {
			a.Value = slog.StringValue(MaskPhones(s))
		}
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok && err != nil {
			a.Value = slog.StringValue(MaskPhones(err.Error()))
		}
	}
	return a
}

type ctxKey struct{}

// WithLogger 把日志器放进 context，供下游取用（例如带上请求 ID 的日志器）。
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// From 从 context 取日志器，没有时返回默认日志器。
func From(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
