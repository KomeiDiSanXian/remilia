package errutil

import "fmt"

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
func WrapErrorf(err error, message string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{Err: err, Message: message}
}

// WrapErrorWithContextf wraps an error with both a message and context string.
// Returns nil if the input error is nil.
func WrapErrorWithContextf(err error, message, context string) error {
	if err == nil {
		return nil
	}
	return &ErrorWrapper{Err: err, Message: message, Context: context}
}
