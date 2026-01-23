package errutil

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
