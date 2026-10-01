package http

import (
	"errors"
	"fmt"
	"net/http"
)

// 错误码集中定义在这里（CLAUDE.md「约定」）。前端按 code 判断，message 直接展示给用户。
// 新增错误码时同步写进 api/openapi.yaml 的说明。
const (
	CodeBadRequest         = "BAD_REQUEST"         // 参数不合法
	CodeUnauthorized       = "UNAUTHORIZED"        // 未登录或令牌失效
	CodeForbidden          = "FORBIDDEN"           // 已登录但无权操作
	CodeNotFound           = "NOT_FOUND"           // 不存在、不属于当前用户，或功能开关关闭
	CodeConflict           = "CONFLICT"            // 状态冲突（如同时只能有一套进行中的试卷）
	CodeTooManyRequests    = "TOO_MANY_REQUESTS"   // 频率限制
	CodeQuotaExceeded      = "QUOTA_EXCEEDED"      // 额度不足
	CodeInternal           = "INTERNAL"            // 服务端错误
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE" // 依赖不可用
	CodeNotImplemented     = "NOT_IMPLEMENTED"     // 契约已定义、实现未完成
)

// Error 是对外的统一错误：{ code, message, detail }。
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
	// cause 是内部原因，只写日志，不返回给客户端。
	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return e.Code + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.cause }

// WithDetail 返回附带补充信息的副本。
func (e *Error) WithDetail(detail map[string]any) *Error {
	cp := *e
	cp.Detail = detail
	return &cp
}

// Wrap 返回带内部原因的副本，原因只进日志。
func (e *Error) Wrap(cause error) *Error {
	cp := *e
	cp.cause = cause
	return &cp
}

func newError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// 常用错误。message 是面向用户的简体中文。
func ErrBadRequest(message string) *Error {
	return newError(http.StatusBadRequest, CodeBadRequest, message)
}

func ErrUnauthorized() *Error {
	return newError(http.StatusUnauthorized, CodeUnauthorized, "登录已失效，请重新登录")
}

func ErrForbidden() *Error {
	return newError(http.StatusForbidden, CodeForbidden, "没有权限进行这个操作")
}

// ErrNotFound 也用于「不属于当前用户」和「功能开关关闭」：不区分这几种情况，避免泄露存在性。
func ErrNotFound() *Error {
	return newError(http.StatusNotFound, CodeNotFound, "内容不存在")
}

func ErrConflict(message string) *Error {
	return newError(http.StatusConflict, CodeConflict, message)
}

func ErrTooManyRequests(message string) *Error {
	return newError(http.StatusTooManyRequests, CodeTooManyRequests, message)
}

func ErrQuotaExceeded(message string) *Error {
	return newError(http.StatusPaymentRequired, CodeQuotaExceeded, message)
}

func ErrInternal() *Error {
	return newError(http.StatusInternalServerError, CodeInternal, "服务暂时出了点问题，请稍后再试")
}

func ErrNotImplemented() *Error {
	return newError(http.StatusNotImplemented, CodeNotImplemented, "功能开发中")
}

// AsError 把任意错误转成对外错误；未知错误一律视为服务端错误，不把内部信息返回给客户端。
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return ErrInternal().Wrap(err)
}
