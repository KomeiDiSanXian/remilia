package dlq

import (
	stdctx "context"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBackwardCompatibility tests backward compatibility with existing code
func TestBackwardCompatibility(t *testing.T) {
	t.Run("PayloadQueue alias works", func(t *testing.T) {
		// Should be able to use PayloadQueue like the old DeadLetterQueue
		var q *PayloadQueue
		q = NewPayloadQueue(PayloadConfig{
			MaxSize: 100,
			Workers: 2,
		})
		defer q.Close(time.Second)

		assert.NotNil(t, q)
		assert.Equal(t, 100, q.config.MaxSize)
	})

	t.Run("type aliases work", func(t *testing.T) {
		// These should compile without issues
		var _ PayloadQueue
		var _ PayloadItem
		var _ PayloadConsumer
		var _ PayloadConfig
	})
}

// TestItemConversion tests conversion between old and new types
func TestItemConversion(t *testing.T) {
	t.Run("DeadLetterItem to Item", func(t *testing.T) {
		oldItem := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "test-123",
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: 3,
			Source:  "test-source",
		}

		newItem := DeadLetterItemToItem(oldItem)

		assert.Equal(t, oldItem.Event, newItem.Data)
		assert.Equal(t, oldItem.Err, newItem.Err)
		assert.Equal(t, oldItem.Attempt, newItem.Attempt)
		assert.Equal(t, oldItem.Source, newItem.Source)
	})

	t.Run("Item to DeadLetterItem", func(t *testing.T) {
		newItem := Item[*dto.Payload]{
			Data: &dto.Payload{
				ID:   "test-456",
				Type: dto.GroupAtMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: 2,
			Source:  "new-source",
		}

		oldItem := ItemToDeadLetterItem(newItem)

		assert.Equal(t, newItem.Data, oldItem.Event)
		assert.Equal(t, newItem.Err, oldItem.Err)
		assert.Equal(t, newItem.Attempt, oldItem.Attempt)
		assert.Equal(t, newItem.Source, oldItem.Source)
	})

	t.Run("round trip conversion", func(t *testing.T) {
		original := DeadLetterItem{
			Event: &dto.Payload{
				ID:   "round-trip",
				Type: dto.C2CMessageCreate,
			},
			Err:     assert.AnError,
			Attempt: 1,
			Source:  "test",
		}

		// Convert to new type and back
		newItem := DeadLetterItemToItem(original)
		restored := ItemToDeadLetterItem(newItem)

		assert.Equal(t, original.Event, restored.Event)
		assert.Equal(t, original.Err, restored.Err)
		assert.Equal(t, original.Attempt, restored.Attempt)
		assert.Equal(t, original.Source, restored.Source)
	})
}

// TestConsumerAdapter tests the consumer adapter
func TestConsumerAdapter(t *testing.T) {
	t.Run("adapter works with new queue", func(t *testing.T) {
		// Create a legacy consumer
		legacyConsumer := &mockLegacyConsumer{}

		// Wrap it in an adapter
		adapter := NewConsumerAdapter(legacyConsumer)

		// Use with new generic queue
		q := NewPayloadQueue(PayloadConfig{
			MaxSize: 10,
			Workers: 1,
		})

		q.AddConsumer(adapter)
		q.Start()
		defer q.Close(time.Second)

		// Enqueue an item
		err := q.Enqueue(Item[*dto.Payload]{
			Data: &dto.Payload{
				ID:   "adapter-test",
				Type: dto.C2CMessageCreate,
			},
			Attempt: 1,
		})
		require.NoError(t, err)

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Legacy consumer should have received it
		assert.Equal(t, 1, legacyConsumer.count)
		assert.Equal(t, "adapter-test", string(legacyConsumer.lastItem.Event.ID))
	})

	t.Run("multiple legacy consumers", func(t *testing.T) {
		consumer1 := &mockLegacyConsumer{}
		consumer2 := &mockLegacyConsumer{}

		q := NewPayloadQueue(PayloadConfig{
			MaxSize: 10,
			Workers: 1,
		})

		q.AddConsumer(NewConsumerAdapter(consumer1))
		q.AddConsumer(NewConsumerAdapter(consumer2))
		q.Start()
		defer q.Close(time.Second)

		q.Enqueue(Item[*dto.Payload]{
			Data: &dto.Payload{ID: "multi-test"},
		})

		time.Sleep(100 * time.Millisecond)

		assert.Equal(t, 1, consumer1.count)
		assert.Equal(t, 1, consumer2.count)
	})
}

// TestMigrationPath demonstrates migration from old to new API
func TestMigrationPath(t *testing.T) {
	t.Run("gradual migration", func(t *testing.T) {
		// Step 1: Start with old API (still works)
		oldQueue := NewDeadLetterQueue(DeadLetterQueueConfig{
			MaxSize: 100,
			Workers: 2,
		})
		defer oldQueue.Shutdown(stdctx.Background())

		// Step 2: Use new generic API alongside
		newQueue := NewPayloadQueue(PayloadConfig{
			MaxSize: 100,
			Workers: 2,
		})
		defer newQueue.Close(time.Second)

		// Step 3: Both should work identically
		assert.Equal(t, 100, oldQueue.config.MaxSize)
		assert.Equal(t, 100, newQueue.config.MaxSize)
	})
}

// Mock legacy consumer for testing
type mockLegacyConsumer struct {
	count    int
	lastItem DeadLetterItem
}

func (m *mockLegacyConsumer) Consume(item DeadLetterItem) {
	m.count++
	m.lastItem = item
}

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
}
