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
	ErrConfigInvalid     = errors.New("invalid configuration")
	ErrMatcherNotFound   = errors.New("matcher not found")
	ErrContextReleased   = errors.New("context already released")
	ErrEngineShutdown    = errors.New("engine is shutting down")
	ErrInvalidEventID    = errors.New("invalid event ID")
	ErrHandlerNotSet     = errors.New("handler not set")
	ErrRuleCompileFailed = errors.New("rule compile failed")
	ErrDedupCacheFull    = errors.New("dedup cache full")
	ErrDeadLetterFailed  = errors.New("dead letter processing failed")

	ErrAdapterStartFailed  = errors.New("adapter start failed")
	ErrAdapterStopFailed   = errors.New("adapter stop failed")
	ErrAdapterNotRunning   = errors.New("adapter not running")
	ErrWebhookCreateFailed = errors.New("failed to create webhook connection")
	ErrNoChatInfo          = errors.New("no ChatInfo provided: ensure SendRequest.Target.ID is set before calling Send")
	ErrEmptyMessage        = errors.New("empty message: at least one of Text, Markdown, Attachments, Embeds, Buttons or Mentions must be set")
	ErrInvalidMessage      = errors.New("invalid message content")

	ErrPluginAlreadyExists = errors.New("plugin already exists")
	ErrPluginNotFound      = errors.New("plugin not found")
	ErrCircularDependency  = errors.New("circular dependency detected")
	ErrDependencyNotFound  = errors.New("dependency not found")
	ErrPluginLoadFailed    = errors.New("plugin load failed")
	ErrPluginUnloadFailed  = errors.New("plugin unload failed")

	ErrAdapterRequired = errors.New("adapter is required")
	ErrEngineRequired  = errors.New("engine is required")
	ErrBotInfoRequired = errors.New("bot info is required")

	ErrBotAlreadyRunning  = errors.New("bot already running")
	ErrBotNotRunning      = errors.New("bot not running")
	ErrBotShutdownTimeout = errors.New("bot shutdown timeout")

	ErrComponentStartFailed   = errors.New("lifecycle: component start failed")
	ErrComponentStopFailed    = errors.New("lifecycle: component stop failed")
	ErrComponentNotRegistered = errors.New("lifecycle: component not registered")
	ErrComponentStartTimeout  = errors.New("lifecycle: component start timeout")
	ErrComponentStopTimeout   = errors.New("lifecycle: component stop timeout")

	ErrConfigFieldRequired = errors.New("config field is required")
	ErrConfigFieldInvalid  = errors.New("config field value is invalid")
	ErrConfigLoadFailed    = errors.New("failed to load config")
	ErrConfigParseFailed   = errors.New("failed to parse config")
	ErrConfigWatchFailed   = errors.New("failed to watch config")

	ErrWebhookSignFailed       = errors.New("failed to sign webhook request")
	ErrWebhookMarshalFailed    = errors.New("failed to marshal webhook data")
	ErrWebhookUnmarshalFailed  = errors.New("failed to unmarshal webhook data")
	ErrWebhookValidationFailed = errors.New("webhook validation failed")

	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrRetryExhausted     = errors.New("retry attempts exhausted")
	ErrDegradationActive  = errors.New("system is in degraded mode")

	ErrTokenExpired       = errors.New("token expired")
	ErrTokenInvalid       = errors.New("token invalid")
	ErrTokenRefreshFailed = errors.New("token refresh failed")

	ErrPassiveReplyExpired      = errors.New("passive reply window expired")
	ErrPassiveReplyLimitReached = errors.New("passive reply limit reached")
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

// PluginError 结构化插件错误。
//
// Deprecated: 请使用 plugin.PluginError（定义于 plugin/errors.go），
// 它提供更丰富的诊断上下文（Operation、Hint、RegisteredPlugins）。
// errutil.PluginError 仅保留以兼容旧代码，新代码不应使用。
type PluginError struct {
	PluginName string
	Message    string
}

func (e *PluginError) Error() string {
	return fmt.Sprintf("plugin '%s': %s", e.PluginName, e.Message)
}

func (e *PluginError) Unwrap() error {
	return ErrPluginLoadFailed
}

// NewPluginError 创建插件专属错误。
//
// Deprecated: 使用 plugin.PluginError（import "github.com/KomeiDiSanXian/remilia/plugin"）。
// 返回 *PluginError 类型，可通过 errors.As 提取结构化信息。
func NewPluginError(pluginName, message string) error {
	return &PluginError{PluginName: pluginName, Message: message}
}
