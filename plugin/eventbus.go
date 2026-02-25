package plugin

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ErrEventDropped 在 goroutine 池满时，Publish 会返回此错误
var ErrEventDropped = errors.New("eventbus: worker pool full, event dropped")

// EventHandler 事件处理函数
type EventHandler func(data any)

// Subscription 订阅凭证
type Subscription interface {
	// Unsubscribe 取消订阅
	Unsubscribe() error

	// Topic 返回订阅的主题
	Topic() string
}

// EventBus 事件总线接口
type EventBus interface {
	// Publish 发布事件
	Publish(topic string, data any) error

	// Subscribe 订阅事件
	Subscribe(topic string, handler EventHandler) (Subscription, error)

	// SubscribeAll 订阅所有主题（通配符订阅）
	// 此订阅者会收到所有通过 Publish 发布的事件（topic 作为 data 的包装不传递，直接传原始 data）
	// 注意：通配符订阅者不会收到其他通配符订阅者的事件（防止无限循环）
	SubscribeAll(handler EventHandler) (Subscription, error)

	// Unsubscribe 取消订阅
	Unsubscribe(sub Subscription) error

	// GetStats 获取统计信息
	GetStats() EventBusStats
}

// EventBusStats 事件总线统计信息
type EventBusStats struct {
	TopicCount        int            // 主题数量
	SubscriptionCount int            // 订阅数量
	PublishCount      int64          // 发布事件总数
	DroppedCount      int64          // 池满时丢弃的事件总数
	TopicStats        map[string]int // 每个主题的订阅数
}

// eventBus 事件总线实现
type eventBus struct {
	subscribers  map[string][]subscriptionImpl // topic -> handlers
	publishCount atomic.Int64                  // 发布事件总数（原子操作，无需写锁）
	droppedCount atomic.Int64                  // 池满时丢弃的事件总数
	subIDCounter atomic.Int64                  // 全局单调递增订阅 ID，避免 ID 重复
	workerPool   chan struct{}                 // goroutine 池，限制并发数
	mu           sync.RWMutex
}

// subscriptionImpl 订阅实现
type subscriptionImpl struct {
	id      string
	topic   string
	handler EventHandler
	bus     *eventBus
}

// Topic 返回订阅的主题
func (s *subscriptionImpl) Topic() string {
	return s.topic
}

// Unsubscribe 取消订阅
func (s *subscriptionImpl) Unsubscribe() error {
	return s.bus.unsubscribeByID(s.topic, s.id)
}

// EventBusOptions EventBus 创建选项
type EventBusOptions struct {
	// WorkerPoolSize goroutine 池大小，控制事件处理的最大并发数。
	// 默认值 100；小于等于 0 时使用默认值。
	WorkerPoolSize int
}

// NewEventBus 创建事件总线（使用默认选项，池大小 100）
func NewEventBus() EventBus {
	return NewEventBusWithOptions(EventBusOptions{})
}

// NewEventBusWithOptions 使用指定选项创建事件总线
func NewEventBusWithOptions(opts EventBusOptions) EventBus {
	size := opts.WorkerPoolSize
	if size <= 0 {
		size = 100
	}
	return &eventBus{
		subscribers: make(map[string][]subscriptionImpl),
		workerPool:  make(chan struct{}, size),
	}
}

// wildcardTopic 通配符主题键，订阅此主题的处理器会收到所有事件
const wildcardTopic = "*"

// Publish 发布事件
func (eb *eventBus) Publish(topic string, data any) error {
	eb.mu.RLock()
	handlers := eb.subscribers[topic]
	// 通配符订阅者也应收到此事件（不对 wildcardTopic 本身触发通配符，避免无限循环）
	var wildcardHandlers []subscriptionImpl
	if topic != wildcardTopic {
		wildcardHandlers = eb.subscribers[wildcardTopic]
	}
	eb.mu.RUnlock()

	allHandlers := make([]subscriptionImpl, 0, len(handlers)+len(wildcardHandlers))
	allHandlers = append(allHandlers, handlers...)
	allHandlers = append(allHandlers, wildcardHandlers...)

	if len(allHandlers) == 0 {
		logger.Debugf("[EventBus] No subscribers for topic: %s", topic)
		return nil
	}

	var dropped bool

	// 异步通知所有订阅者（含通配符订阅者）
	for _, sub := range allHandlers {
		select {
		case eb.workerPool <- struct{}{}:
			// 成功获取令牌，正常受限并发
			go func(h EventHandler) {
				defer func() {
					<-eb.workerPool // 释放令牌
					if r := recover(); r != nil {
						logger.Errorf("[EventBus] Panic in event handler: %v", r)
					}
				}()
				h(data)
			}(sub.handler)
		default:
			// 池已满：丢弃本次事件并记录统计
			cnt := eb.droppedCount.Add(1)
			logger.Warnf("[EventBus] Worker pool full, dropping event for topic %s (total dropped: %d)", topic, cnt)
			dropped = true
		}
	}

	eb.publishCount.Add(1)
	logger.Debugf("[EventBus] Published event to topic: %s, subscribers: %d (wildcard: %d)", topic, len(handlers), len(wildcardHandlers))

	if dropped {
		return ErrEventDropped
	}
	return nil
}

