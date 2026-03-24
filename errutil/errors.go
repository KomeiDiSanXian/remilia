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

	ErrComponentStartFailed   = errors.New("component start failed")
	ErrComponentStopFailed    = errors.New("component stop failed")
	ErrComponentNotRegistered = errors.New("component not registered")

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
)

// IsErrorType 使用 errors.Is 检查 error 是否与目标错误匹配。
func IsErrorType(err, target error) bool {
	return errors.Is(err, target)
}

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

// NewValidationError 创建特定字段的验证错误。
func NewValidationError(field, reason string) error {
	return fmt.Errorf("validation failed for field '%s': %s", field, reason)
}

// NewConfigError 创建特定键的配置错误。
func NewConfigError(key, reason string) error {
	return fmt.Errorf("config key '%s': %s: %w", key, reason, ErrConfigInvalid)
}

// NewPluginError 创建插件专属错误。
func NewPluginError(pluginName, message string) error {
	return fmt.Errorf("plugin '%s': %s", pluginName, message)
}
