package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

type mockConsumer struct {
	ch chan remilia.DeadLetterItem
}

func (m *mockConsumer) Consume(item remilia.DeadLetterItem) {
	select {
	case m.ch <- item:
	default:
	}
}

func TestDeadLetter(t *testing.T) {
	// Setup DeadLetterQueue
	dlqConfig := remilia.DeadLetterQueueConfig{
		MaxSize: 10,
		Workers: 1,
	}
	dlq := remilia.NewDeadLetterQueue(dlqConfig)
	consumer := &mockConsumer{
		ch: make(chan remilia.DeadLetterItem, 10),
	}
	dlq.AddConsumer(consumer)
	dlq.Start()
	defer func() { _ = dlq.Shutdown(context.Background()) }()

	engine := remilia.NewEngine()

	// Chain: DeadLetter only
	engine.Use(DeadLetter(dlq))

	mockErr := errors.New("something went wrong")

	m := engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		ctx.SetRetryAttempt(3)
		return mockErr
	})
	m.Source = "test-source"

	ctx := remilia.NewContext(&dto.Payload{ID: "event-123", Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	// Verify DLQ received item
	select {
	case item := <-consumer.ch:
		assert.Equal(t, "event-123", string(item.Event.ID))
		assert.Equal(t, dto.C2CMessageCreate, item.Event.Type)
		assert.Equal(t, mockErr, item.Err)
		assert.Equal(t, "test-source", item.Source)
		assert.Equal(t, 3, item.Attempt)
	case <-time.After(1 * time.Second):
		t.Fatal("DeadLetterItem not received")
	}
}

func TestDeadLetter_NoError(t *testing.T) {
	dlq := remilia.NewDeadLetterQueue(remilia.DeadLetterQueueConfig{MaxSize: 10})
	mw := DeadLetter(dlq)

	successHandler := func(ctx *remilia.Context) error {
		return nil
	}

	chain := mw(successHandler)
	ctx := remilia.NewContext(&dto.Payload{}, nil)

	err := chain(ctx)
	assert.NoError(t, err)

	// Ensure nothing queued (check manually since no consumer attached)
	stats := dlq.Stats()
	assert.Equal(t, 0, stats.QueueSize)
}

func TestDeadLetter_WithRetry_AttemptPropagates(t *testing.T) {
	// Setup DeadLetterQueue
	dlqConfig := remilia.DeadLetterQueueConfig{MaxSize: 10, Workers: 1}
	dlq := remilia.NewDeadLetterQueue(dlqConfig)
	consumer := &mockConsumer{ch: make(chan remilia.DeadLetterItem, 10)}
	dlq.AddConsumer(consumer)
	dlq.Start()
	defer func() { _ = dlq.Shutdown(context.Background()) }()

	engine := remilia.NewEngine()

	// Chain: DeadLetter outside Retry
	engine.Use(DeadLetter(dlq))
	engine.Use(Retry(RetryConfig{MaxAttempts: 2, BackoffBase: time.Millisecond, BackoffMax: time.Millisecond}))

	mockErr := errors.New("always fail")
	m := engine.OnC2C().HandleE(func(ctx *remilia.Context) error { return mockErr })
	m.Source = "test-source"

	ctx := remilia.NewContext(&dto.Payload{ID: "event-456", Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	select {
	case item := <-consumer.ch:
		assert.Equal(t, 2, item.Attempt)
		assert.Equal(t, "test-source", item.Source)
		assert.Equal(t, mockErr, item.Err)
	case <-time.After(1 * time.Second):
		t.Fatal("DeadLetterItem not received")
	}
}
