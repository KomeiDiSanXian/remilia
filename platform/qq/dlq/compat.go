// Package dlq provides QQ-platform-specific dead letter queue type aliases.
//
// These aliases simplify working with DLQs that carry *dto.Payload events.
// For platform-agnostic DLQs, use the generic types from [infra/dlq] directly
// (e.g. dlq.Queue[platform.Event]).
package dlq

import (
	"github.com/KomeiDiSanXian/remilia/infra/dlq"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// PayloadQueue is a dead letter queue for QQ *dto.Payload events.
//
// Example:
//
//	q := qqdlq.NewPayloadQueue(qqdlq.PayloadConfig{
//	    MaxSize: 10000,
//	    Workers: 4,
//	})
type PayloadQueue = dlq.Queue[*dto.Payload]

// PayloadItem is a type alias for dlq.Item[*dto.Payload].
type PayloadItem = dlq.Item[*dto.Payload]

// PayloadConsumer is a type alias for dlq.Consumer[*dto.Payload].
type PayloadConsumer = dlq.Consumer[*dto.Payload]

// PayloadConfig is a type alias for dlq.Config[*dto.Payload].
type PayloadConfig = dlq.Config[*dto.Payload]

// NewPayloadQueue creates a new dead letter queue for *dto.Payload events.
func NewPayloadQueue(config PayloadConfig) *PayloadQueue {
	return dlq.New[*dto.Payload](config)
}
