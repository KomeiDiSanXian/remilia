package dlq

import (
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// Backward compatibility type aliases
//
// These aliases allow existing code to continue working without changes
// while providing a smooth migration path to the generic version.

// PayloadQueue is a backward-compatible alias for Queue[*dto.Payload].
//
// Example migration:
//
//	// Old code (still works):
//	var dlq *dlq.DeadLetterQueue
//
//	// New code (type-safe):
//	var dlq *dlq.PayloadQueue
type PayloadQueue = Queue[*dto.Payload]

// PayloadItem is a backward-compatible alias for Item[*dto.Payload].
type PayloadItem = Item[*dto.Payload]

// PayloadConsumer is a backward-compatible alias for Consumer[*dto.Payload].
type PayloadConsumer = Consumer[*dto.Payload]

// PayloadConfig is a backward-compatible alias for Config[*dto.Payload].
type PayloadConfig = Config[*dto.Payload]

// NewPayloadQueue creates a new dead letter queue for dto.Payload.
//
// This is a convenience function that provides the same interface as
// NewDeadLetterQueue but uses the generic implementation.
//
// Example:
//
//	dlq := dlq.NewPayloadQueue(dlq.PayloadConfig{
//	    MaxSize: 10000,
//	    Workers: 4,
//	})
func NewPayloadQueue(config PayloadConfig) *PayloadQueue {
	return New[*dto.Payload](config)
}

// ---- 平台无关 DLQ 类型别名 -------------------------------------------------------

// PlatformEventQueue 是平台无关的死信队列（Queue[platform.Event]）。
//
// 新平台适配器应使用此类型，不依赖 dto.Payload。
type PlatformEventQueue = Queue[platform.Event]

// PlatformEventItem 是平台无关的死信队列条目。
type PlatformEventItem = Item[platform.Event]

// PlatformEventConsumer 是平台无关的死信队列消费者接口。
type PlatformEventConsumer = Consumer[platform.Event]

// PlatformEventConfig 是平台无关的死信队列配置。
type PlatformEventConfig = Config[platform.Event]

// NewPlatformEventQueue 创建平台无关的死信队列。
func NewPlatformEventQueue(config PlatformEventConfig) *PlatformEventQueue {
	return New[platform.Event](config)
}

// Helper functions to convert between old and new types

// ItemToDeadLetterItem converts a generic Item to DeadLetterItem.
//
// This is useful when migrating from the generic API to legacy code.
func ItemToDeadLetterItem(item Item[*dto.Payload]) DeadLetterItem {
	return DeadLetterItem{
		Event:   item.Data,
		Err:     item.Err,
		Attempt: item.Attempt,
		Source:  item.Source,
	}
}

// DeadLetterItemToItem converts DeadLetterItem to generic Item.
//
// This is useful when migrating from legacy code to the generic API.
func DeadLetterItemToItem(item DeadLetterItem) Item[*dto.Payload] {
	return Item[*dto.Payload]{
		Data:    item.Event,
		Err:     item.Err,
		Attempt: item.Attempt,
		Source:  item.Source,
	}
}

// ConsumerAdapter adapts a DeadLetterConsumer to work with PayloadQueue.
type ConsumerAdapter struct {
	consumer DeadLetterConsumer
}

// NewConsumerAdapter creates an adapter for legacy consumers.
//
// Example:
//
//	legacyConsumer := &MyOldConsumer{}
//	adapter := dlq.NewConsumerAdapter(legacyConsumer)
//	payloadQueue.AddConsumer(adapter)
func NewConsumerAdapter(consumer DeadLetterConsumer) *ConsumerAdapter {
	return &ConsumerAdapter{consumer: consumer}
}

// Consume implements Consumer[*dto.Payload] by adapting to DeadLetterConsumer.
func (a *ConsumerAdapter) Consume(item Item[*dto.Payload]) {
	a.consumer.Consume(ItemToDeadLetterItem(item))
}
