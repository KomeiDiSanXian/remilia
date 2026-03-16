package helper

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMustParseEvent tests MustParseEvent function
func TestMustParseEvent(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{"content":"test message","author":{"id":"123"}}`),
		}

		event := MustParseEvent[dto.C2CMessageCreateEvent](payload)
		require.NotNil(t, event)
		assert.Equal(t, "test message", event.Content)
		assert.Equal(t, "123", event.Author.ID)
	})

	t.Run("panic on invalid JSON", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`invalid json`),
		}

		assert.Panics(t, func() {
			MustParseEvent[dto.C2CMessageCreateEvent](payload)
		})
	})
}

// TestParseEventWithDefault tests ParseEventWithDefault function
func TestParseEventWithDefault(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{"content":"test message"}`),
		}

		event := ParseEventWithDefault(payload, dto.C2CMessageCreateEvent{
			MessageCreateEvent: dto.MessageCreateEvent{
				Content: "default",
			},
		})
		assert.Equal(t, "test message", event.Content)
	})

	t.Run("returns default on parse error", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`invalid json`),
		}

		defaultEvent := dto.C2CMessageCreateEvent{
			MessageCreateEvent: dto.MessageCreateEvent{
				Content: "default message",
				Author:  dto.Author{ID: "default-id"},
			},
		}

		event := ParseEventWithDefault(payload, defaultEvent)
		assert.Equal(t, "default message", event.Content)
		assert.Equal(t, "default-id", event.Author.ID)
	})
}

// TestTryParseEvent tests TryParseEvent function
func TestTryParseEvent(t *testing.T) {
	t.Run("success case", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{"content":"test message","author":{"id":"123"}}`),
		}

		event, ok := TryParseEvent[dto.C2CMessageCreateEvent](payload)
		assert.True(t, ok)
		assert.Equal(t, "test message", event.Content)
		assert.Equal(t, "123", event.Author.ID)
	})

	t.Run("failure case", func(t *testing.T) {
		payload := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`invalid json`),
		}

		event, ok := TryParseEvent[dto.C2CMessageCreateEvent](payload)
		assert.False(t, ok)
		assert.Equal(t, dto.C2CMessageCreateEvent{}, event)
	})
}

// TestParseEventSlice tests ParseEventSlice function
func TestParseEventSlice(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		events, err := ParseEventSlice[dto.C2CMessageCreateEvent]([]*dto.Payload{})
		assert.NoError(t, err)
		assert.Equal(t, []*dto.C2CMessageCreateEvent{}, events)
	})

	t.Run("success case", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 1"}`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 2"}`),
			},
		}

		events, err := ParseEventSlice[dto.C2CMessageCreateEvent](payloads)
		assert.NoError(t, err)
		require.Len(t, events, 2)
		assert.Equal(t, "message 1", events[0].Content)
		assert.Equal(t, "message 2", events[1].Content)
	})

	t.Run("fails on first error", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 1"}`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`invalid json`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 3"}`),
			},
		}

		events, err := ParseEventSlice[dto.C2CMessageCreateEvent](payloads)
		assert.Error(t, err)
		assert.Nil(t, events)
	})
}

// TestParseEventSlicePartial tests ParseEventSlicePartial function
func TestParseEventSlicePartial(t *testing.T) {
	t.Run("empty slice", func(t *testing.T) {
		events := ParseEventSlicePartial[dto.C2CMessageCreateEvent]([]*dto.Payload{})
		assert.Equal(t, []*dto.C2CMessageCreateEvent{}, events)
	})

	t.Run("all success", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 1"}`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 2"}`),
			},
		}

		events := ParseEventSlicePartial[dto.C2CMessageCreateEvent](payloads)
		require.Len(t, events, 2)
		assert.Equal(t, "message 1", events[0].Content)
		assert.Equal(t, "message 2", events[1].Content)
	})

	t.Run("skips failed parses", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 1"}`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`invalid json`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 3"}`),
			},
		}

		events := ParseEventSlicePartial[dto.C2CMessageCreateEvent](payloads)
		require.Len(t, events, 2)
		assert.Equal(t, "message 1", events[0].Content)
		assert.Equal(t, "message 3", events[1].Content)
	})
}

// TestFilterParseEvents tests FilterParseEvents function
func TestFilterParseEvents(t *testing.T) {
	t.Run("filters and parses", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				ID:     "1",
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 1"}`),
			},
			{
				ID:     "",
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 2"}`),
			},
			{
				ID:     "3",
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 3"}`),
			},
		}

		events := FilterParseEvents[dto.C2CMessageCreateEvent](payloads, func(p *dto.Payload) bool {
			return p.ID != ""
		})

		require.Len(t, events, 2)
		assert.Equal(t, "message 1", events[0].Content)
		assert.Equal(t, "message 3", events[1].Content)
	})

	t.Run("skips parse errors", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				ID:     "1",
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"message 1"}`),
			},
			{
				ID:     "2",
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`invalid json`),
			},
		}

		events := FilterParseEvents[dto.C2CMessageCreateEvent](payloads, func(p *dto.Payload) bool {
			return true
		})

		require.Len(t, events, 1)
		assert.Equal(t, "message 1", events[0].Content)
	})
}

// TestMapParseEvents tests MapParseEvents function
func TestMapParseEvents(t *testing.T) {
	t.Run("maps parsed events", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"hello"}`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"world"}`),
			},
		}

		contents, err := MapParseEvents(payloads, func(e *dto.C2CMessageCreateEvent) string {
			return e.Content
		})

		assert.NoError(t, err)
		assert.Equal(t, []string{"hello", "world"}, contents)
	})

	t.Run("returns error on parse failure", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`invalid json`),
			},
		}

		contents, err := MapParseEvents(payloads, func(e *dto.C2CMessageCreateEvent) string {
			return e.Content
		})

		assert.Error(t, err)
		assert.Nil(t, contents)
	})

	t.Run("transforms to different type", func(t *testing.T) {
		payloads := []*dto.Payload{
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"hello","author":{"id":"123"}}`),
			},
			{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content":"world","author":{"id":"456"}}`),
			},
		}

		type Summary struct {
			Content  string
			AuthorID string
		}

		summaries, err := MapParseEvents(payloads, func(e *dto.C2CMessageCreateEvent) Summary {
			return Summary{
				Content:  e.Content,
				AuthorID: e.Author.ID,
			}
		})

		assert.NoError(t, err)
		require.Len(t, summaries, 2)
		assert.Equal(t, "hello", summaries[0].Content)
		assert.Equal(t, "123", summaries[0].AuthorID)
		assert.Equal(t, "world", summaries[1].Content)
		assert.Equal(t, "456", summaries[1].AuthorID)
	})
}
