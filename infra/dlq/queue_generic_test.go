package dlq

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data structures

type TestData struct {
	ID      int
	Message string
}

type TestConsumer struct {
	items     []Item[*TestData]
	mu        sync.Mutex
	onConsume func(Item[*TestData])
}

func (c *TestConsumer) Consume(item Item[*TestData]) {
	c.mu.Lock()
	c.items = append(c.items, item)
	c.mu.Unlock()

	if c.onConsume != nil {
		c.onConsume(item)
	}
}

func (c *TestConsumer) GetItems() []Item[*TestData] {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Item[*TestData](nil), c.items...)
}

func (c *TestConsumer) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// TestGenericQueue_BasicFunctionality tests basic queue operations
func TestGenericQueue_BasicFunctionality(t *testing.T) {
	t.Run("create with defaults", func(t *testing.T) {
		q := New[*TestData](Config[*TestData]{})
		defer func() { _ = q.Close(time.Second) }()

		assert.Equal(t, 10000, q.config.MaxSize)
		assert.Equal(t, 1, q.config.Workers)
		assert.False(t, q.IsClosed())
		assert.True(t, q.IsEmpty())
	})

	t.Run("create with custom config", func(t *testing.T) {
		q := New[*TestData](Config[*TestData]{
			MaxSize: 100,
			Workers: 4,
		})
		defer func() { _ = q.Close(time.Second) }()

		assert.Equal(t, 100, q.config.MaxSize)
		assert.Equal(t, 4, q.config.Workers)
	})
}

// TestGenericQueue_EnqueueDequeue tests enqueue and dequeue operations
func TestGenericQueue_EnqueueDequeue(t *testing.T) {
	t.Run("enqueue and consume", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			consumer := &TestConsumer{}
			q := New[*TestData](Config[*TestData]{
				MaxSize: 10,
				Workers: 1,
			})

			q.AddConsumer(consumer)
			q.Start()
			defer func() { _ = q.Close(time.Second) }()

			item := Item[*TestData]{
				Data:    &TestData{ID: 1, Message: "test"},
				Err:     errors.New("test error"),
				Attempt: 1,
				Source:  "test",
			}

			err := q.Enqueue(item)
			require.NoError(t, err)

			synctest.Wait()

			items := consumer.GetItems()
			require.Len(t, items, 1)
			assert.Equal(t, 1, items[0].Data.ID)
			assert.Equal(t, "test", items[0].Data.Message)
			assert.Equal(t, "test error", items[0].Err.Error())
		})
	})

	t.Run("multiple items", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			consumer := &TestConsumer{}
			q := New[*TestData](Config[*TestData]{
				MaxSize: 100,
				Workers: 2,
			})

			q.AddConsumer(consumer)
			q.Start()
			defer q.Close(time.Second)

			for i := range 10 {
				err := q.Enqueue(Item[*TestData]{
					Data: &TestData{ID: i, Message: "msg"},
				})
				require.NoError(t, err)
			}

			synctest.Wait()

			assert.Equal(t, 10, consumer.Count())
		})
	})
}

// TestGenericQueue_DropPolicy tests different drop policies
func TestGenericQueue_DropPolicy(t *testing.T) {
	t.Run("drop oldest", func(t *testing.T) {
		var dropped atomic.Int32
		q := New[*TestData](Config[*TestData]{
			MaxSize:    2,
			Workers:    1,
			DropPolicy: DropPolicyOldest,
			OnDropped: func(item Item[*TestData], reason string) {
				dropped.Add(1)
			},
		})
		defer func() { _ = q.Close(time.Second) }()

		_ = q.Enqueue(Item[*TestData]{Data: &TestData{ID: 1}})
		_ = q.Enqueue(Item[*TestData]{Data: &TestData{ID: 2}})
		_ = q.Enqueue(Item[*TestData]{Data: &TestData{ID: 3}})

		assert.Equal(t, int32(1), dropped.Load())
	})

	t.Run("drop newest", func(t *testing.T) {
		var dropped atomic.Int32
		q := New[*TestData](Config[*TestData]{
			MaxSize:    2,
			Workers:    1,
			DropPolicy: DropPolicyNewest,
			OnDropped: func(item Item[*TestData], reason string) {
				dropped.Add(1)
			},
		})
		defer q.Close(time.Second)

		q.Enqueue(Item[*TestData]{Data: &TestData{ID: 1}})
		q.Enqueue(Item[*TestData]{Data: &TestData{ID: 2}})

		err := q.Enqueue(Item[*TestData]{Data: &TestData{ID: 3}})
		assert.Error(t, err)
		assert.Equal(t, ErrQueueFull, err)

		assert.Equal(t, int32(1), dropped.Load())
	})

	t.Run("block until space", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			consumer := &TestConsumer{}
			q := New[*TestData](Config[*TestData]{
				MaxSize:    2,
				Workers:    1,
				DropPolicy: DropPolicyBlockUntilSpace,
			})

			q.AddConsumer(consumer)
			q.Start()
			defer q.Close(time.Second)

			q.Enqueue(Item[*TestData]{Data: &TestData{ID: 1}})
			q.Enqueue(Item[*TestData]{Data: &TestData{ID: 2}})

			done := make(chan bool)
			go func() {
				err := q.Enqueue(Item[*TestData]{Data: &TestData{ID: 3}})
				assert.NoError(t, err)
				done <- true
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("Enqueue blocked too long")
			}
		})
	})
}

