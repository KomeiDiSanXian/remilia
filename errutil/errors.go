package errutil

import (
	"errors"
	"fmt"
)

// BlockError 表示处理器被中间件阻断。
// 在框架内部作为控制流错误使用。
//
// 中间件可返回 BlockError 以表示处理应停止，但不触发重试逻辑。
// 使用 IsBlockError 检查此类型。
type BlockError struct {
	Message string
}

func (be BlockError) Error() string { return be.Message }

// NewBlockError 创建带有指定消息的 BlockError。
func NewBlockError(message string) error { return BlockError{Message: message} }

// IsBlockError 检查 error 是否为 BlockError。
func IsBlockError(err error) bool {
	var be BlockError
	return errors.As(err, &be)
}

// 预定义的框架/公共错误。
// 这些错误是稳定的，可使用 errors.Is 进行检查。
//
// # 全项目错误使用规范
//
// 为保证调用方可用 errors.Is/errors.As 精确匹配错误，全项目遵循以下规则：
//
//  1. 公共哨兵错误在此包用 errors.New 定义，不在业务逻辑中重新构造字符串。
//
//  2. 需要添加上下文信息时，用 fmt.Errorf 包裹哨兵错误（保留 %w 链）：
//
//     return fmt.Errorf("操作失败: %w", errutil.ErrCircuitBreakerOpen)
//
//  3. 禁止直接用固定字符串 fmt.Errorf 替代哨兵错误（无法被 errors.Is 识别）：
//
//     // ❌ 错误做法
//     return fmt.Errorf("circuit breaker is open")
//     // ✅ 正确做法
//     return fmt.Errorf("circuit breaker is open: %w", errutil.ErrCircuitBreakerOpen)
//
//  4. 框架内部控制流错误（如 BlockError）使用具体类型，通过 errors.As 检查。
//
//  5. 包私有错误可以在包内用 errors.New 定义，不强制导出到此包。
var (
	ErrConfigInvalid  = errors.New("invalid configuration")
	ErrDedupCacheFull = errors.New("dedup cache full")

	ErrAdapterStartFailed  = errors.New("adapter start failed")
	ErrWebhookCreateFailed = errors.New("failed to create webhook connection")
	ErrNoChatInfo          = errors.New("no ChatInfo provided: ensure SendRequest.Target.ID is set before calling Send")
	ErrEmptyMessage        = errors.New("empty message: at least one of Text, Markdown, Attachments, Embeds, Buttons or Mentions must be set")
	ErrInvalidMessage      = errors.New("invalid message content")

	ErrPluginAlreadyExists = errors.New("plugin already exists")
	ErrPluginNotFound      = errors.New("plugin not found")
	ErrCircularDependency  = errors.New("circular dependency detected")
	ErrDependencyNotFound  = errors.New("dependency not found")
	ErrPluginLoadFailed    = errors.New("plugin load failed")

	ErrAdapterRequired = errors.New("adapter is required")

	ErrConfigFieldInvalid = errors.New("config field value is invalid")

	ErrRateLimitExceeded        = errors.New("rate limit exceeded")
	ErrCircuitBreakerOpen       = errors.New("circuit breaker is open")
	ErrCircuitBreakerHalfOpen   = errors.New("circuit breaker is half-open")
	ErrCircuitBreakerContention = errors.New("circuit breaker state transition contention")
)

// RecoverError 将 panic 转换为 error。
// 通常在 defer 语句中使用，用于捕获 panic 并将其转换为合适的错误值。
//
// 示例：
//
//	defer func() {
//	    if err := RecoverError(); err != nil {
//	        log.Printf("Recovered from panic: %v", err)
//	    }
//	}()
func RecoverError() error {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case error:
			return Wrap(v, "panic recovered")
		case string:
			return fmt.Errorf("panic recovered: %s", v)
		default:
			return fmt.Errorf("panic recovered: %v", r)
		}
	}
	return nil
}

// ValidationError 结构化验证错误，支持调用方通过 errors.As 提取字段名和原因。
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for field '%s': %s", e.Field, e.Reason)
}

func (e *ValidationError) Unwrap() error {
	return ErrConfigFieldInvalid
}

// NewValidationError 创建特定字段的验证错误。
// 返回 *ValidationError 类型，可通过 errors.As 提取结构化信息。
func NewValidationError(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}

// ConfigError 结构化配置错误。
type ConfigError struct {
	Key    string
	Reason string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config key '%s': %s", e.Key, e.Reason)
}

func (e *ConfigError) Unwrap() error {
	return ErrConfigInvalid
}

// NewConfigError 创建特定键的配置错误。
// 返回 *ConfigError 类型，可通过 errors.As 提取结构化信息。
func NewConfigError(key, reason string) error {
	return &ConfigError{Key: key, Reason: reason}
}
