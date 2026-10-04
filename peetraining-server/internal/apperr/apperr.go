// Package apperr 是业务层返回给用户的错误。业务包只用它表达「哪类错误 + 中文说明 + 补充信息」，
// 由 internal/http 统一转成 { code, message, detail } 与 HTTP 状态码，业务包不依赖 HTTP。
package apperr

import (
	"errors"
	"fmt"
)

// Kind 是错误类别，与 internal/http/errors.go 的错误码一一对应。
type Kind int

const (
	BadRequest Kind = iota + 1
	Unauthorized
	Forbidden
	NotFound
	Conflict
	TooManyRequests
	QuotaExceeded
	AIFailed
)

// Error 是面向用户的业务错误。
type Error struct {
	Kind    Kind
	Message string
	Detail  map[string]any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s：%v", e.Message, e.cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.cause }

// New 创建业务错误。
func New(kind Kind, message string) *Error { return &Error{Kind: kind, Message: message} }

// With 附带补充信息（如 remaining_attempts、retry_after_seconds、reason）。
func (e *Error) With(key string, value any) *Error {
	cp := *e
	cp.Detail = make(map[string]any, len(e.Detail)+1)
	for k, v := range e.Detail {
		cp.Detail[k] = v
	}
	cp.Detail[key] = value
	return &cp
}

// Wrap 附带内部原因（只进日志）。
func (e *Error) Wrap(cause error) *Error {
	cp := *e
	cp.cause = cause
	return &cp
}

// As 取出错误链里的业务错误。
func As(err error) (*Error, bool) {
	var e *Error
	ok := errors.As(err, &e)
	return e, ok
}

// IsKind 判断错误是否某类业务错误。
func IsKind(err error, kind Kind) bool {
	e, ok := As(err)
	return ok && e.Kind == kind
}

// 常用错误。

// NotFoundErr 用于不存在、不属于当前用户、功能开关关闭：不区分，避免泄露存在性（ADR 0006）。
func NotFoundErr() *Error { return New(NotFound, "内容不存在") }
