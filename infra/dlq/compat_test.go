package dlq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRealWorldScenarios demonstrates real-world use cases
func TestRealWorldScenarios(t *testing.T) {
	t.Run("HTTP request retry queue", func(t *testing.T) {
		type HTTPRequest struct {
			URL     string
			Method  string
			Body    []byte
			Headers map[string]string
		}

		// Create a DLQ for failed HTTP requests
		httpQueue := New[*HTTPRequest](Config[*HTTPRequest]{
			MaxSize: 1000,
			Workers: 4,
			OnDropped: func(item Item[*HTTPRequest], reason string) {
				// Log dropped requests
				t.Logf("Dropped HTTP request to %s: %s", item.Data.URL, reason)
			},
		})
		defer httpQueue.Close(time.Second)

		// Enqueue a failed request
		err := httpQueue.Enqueue(Item[*HTTPRequest]{
			Data: &HTTPRequest{
				URL:    "https://api.example.com/users",
				Method: "POST",
				Body:   []byte(`{"name":"test"}`),
			},
			Err:     assert.AnError,
			Attempt: 3,
			Source:  "http-client",
		})

		assert.NoError(t, err)
	})

	t.Run("database operation retry queue", func(t *testing.T) {
		type DBOperation struct {
			SQL  string
			Args []any
		}

		// Create a DLQ for failed DB operations
		dbQueue := New[*DBOperation](Config[*DBOperation]{
			MaxSize:    500,
			Workers:    2,
			DropPolicy: DropPolicyOldest,
		})
		defer dbQueue.Close(time.Second)

		// Enqueue a failed operation
		err := dbQueue.Enqueue(Item[*DBOperation]{
			Data: &DBOperation{
				SQL:  "INSERT INTO users (name, email) VALUES (?, ?)",
				Args: []any{"John", "john@example.com"},
			},
			Err:     assert.AnError,
			Attempt: 2,
		})

		assert.NoError(t, err)
	})

	t.Run("message broker retry queue", func(t *testing.T) {
		type Message struct {
			Topic   string
			Key     string
			Payload []byte
		}

		msgQueue := New[*Message](Config[*Message]{
			MaxSize:    2000,
			Workers:    8,
			DropPolicy: DropPolicyBlockUntilSpace,
		})
		defer msgQueue.Close(time.Second)

		err := msgQueue.Enqueue(Item[*Message]{
			Data: &Message{
				Topic:   "user.events",
				Key:     "user-123",
				Payload: []byte(`{"event":"created"}`),
			},
			Err:    assert.AnError,
			Source: "kafka-producer",
		})

		assert.NoError(t, err)
	})

	t.Run("PlatformEventQueue works", func(t *testing.T) {
		q := NewPlatformEventQueue(PlatformEventConfig{
			MaxSize: 100,
			Workers: 1,
		})
		defer q.Close(time.Second)
		require.NotNil(t, q)
	})
}
