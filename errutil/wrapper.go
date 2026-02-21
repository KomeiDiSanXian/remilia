package errutil

import (
	"errors"
	"fmt"
)

// ErrorWrapper wraps an error adding message and optional context.
type ErrorWrapper struct {
	Err     error
	Message string
	Context string
}

func (e *ErrorWrapper) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s [context: %s]: %v", e.Message, e.Context, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

func (e *ErrorWrapper) Unwrap() error { return e.Err }

// WrapErrorf wraps an error with a formatted message.
// Returns nil if the input error is nil.
//
// Deprecated: 请使用 Wrap 或 Wrapf 替代，语义更清晰。
func WrapErrorf(err error, message string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{Err: err, Message: message}
}

// WrapErrorWithContextf wraps an error with both a message and context string.
// Returns nil if the input error is nil.
//
// Deprecated: 请使用 WrapWithContext 替代。
func WrapErrorWithContextf(err error, message, context string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{Err: err, Message: message, Context: context}
}

// ─── 推荐 API ───────────────────────────────────────────────────────────────

// New 创建新错误，替代 errors.New。
// 推荐用于包级别的哨兵错误：var ErrFoo = errutil.New("foo failed")
func New(msg string) error {
	return errors.New(msg)
}

// Newf 创建带格式化消息的新错误。
// 注意：此函数不应用于哨兵错误（每次调用返回不同的实例）。
// 如需创建包装现有错误的新错误，请使用 Wrap/Wrapf。
func Newf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// Wrap 用给定消息包装 err（使用 %w，支持 errors.Is/As 链式解包）。
// 返回 nil 如果 err 为 nil。
//
// 用法：
//
//	return errutil.Wrap(err, "failed to load config")
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// Wrapf 用格式化消息包装 err（使用 %w）。
// 返回 nil 如果 err 为 nil。
//
// 用法：
//
//	return errutil.Wrapf(err, "plugin %s load failed", name)
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	// 在 format 末尾追加 ": %w" 来包装原始错误
	args = append(args, err)
	return fmt.Errorf(format+": %w", args...)
}

// WrapWithContext 用消息和上下文字符串包装 err（使用 ErrorWrapper，保留上下文字段）。
// 返回 nil 如果 err 为 nil。
//
// 用法：
//
//	return errutil.WrapWithContext(err, "query failed", "table=users, id=123")
func WrapWithContext(err error, message, ctx string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{Err: err, Message: message, Context: ctx}
}

// Is 是 errors.Is 的快捷方式。
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As 是 errors.As 的快捷方式。
func As[T any](err error, target *T) bool {
	return errors.As(err, target)
}

// Unwrap 是 errors.Unwrap 的快捷方式。
func Unwrap(err error) error {
	return errors.Unwrap(err)
}

// Join 是 errors.Join 的快捷方式（Go 1.20+）。
func Join(errs ...error) error {
	return errors.Join(errs...)
}