// TestGenericQueue_Stats tests statistics
func TestGenericQueue_Stats(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		consumer := &TestConsumer{}
		q := New[*TestData](Config[*TestData]{
			MaxSize: 100,
			Workers: 2,
		})

		q.AddConsumer(consumer)
		q.Start()
		defer q.Close(time.Second)

		for i := range 5 {
			q.Enqueue(Item[*TestData]{Data: &TestData{ID: i}})
		}

		synctest.Wait()

		stats := q.Stats()
		assert.Equal(t, 100, stats.MaxSize)
		assert.Equal(t, 2, stats.Workers)
		assert.Equal(t, int64(5), stats.Processed)
		assert.Equal(t, 1, stats.Consumers)
		assert.False(t, stats.IsClosed)
	})
}

// TestGenericQueue_Close tests graceful shutdown
func TestGenericQueue_Close(t *testing.T) {
	t.Run("close empty queue", func(t *testing.T) {
		q := New[*TestData](Config[*TestData]{})
		q.Start()

		err := q.Close(time.Second)
		assert.NoError(t, err)
		assert.True(t, q.IsClosed())
	})

	t.Run("close with pending items", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			consumer := &TestConsumer{}
			q := New[*TestData](Config[*TestData]{
				MaxSize: 100,
				Workers: 1,
			})

			q.AddConsumer(consumer)
			q.Start()

			for i := range 10 {
				q.Enqueue(Item[*TestData]{Data: &TestData{ID: i}})
			}

			err := q.Close(2 * time.Second)
			assert.NoError(t, err)
			assert.Equal(t, 10, consumer.Count())
		})
	})

	t.Run("enqueue after close", func(t *testing.T) {
		q := New[*TestData](Config[*TestData]{})
		q.Start()
		q.Close(time.Second)

		err := q.Enqueue(Item[*TestData]{Data: &TestData{ID: 1}})
		assert.Error(t, err)
		assert.Equal(t, ErrQueueClosed, err)
	})
}

// TestGenericQueue_MultipleConsumers tests multiple consumers
func TestGenericQueue_MultipleConsumers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		consumer1 := &TestConsumer{}
		consumer2 := &TestConsumer{}

		q := New[*TestData](Config[*TestData]{
			MaxSize: 10,
			Workers: 1,
		})

		q.AddConsumer(consumer1)
		q.AddConsumer(consumer2)
		q.Start()
		defer q.Close(time.Second)

		item := Item[*TestData]{
			Data: &TestData{ID: 1, Message: "broadcast"},
		}

		q.Enqueue(item)
		synctest.Wait()

		assert.Equal(t, 1, consumer1.Count())
		assert.Equal(t, 1, consumer2.Count())
	})
}

// TestGenericQueue_ConsumerPanic tests panic recovery
func TestGenericQueue_ConsumerPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		panicking := &TestConsumer{
			onConsume: func(item Item[*TestData]) {
				panic("test panic")
			},
		}
		normal := &TestConsumer{}

		q := New[*TestData](Config[*TestData]{
			MaxSize: 10,
			Workers: 1,
		})

		q.AddConsumer(panicking)
		q.AddConsumer(normal)
		q.Start()
		defer q.Close(time.Second)

		q.Enqueue(Item[*TestData]{Data: &TestData{ID: 1}})
		synctest.Wait()

		assert.Equal(t, 1, normal.Count())
	})
}

// TestGenericQueue_OnCallbacks tests callbacks
func TestGenericQueue_OnCallbacks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var processedCount atomic.Int32

		q := New[*TestData](Config[*TestData]{
			MaxSize: 10,
			Workers: 1,
			OnProcessed: func(item Item[*TestData], duration time.Duration) {
				processedCount.Add(1)
			},
		})

		consumer := &TestConsumer{}
		q.AddConsumer(consumer)
		q.Start()
		defer q.Close(time.Second)

		for i := range 5 {
			q.Enqueue(Item[*TestData]{Data: &TestData{ID: i}})
		}

		synctest.Wait()

		assert.GreaterOrEqual(t, processedCount.Load(), int32(5), "Should have processed all 5 items")
	})
}

// TestGenericQueue_TypeSafety demonstrates type safety
func TestGenericQueue_TypeSafety(t *testing.T) {
	// Different data types use different queues
	type User struct {
		Name  string
		Email string
	}

	type Order struct {
		ID     int
		Amount float64
	}

	userQueue := New[*User](Config[*User]{MaxSize: 10})
	defer func() { _ = userQueue.Close(time.Second) }()

	orderQueue := New[*Order](Config[*Order]{MaxSize: 10})
	defer func() { _ = orderQueue.Close(time.Second) }()

	// Type-safe enqueue
	_ = userQueue.Enqueue(Item[*User]{
		Data: &User{Name: "Alice", Email: "alice@example.com"},
	})

	_ = orderQueue.Enqueue(Item[*Order]{
		Data: &Order{ID: 123, Amount: 99.99},
	})

	// Compile-time type checking prevents mixing types
	// userQueue.Enqueue(Item[*Order]{...}) // Would not compile!
}

// BenchmarkGenericQueue_Enqueue benchmarks enqueue operations
func BenchmarkGenericQueue_Enqueue(b *testing.B) {
	q := New[*TestData](Config[*TestData]{
		MaxSize: 100000,
		Workers: 4,
	})
	q.Start()
	defer q.Close(time.Second)

	item := Item[*TestData]{
		Data: &TestData{ID: 1, Message: "benchmark"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Enqueue(item)
	}
}

// BenchmarkGenericQueue_EnqueueParallel benchmarks parallel enqueue
func BenchmarkGenericQueue_EnqueueParallel(b *testing.B) {
	q := New[*TestData](Config[*TestData]{
		MaxSize: 100000,
		Workers: 8,
	})
	consumer := &TestConsumer{}
	q.AddConsumer(consumer)
	q.Start()
	defer q.Close(time.Second)

	item := Item[*TestData]{
		Data: &TestData{ID: 1, Message: "benchmark"},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.Enqueue(item)
		}
	})
}