// Subscribe 订阅事件
func (eb *eventBus) Subscribe(topic string, handler EventHandler) (Subscription, error) {
	if handler == nil {
		return nil, fmt.Errorf("handler cannot be nil")
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	// 修复 #6：使用全局单调递增 ID，避免"订阅→取消→再订阅"时 ID 重复导致误删
	id := fmt.Sprintf("%s-%d", topic, eb.subIDCounter.Add(1))

	sub := subscriptionImpl{
		id:      id,
		topic:   topic,
		handler: handler,
		bus:     eb,
	}

	eb.subscribers[topic] = append(eb.subscribers[topic], sub)

	logger.Debugf("[EventBus] Subscribed to topic: %s, id: %s", topic, id)
	return &sub, nil
}

// SubscribeAll 订阅所有主题（通配符订阅，实现 EventBus 接口）
func (eb *eventBus) SubscribeAll(handler EventHandler) (Subscription, error) {
	return eb.Subscribe(wildcardTopic, handler)
}

// Unsubscribe 取消订阅
func (eb *eventBus) Unsubscribe(sub Subscription) error {
	if sub == nil {
		return fmt.Errorf("subscription cannot be nil")
	}

	impl, ok := sub.(*subscriptionImpl)
	if !ok {
		return fmt.Errorf("invalid subscription type")
	}

	return eb.unsubscribeByID(impl.topic, impl.id)
}

// unsubscribeByID 通过 ID 取消订阅
func (eb *eventBus) unsubscribeByID(topic, id string) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	handlers, exists := eb.subscribers[topic]
	if !exists {
		return fmt.Errorf("topic not found: %s", topic)
	}

	// 查找并删除订阅
	for i, sub := range handlers {
		if sub.id == id {
			eb.subscribers[topic] = append(handlers[:i], handlers[i+1:]...)
			logger.Debugf("[EventBus] Unsubscribed from topic: %s, id: %s", topic, id)

			// 如果该主题没有订阅者了，删除该主题
			if len(eb.subscribers[topic]) == 0 {
				delete(eb.subscribers, topic)
			}

			return nil
		}
	}

	return fmt.Errorf("subscription not found: %s", id)
}

// GetStats 获取统计信息
func (eb *eventBus) GetStats() EventBusStats {
	eb.mu.RLock()
	defer eb.mu.RUnlock()

	stats := EventBusStats{
		TopicCount:   len(eb.subscribers),
		PublishCount: eb.publishCount.Load(),
		DroppedCount: eb.droppedCount.Load(),
		TopicStats:   make(map[string]int),
	}

	totalSubs := 0
	for topic, subs := range eb.subscribers {
		count := len(subs)
		stats.TopicStats[topic] = count
		totalSubs += count
	}

	stats.SubscriptionCount = totalSubs

	return stats
}

// --- 类型安全的 EventBus 辅助函数 ---

// Subscribe 以类型安全的方式订阅事件。
// 只有当事件数据可以断言为 T 时，handler 才会被调用；类型不匹配的事件被静默忽略。
//
//	// 订阅字符串类型的 plugin.loaded 事件
//	plugin.Subscribe[string](ctx.EventBus, "plugin.loaded", func(name string) {
//	    log.Printf("plugin loaded: %s", name)
//	})
func Subscribe[T any](bus EventBus, topic string, handler func(T)) (Subscription, error) {
	if bus == nil {
		return nil, fmt.Errorf("eventbus: bus is nil")
	}
	return bus.Subscribe(topic, func(data any) {
		if v, ok := data.(T); ok {
			handler(v)
		}
	})
}

// PublishTyped 发布强类型事件（语义等同于 bus.Publish，增加编译期类型检查）。
//
//	plugin.PublishTyped(ctx.EventBus, "user.banned", userID)
func PublishTyped[T any](bus EventBus, topic string, data T) error {
	if bus == nil {
		return fmt.Errorf("eventbus: bus is nil")
	}
	return bus.Publish(topic, data)
}

// --- DryRun no-op 实现 ---

// noopSubscription DryRun 阶段的空订阅凭证
type noopSubscription struct{ topic string }

func (s *noopSubscription) Unsubscribe() error { return nil }
func (s *noopSubscription) Topic() string      { return s.topic }

// noopEventBus DryRun 阶段注入的空事件总线。
//
// 所有操作均为无副作用的空操作，避免推断阶段的 Setup 调用向真实 EventBus 注册订阅。
// 插件代码无需感知 DryRun，直接使用 ctx.EventBus 即可。
type noopEventBus struct{}

func (n *noopEventBus) Publish(_ string, _ any) error { return nil }
func (n *noopEventBus) Subscribe(topic string, _ EventHandler) (Subscription, error) {
	return &noopSubscription{topic: topic}, nil
}
func (n *noopEventBus) SubscribeAll(_ EventHandler) (Subscription, error) {
	return &noopSubscription{topic: "*"}, nil
}
func (n *noopEventBus) Unsubscribe(_ Subscription) error { return nil }
func (n *noopEventBus) GetStats() EventBusStats          { return EventBusStats{} }

// newNoopEventBus 创建 DryRun 阶段使用的空事件总线
func newNoopEventBus() EventBus { return &noopEventBus{} }
