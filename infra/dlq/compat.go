package dlq

import (
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/platform/qq/openapi/dto"
)

// ─── QQ Payload DLQ 类型别名 ─────────────────────────────────────────────────

// PayloadQueue is a type alias for Queue[*dto.Payload].
//
// Use this for QQ-platform dead letter queues.
//
// Example:
//
//	q := dlq.NewPayloadQueue(dlq.PayloadConfig{
//	    MaxSize: 10000,
//	    Workers: 4,
//	})
type PayloadQueue = Queue[*dto.Payload]

// PayloadItem is a type alias for Item[*dto.Payload].
type PayloadItem = Item[*dto.Payload]

// PayloadConsumer is a type alias for Consumer[*dto.Payload].
type PayloadConsumer = Consumer[*dto.Payload]

// PayloadConfig is a type alias for Config[*dto.Payload].
type PayloadConfig = Config[*dto.Payload]

// NewPayloadQueue creates a new dead letter queue for dto.Payload.
//
// Example:
//
//	q := dlq.NewPayloadQueue(dlq.PayloadConfig{
//	    MaxSize: 10000,
//	    Workers: 4,
//	})
func NewPayloadQueue(config PayloadConfig) *PayloadQueue {
	return New[*dto.Payload](config)
}

// ─── 平台无关 DLQ 类型别名 ───────────────────────────────────────────────────

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
