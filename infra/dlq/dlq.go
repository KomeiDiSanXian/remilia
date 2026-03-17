package dlq

import "errors"

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
