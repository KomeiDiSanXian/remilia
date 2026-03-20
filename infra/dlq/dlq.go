package dlq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	infraatomic "github.com/KomeiDiSanXian/remilia/infra/atomic"
)

// Error definitions
var (
	ErrQueueClosed       = errors.New("queue is closed")
	ErrQueueFull         = errors.New("queue is full")
	ErrInvalidDropPolicy = errors.New("invalid drop policy")
	ErrCloseTimeout      = errors.New("close operation timed out")
)

// DropPolicy defines the behavior when the queue is full.
type DropPolicy int

const (
	DropPolicyOldest          DropPolicy = iota // Drop the oldest item when full
	DropPolicyNewest                            // Drop the new item when full
	DropPolicyBlockUntilSpace                   // Block until space is available
)

// Stats holds current queue statistics.
type Stats struct {
	QueueSize  int
	MaxSize    int
	Processed  int64
	Dropped    int64
	Workers    int
	IsClosed   bool
	Consumers  int
	DropPolicy DropPolicy
}

// Item represents a generic dead letter entry.
//
// This is a type-safe version of DeadLetterItem that works with any data type.
//
// Example:
//
//	// For HTTP requests
//	type FailedRequest struct {
//	    URL     string
//	    Body    []byte
//	    Headers map[string]string
//	}
//	httpItem := dlq.Item[*FailedRequest]{
//	    Data:    &FailedRequest{URL: "https://api.example.com"},
//	    Err:     errors.New("connection timeout"),
//	    Attempt: 3,
//	    Source:  "http-client",
//	}
type Item[T any] struct {
	Data    T      // The failed data
	Err     error  // The error that caused the failure
	Attempt int    // Number of retry attempts
	Source  string // Source identifier
}

// Consumer consumes dead letter items of type T.
//
// Implementations should be thread-safe as they may be called
// concurrently by multiple workers.
type Consumer[T any] interface {
	Consume(item Item[T])
}

// Config configures a generic dead letter queue.
type Config[T any] struct {
	MaxSize     int                                        // Maximum queue size
	Workers     int                                        // Number of consumer workers
	DropPolicy  DropPolicy                                 // Policy when queue is full
	OnDropped   func(item Item[T], reason string)          // Callback when item is dropped
	OnProcessed func(item Item[T], duration time.Duration) // Callback when item is processed
}

// Queue is a generic dead letter queue that can handle any data type.
//
// It provides the same functionality as DeadLetterQueue but with type safety.
//
// Example:
//
//	// Create a DLQ for failed HTTP requests
//	type FailedRequest struct {
//	    URL  string
//	    Body []byte
//	}
//
//	dlq := dlq.New[*FailedRequest](dlq.Config[*FailedRequest]{
//	    MaxSize: 1000,
//	    Workers: 4,
//	    OnDropped: func(item dlq.Item[*FailedRequest], reason string) {
//	        log.Printf("Dropped request to %s: %s", item.Data.URL, reason)
//	    },
//	})
//
//	// Add a consumer
//	dlq.AddConsumer(myHTTPConsumer)
//
//	// Start processing
//	dlq.Start()
//
//	// Enqueue failed requests
//	dlq.Enqueue(dlq.Item[*FailedRequest]{
//	    Data:    &FailedRequest{URL: "https://api.example.com"},
//	    Err:     errors.New("timeout"),
//	    Attempt: 3,
//	})
type Queue[T any] struct {
	config       Config[T]
	queue        chan Item[T]
	consumers    []Consumer[T]
	consumerSnap *infraatomic.Value[[]Consumer[T]]

	dropped   atomic.Int64
	processed atomic.Int64

	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.RWMutex
	enqueueMu   sync.Mutex
	queueClosed atomic.Bool
	closeOnce   sync.Once
}
