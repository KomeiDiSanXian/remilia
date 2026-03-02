package errutil

import (
	"errors"
	"fmt"
)

// BlockError indicates handler processing was blocked by middleware.
// This is used as a control-flow error inside the framework.
//
// Middleware can return a BlockError to signal that processing should stop
// without triggering retry logic. Use IsBlockError to check for this type.
type BlockError struct {
	Message string
}

func (be BlockError) Error() string { return be.Message }

// NewBlockError creates a new BlockError with the given message.
func NewBlockError(message string) error { return BlockError{Message: message} }

// IsBlockError checks if an error is a BlockError.
func IsBlockError(err error) bool {
	var be BlockError
	return errors.As(err, &be)
}

// Predefined framework/public errors.
// These errors are stable and can be checked with errors.Is.
var (
	// Core engine errors
	ErrConfigInvalid     = errors.New("invalid configuration")
	ErrMatcherNotFound   = errors.New("matcher not found")
	ErrContextReleased   = errors.New("context already released")
	ErrEngineShutdown    = errors.New("engine is shutting down")
	ErrInvalidEventID    = errors.New("invalid event ID")
	ErrHandlerNotSet     = errors.New("handler not set")
	ErrRuleCompileFailed = errors.New("rule compile failed")
	ErrDedupCacheFull    = errors.New("dedup cache full")
	ErrDeadLetterFailed  = errors.New("dead letter processing failed")

	// Adapter errors
	ErrAdapterStartFailed  = errors.New("adapter start failed")
	ErrAdapterStopFailed   = errors.New("adapter stop failed")
	ErrAdapterNotRunning   = errors.New("adapter not running")
	ErrWebhookCreateFailed = errors.New("failed to create webhook connection")

	// Plugin errors
	ErrPluginAlreadyExists = errors.New("plugin already exists")
	ErrPluginNotFound      = errors.New("plugin not found")
	ErrCircularDependency  = errors.New("circular dependency detected")
	ErrDependencyNotFound  = errors.New("dependency not found")
	ErrPluginLoadFailed    = errors.New("plugin load failed")
	ErrPluginUnloadFailed  = errors.New("plugin unload failed")

	// Bot errors
	ErrBotAlreadyRunning  = errors.New("bot already running")
	ErrBotNotRunning      = errors.New("bot not running")
	ErrBotShutdownTimeout = errors.New("bot shutdown timeout")

	// Lifecycle errors
	ErrComponentStartFailed   = errors.New("component start failed")
	ErrComponentStopFailed    = errors.New("component stop failed")
	ErrComponentNotRegistered = errors.New("component not registered")

	// Config errors
	ErrConfigFieldRequired = errors.New("config field is required")
	ErrConfigFieldInvalid  = errors.New("config field value is invalid")
	ErrConfigLoadFailed    = errors.New("failed to load config")
	ErrConfigParseFailed   = errors.New("failed to parse config")
	ErrConfigWatchFailed   = errors.New("failed to watch config")

	// Webhook protocol errors
	ErrWebhookSignFailed       = errors.New("failed to sign webhook request")
	ErrWebhookMarshalFailed    = errors.New("failed to marshal webhook data")
	ErrWebhookUnmarshalFailed  = errors.New("failed to unmarshal webhook data")
	ErrWebhookValidationFailed = errors.New("webhook validation failed")

	// Middleware errors
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
	ErrRetryExhausted     = errors.New("retry attempts exhausted")
	ErrDegradationActive  = errors.New("system is in degraded mode")

	// Token errors
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenInvalid       = errors.New("token invalid")
	ErrTokenRefreshFailed = errors.New("token refresh failed")
)

// IsErrorType checks if an error matches a target error using errors.Is.
func IsErrorType(err, target error) bool {
	return errors.Is(err, target)
}

// RecoverError converts a panic to an error.
// This function is typically used in defer statements to recover from panics
// and convert them into proper error values.
//
// Example:
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
			return WrapErrorf(v, "panic recovered")
		case string:
			return fmt.Errorf("panic recovered: %s", v)
		default:
			return fmt.Errorf("panic recovered: %v", r)
		}
	}
	return nil
}

// NewValidationError creates a validation error for a specific field.
func NewValidationError(field, reason string) error {
	return fmt.Errorf("validation failed for field '%s': %s", field, reason)
}

// NewConfigError creates a configuration error for a specific key.
func NewConfigError(key, reason string) error {
	return WrapErrorf(ErrConfigInvalid, fmt.Sprintf("config key '%s': %s", key, reason))
}

// NewPluginError creates a plugin-specific error.
func NewPluginError(pluginName, message string) error {
	return fmt.Errorf("plugin '%s': %s", pluginName, message)
}
