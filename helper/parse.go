package helper

import "github.com/KomeiDiSanXian/remilia/openapi/dto"

// MustParseEvent parses an event or panics if parsing fails.
//
// This is useful in test setup or other scenarios where parsing failure
// should immediately abort execution.
//
// Example:
//
//	event := helper.MustParseEvent[dto.C2CMessageCreateEvent](payload)
//	// No need to check error - will panic if parsing fails
func MustParseEvent[T any](p *dto.Payload) *T {
	event, err := ParseEvent[T](p)
	if err != nil {
		panic(err)
	}
	return event
}

// ParseEventWithDefault parses an event and returns a default value if parsing fails.
//
// This is useful when you want to provide a fallback value instead of handling errors.
//
// Example:
//
//	event := helper.ParseEventWithDefault(payload, dto.C2CMessageCreateEvent{
//	    Content: "default message",
//	})
func ParseEventWithDefault[T any](p *dto.Payload, defaultValue T) T {
	event, err := ParseEvent[T](p)
	if err != nil {
		return defaultValue
	}
	return *event
}

// TryParseEvent attempts to parse an event and returns (value, success bool).
//
// This is useful when you want to handle parsing failure without error values.
//
// Example:
//
//	if event, ok := helper.TryParseEvent[dto.C2CMessageCreateEvent](payload); ok {
//	    // Use event
//	} else {
//	    // Handle parsing failure
//	}
func TryParseEvent[T any](p *dto.Payload) (T, bool) {
	event, err := ParseEvent[T](p)
	if err != nil {
		var zero T
		return zero, false
	}
	return *event, true
}

// ParseEventSlice parses multiple payloads into a slice of events.
//
// Returns an error if any payload fails to parse. The error indicates
// which payload failed.
//
// Example:
//
//	events, err := helper.ParseEventSlice[dto.C2CMessageCreateEvent](payloads)
//	if err != nil {
//	    log.Error("Failed to parse events:", err)
//	}
func ParseEventSlice[T any](payloads []*dto.Payload) ([]*T, error) {
	if len(payloads) == 0 {
		return []*T{}, nil
	}

	results := make([]*T, len(payloads))
	for i, p := range payloads {
		event, err := ParseEvent[T](p)
		if err != nil {
			return nil, err
		}
		results[i] = event
	}
	return results, nil
}

// ParseEventSlicePartial parses multiple payloads and returns successfully parsed events.
//
// Unlike ParseEventSlice, this function doesn't fail on parse errors.
// Instead, it skips failed payloads and returns all successful parses.
//
// Example:
//
//	events := helper.ParseEventSlicePartial[dto.C2CMessageCreateEvent](payloads)
//	// Returns only successfully parsed events
func ParseEventSlicePartial[T any](payloads []*dto.Payload) []*T {
	if len(payloads) == 0 {
		return []*T{}
	}

	results := make([]*T, 0, len(payloads))
	for _, p := range payloads {
		if event, err := ParseEvent[T](p); err == nil {
			results = append(results, event)
		}
	}
	return results
}

// FilterParseEvents filters and parses events in a single pass.
//
// Only payloads that match the predicate are parsed.
//
// Example:
//
//	// Only parse C2C events with specific IDs
//	events := helper.FilterParseEvents(payloads, func(p *dto.Payload) bool {
//	    return p.Type == dto.C2CMessageCreate && p.ID != ""
//	})
func FilterParseEvents[T any](payloads []*dto.Payload, predicate func(*dto.Payload) bool) []*T {
	results := make([]*T, 0)
	for _, p := range payloads {
		if predicate(p) {
			if event, err := ParseEvent[T](p); err == nil {
				results = append(results, event)
			}
		}
	}
	return results
}

// MapParseEvents parses events and transforms them using a mapper function.
//
// Example:
//
//	// Parse and extract only content
//	contents := helper.MapParseEvents(payloads,
//	    func(event *dto.C2CMessageCreateEvent) string {
//	        return event.Content
//	    })
func MapParseEvents[T any, R any](payloads []*dto.Payload, mapper func(*T) R) ([]R, error) {
	events, err := ParseEventSlice[T](payloads)
	if err != nil {
		return nil, err
	}

	results := make([]R, len(events))
	for i, event := range events {
		results[i] = mapper(event)
	}
	return results, nil
}
