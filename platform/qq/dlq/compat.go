// Package dlq provides platform-agnostic dead letter queue type aliases for the QQ adapter.
//
// After the D5 pool optimization, *dto.Payload objects are immediately returned
// to the sync.Pool after NewEvent() completes, so storing *dto.Payload pointers
// in a DLQ is unsafe. This package now exposes DLQ types based on platform.Event,
// which are always safe to hold after event creation.
//
// For the rare case where raw QQ payload bytes need to be preserved for replay,
// copy payload.Raw ([]byte) before NewEvent is called.
package dlq

import (
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// EventQueue is a dead letter queue for platform.Event values from the QQ adapter.
//
// This is the safe replacement for the former PayloadQueue: platform.Event objects
// are fully populated before the underlying *dto.Payload is released to the pool,
// so they are safe to hold indefinitely.
//
// Example:
//
//	q := qqdlq.NewEventQueue(qqdlq.EventConfig{
//	    MaxSize: 10000,
//	    Workers: 4,
//	})
//
//go:fix inline
type EventQueue = dlq.Queue[platform.Event]

// EventItem is a type alias for dlq.Item[platform.Event].
//
//go:fix inline
type EventItem = dlq.Item[platform.Event]

// EventConsumer is a type alias for dlq.Consumer[platform.Event].
//
//go:fix inline
type EventConsumer = dlq.Consumer[platform.Event]

// EventConfig is a type alias for dlq.Config[platform.Event].
//
//go:fix inline
type EventConfig = dlq.Config[platform.Event]

// NewEventQueue creates a new dead letter queue for platform.Event values.
//
//go:fix inline
func NewEventQueue(config EventConfig) *EventQueue {
	return dlq.New[platform.Event](config)
}
