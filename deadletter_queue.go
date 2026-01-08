package remilia

import (
	"context"

	"github.com/KomeiDiSanXian/remilia/infra/dlq"
)

type DeadLetterQueueConfig = dlq.DeadLetterQueueConfig

type DropPolicy = dlq.DropPolicy

const (
	DropOldest      = dlq.DropOldest
	DropNewest      = dlq.DropNewest
	BlockUntilSpace = dlq.BlockUntilSpace
)

// DeadLetterQueue is a backward-compatible wrapper around infra/dlq.DeadLetterQueue.
//
// NOTE: We use a wrapper (not a type alias) because Go does not allow attaching
// methods to non-local types.
type DeadLetterQueue struct {
	q *dlq.DeadLetterQueue
}

type DeadLetterQueueStats = dlq.Stats

func NewDeadLetterQueue(config DeadLetterQueueConfig) *DeadLetterQueue {
	return &DeadLetterQueue{q: dlq.NewDeadLetterQueue(config)}
}

func (q *DeadLetterQueue) Start() { q.q.Start() }

func (q *DeadLetterQueue) Shutdown(ctx context.Context) error { return q.q.Shutdown(ctx) }

func (q *DeadLetterQueue) Enqueue(item DeadLetterItem) { q.q.Enqueue(dlq.DeadLetterItem(item)) }

func (q *DeadLetterQueue) Stats() DeadLetterQueueStats { return q.q.Stats() }

// DeadLetterConsumer is kept in root package for existing middleware/tests.
// Adapter wraps it to infra/dlq consumer.
func (q *DeadLetterQueue) AddConsumer(c DeadLetterConsumer) {
	q.q.AddConsumer(deadLetterConsumerAdapter{c: c})
}

type deadLetterConsumerAdapter struct{ c DeadLetterConsumer }

func (a deadLetterConsumerAdapter) Consume(item dlq.DeadLetterItem) {
	a.c.Consume(DeadLetterItem(item))
}
