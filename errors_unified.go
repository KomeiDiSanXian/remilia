package remilia

import (
	"errors"
	"fmt"
)

// 预定义错误类型
//
// 这些错误可以使用 errors.Is() 进行判断，便于错误处理和测试。
var (
	// ErrConfigInvalid 表示配置无效
	ErrConfigInvalid = errors.New("invalid configuration")

	// ErrMatcherNotFound 表示 Matcher 未找到
	ErrMatcherNotFound = errors.New("matcher not found")

	// ErrContextReleased 表示 Context 已被释放
	ErrContextReleased = errors.New("context already released")

	// ErrEngineShutdown 表示 Engine 正在关闭
	ErrEngineShutdown = errors.New("engine is shutting down")

	// ErrInvalidEventID 表示事件 ID 无效
	ErrInvalidEventID = errors.New("invalid event ID")

	// ErrHandlerNotSet 表示处理器未设置
	ErrHandlerNotSet = errors.New("handler not set")

	// ErrRuleCompileFailed 表示规则编译失败
	ErrRuleCompileFailed = errors.New("rule compile failed")

	// ErrDedupCacheFull 表示去重缓存已满
	ErrDedupCacheFull = errors.New("dedup cache full")

	// ErrDeadLetterFailed 表示死信处理失败
	ErrDeadLetterFailed = errors.New("dead letter processing failed")
)

// ErrorWrapper 包装错误，添加上下文信息
type ErrorWrapper struct {
	Err     error  // 原始错误
	Message string // 错误消息
	Context string // 错误上下文
}

// Error 实现 error 接口
func (e *ErrorWrapper) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s [context: %s]: %v", e.Message, e.Context, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Err)
}

// Unwrap 实现 errors.Unwrap 接口
func (e *ErrorWrapper) Unwrap() error {
	return e.Err
}

// WrapErrorf 包装错误，添加格式化消息
//
// 使用示例：
//
//	err := doSomething()
//	if err != nil {
//	    return WrapErrorf(err, "failed to do something")
//	}
func WrapErrorf(err error, message string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{
		Err:     err,
		Message: message,
	}
}

// WrapErrorWithContextf 包装错误，添加消息和上下文
//
// 使用示例：
//
//	err := doSomething()
//	if err != nil {
//	    return WrapErrorWithContextf(err, "failed to do something", "event_id="+eventID)
//	}
func WrapErrorWithContextf(err error, message, context string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{
		Err:     err,
		Message: message,
		Context: context,
	}
}

// IsErrorType 检查错误是否为指定类型
//
// 使用示例：
//
//	if IsErrorType(err, ErrContextReleased) {
//	    // 处理 Context 已释放的情况
//	}
func IsErrorType(err, target error) bool {
	return errors.Is(err, target)
}

// NewValidationError 创建验证错误
func NewValidationError(field, reason string) error {
	return fmt.Errorf("validation failed for field '%s': %s", field, reason)
}

// NewConfigError 创建配置错误
func NewConfigError(key, reason string) error {
	return WrapErrorf(ErrConfigInvalid, fmt.Sprintf("config key '%s': %s", key, reason))
}

// NewPluginError 创建插件错误
func NewPluginError(pluginName, message string) error {
	return fmt.Errorf("plugin '%s': %s", pluginName, message)
}

// RecoverError 从 panic 中恢复并转换为错误
//
// 使用示例：
//
//	defer func() {
//	    if err := RecoverError(); err != nil {
//	        log.Error("panic recovered:", err)
//	    }
//	}()
func RecoverError() error {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			return WrapErrorf(v, "panic recovered")
		case string:
			return fmt.Errorf("panic recovered: %s", v)
		default:
			return fmt.Errorf("panic recovered: %v", r)
		}
	}
	return nil
}
