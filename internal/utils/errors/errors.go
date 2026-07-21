package errors

import (
	"fmt"
	"strings"
)

// ErrorCode 定义标准化的错误代码
type ErrorCode string

const (
	ErrCodeNotFound      ErrorCode = "NOT_FOUND"
	ErrCodeInvalidInput  ErrorCode = "INVALID_INPUT"
	ErrCodePermission    ErrorCode = "PERMISSION_DENIED"
	ErrCodeTimeout       ErrorCode = "TIMEOUT"
	ErrCodeInternal      ErrorCode = "INTERNAL_ERROR"
	ErrCodeNotImplemented ErrorCode = "NOT_IMPLEMENTED"
	ErrCodeUnavailable   ErrorCode = "SERVICE_UNAVAILABLE"
	ErrCodeConflict      ErrorCode = "CONFLICT"
)

// AppError 是应用程序的标准错误类型
type AppError struct {
	Code    ErrorCode
	Message string
	Cause   error
	Details map[string]any
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

// NewError 创建一个新的应用错误
func NewError(code ErrorCode, message string, opts ...ErrorOption) *AppError {
	err := &AppError{
		Code:    code,
		Message: message,
		Details: make(map[string]any),
	}
	for _, opt := range opts {
		opt(err)
	}
	return err
}

// ErrorOption 是错误选项函数
type ErrorOption func(*AppError)

// WithCause 设置错误的根本原因
func WithCause(cause error) ErrorOption {
	return func(e *AppError) {
		e.Cause = cause
	}
}

// WithDetail 添加错误详情
func WithDetail(key string, value any) ErrorOption {
	return func(e *AppError) {
		e.Details[key] = value
	}
}

// IsNotFound 检查是否为"未找到"错误
func IsNotFound(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeNotFound
	}
	return false
}

// IsPermissionDenied 检查是否为权限错误
func IsPermissionDenied(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodePermission
	}
	return false
}

// IsTimeout 检查是否为超时错误
func IsTimeout(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeTimeout
	}
	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "deadline exceeded")
}

// IsNotImplemented 检查是否为"未实现"错误
func IsNotImplemented(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeNotImplemented
	}
	return strings.Contains(err.Error(), "not implemented")
}

// NotFound 创建"未找到"错误
func NotFound(resource, id string) *AppError {
	return NewError(ErrCodeNotFound, fmt.Sprintf("%s not found: %s", resource, id),
		WithDetail("resource", resource),
		WithDetail("id", id),
	)
}

// InvalidInput 创建"无效输入"错误
func InvalidInput(message string, opts ...ErrorOption) *AppError {
	return NewError(ErrCodeInvalidInput, message, opts...)
}

// PermissionDenied 创建"权限拒绝"错误
func PermissionDenied(message string, opts ...ErrorOption) *AppError {
	return NewError(ErrCodePermission, message, opts...)
}

// Timeout 创建"超时"错误
func Timeout(operation string, opts ...ErrorOption) *AppError {
	return NewError(ErrCodeTimeout, fmt.Sprintf("operation timed out: %s", operation), opts...)
}

// Internal 创建"内部错误"错误
func Internal(message string, cause error) *AppError {
	return NewError(ErrCodeInternal, message, WithCause(cause))
}

// NotImplemented 创建"未实现"错误
func NotImplemented(feature string) *AppError {
	return NewError(ErrCodeNotImplemented, fmt.Sprintf("%s is not implemented", feature))
}

// Unavailable 创建"服务不可用"错误
func Unavailable(service string, opts ...ErrorOption) *AppError {
	return NewError(ErrCodeUnavailable, fmt.Sprintf("service unavailable: %s", service), opts...)
}

// Wrap 包装一个错误为应用错误
func Wrap(err error, code ErrorCode, message string) *AppError {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*AppError); ok {
		return appErr
	}
	return NewError(code, message, WithCause(err))
}

// WrapNotFound 包装为"未找到"错误
func WrapNotFound(err error, resource string) *AppError {
	if err == nil {
		return nil
	}
	return NewError(ErrCodeNotFound, fmt.Sprintf("%s not found", resource), WithCause(err))
}

// WrapInternal 包装为"内部错误"
func WrapInternal(err error, message string) *AppError {
	if err == nil {
		return nil
	}
	return Internal(message, err)
}