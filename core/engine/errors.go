package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/KomeiDiSanXian/remilia/core/context"
	remiliaerrors "github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
)

// HandlerError is a framework error envelope for handler execution.
//
// Fields:
//   - Message: original error message
//   - Source: matcher.Source (e.g. "global" or "plugin:<name>")
//   - Attempt: retry attempt number (best-effort)
//   - Trace: executed middleware trace (if enabled)
//   - EventID: event identifier (if available)
//   - Stack: stack trace (optional; controlled by stack tracing settings)
//
// Contract:
//   - HandlerError is meant for framework use (logging/DLQ) and is safe to serialize.
//   - Users should treat it as opaque and use errors.As to extract it when needed.
//
// NOTE: This was split out of errors.go to keep framework execution errors
// separate from generic utility errors.
type HandlerError struct {
	Message string   `json:"message"`
	Source  string   `json:"source"`
	Attempt int      `json:"attempt"`
	Trace   []string `json:"trace,omitempty"`
	EventID string   `json:"event_id,omitempty"`
	Stack   string   `json:"stack,omitempty"`
}

func (he HandlerError) Error() string { return he.Message }

// WrapError builds a HandlerError used by the framework.
func WrapError(err error, ctx *context.Context, m *Matcher, attempt int) error {
	if err == nil {
		return nil
	}

	var trace []string
	if ctx != nil {
		if arr, ok := ctx.GetMiddlewareTrace(); ok {
			trace = arr
		}
	}

	var eventID string
	if ctx != nil {
		event := ctx.GetEvent()
		if event != nil {
			eventID = string(event.ID)
		}
	}

	herr := HandlerError{
		Message: err.Error(),
		Attempt: attempt,
		Trace:   trace,
		EventID: eventID,
	}
	if m != nil {
		herr.Source = m.Source
	}

	if remiliaerrors.ShouldCaptureStack() {
		herr.Stack = remiliaerrors.CaptureStack()
	}

	return herr
}

// FormatHandlerError formats a HandlerError for logs.
func FormatHandlerError(err error) string {
	var he HandlerError
	if !errors.As(err, &he) {
		return err.Error()
	}

	parts := make([]string, 0, 6)
	parts = append(parts, fmt.Sprintf("Message: %s", he.Message))
	parts = append(parts, fmt.Sprintf("Source: %s", he.Source))
	parts = append(parts, fmt.Sprintf("Attempt: %d", he.Attempt))

	if he.EventID != "" {
		parts = append(parts, fmt.Sprintf("EventID: %s", he.EventID))
	}
	if len(he.Trace) > 0 {
		parts = append(parts, fmt.Sprintf("Trace: %v", he.Trace))
	}
	if he.Stack != "" {
		parts = append(parts, "Stack:\n"+he.Stack)
	}

	return stringsJoin(parts, "\n")
}

// MarshalDeadLetterItem serializes a PayloadItem with a standardized HandlerError.
func MarshalDeadLetterItem(item dlq.PayloadItem) ([]byte, error) {
	var herr HandlerError
	var he HandlerError
	if errors.As(item.Err, &he) {
		herr = he
	}

	return json.Marshal(struct {
		Event *DeadLetterEvent `json:"event"`
		Error HandlerError     `json:"error"`
	}{
		Event: &DeadLetterEvent{ID: string(item.Data.ID), Type: string(item.Data.Type)},
		Error: herr,
	})
}

// DeadLetterEvent is a lightweight event representation for persistence.
type DeadLetterEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// stringsJoin is a tiny helper to avoid importing strings in this file'services public surface.
func stringsJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(parts[0])
	for i := 1; i < len(parts); i++ {
		out.WriteString(sep)
		out.WriteString(parts[i])
	}
	return out.String()
}
