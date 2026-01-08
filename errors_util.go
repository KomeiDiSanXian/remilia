package remilia

import (
	"errors"
	"fmt"
)

// Predefined framework/public errors.
// These errors are stable and can be checked with errors.Is.
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

func WrapErrorf(err error, message string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{Err: err, Message: message}
}

func WrapErrorWithContextf(err error, message, context string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{Err: err, Message: message, Context: context}
}

func IsErrorType(err, target error) bool { return errors.Is(err, target) }

func NewValidationError(field, reason string) error {
	return fmt.Errorf("validation failed for field '%s': %s", field, reason)
}

func NewConfigError(key, reason string) error {
	return WrapErrorf(ErrConfigInvalid, fmt.Sprintf("config key '%s': %s", key, reason))
}

func NewPluginError(pluginName, message string) error {
	return fmt.Errorf("plugin '%s': %s", pluginName, message)
}

// RecoverError converts a panic to an error.
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
