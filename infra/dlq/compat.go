package dlq

import (
	"github.com/KomeiDiSanXian/remilia/platform"
)

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
