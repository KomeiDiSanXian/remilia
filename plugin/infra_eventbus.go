package plugin

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

// ErrEventDropped PublishWithTimeout 超时无法获取工作槽位时返回此错误。
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

// Publish 发布事件。
//
// 在 goroutine 池满时**阻塞**等待可用槽位，提供背压而非静默丢弃。
// 若需要超时控制，请使用 [PublishWithTimeout]。
func (eb *eventBus) Publish(topic string, data any) error {
	eb.mu.RLock()
	handlers := eb.subscribers[topic]
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

	for _, sub := range allHandlers {
		eb.workerPool <- struct{}{} // blocking: 提供背压而非静默丢弃
		go func(h EventHandler) {
			defer func() {
				<-eb.workerPool
				if r := recover(); r != nil {
					logger.Errorf("[EventBus] Panic in event handler: %v", r)
				}
			}()
			h(data)
		}(sub.handler)
	}

	eb.publishCount.Add(1)
	logger.Debugf("[EventBus] Published event to topic: %s, subscribers: %d (wildcard: %d)", topic, len(handlers), len(wildcardHandlers))
	return nil
}

// PublishWithTimeout 发布事件，若 timeout 内无法获取工作槽位则丢弃并返回 [ErrEventDropped]。
func PublishWithTimeout(bus EventBus, topic string, data any, timeout time.Duration) error {
	eb, ok := bus.(*eventBus)
	if !ok {
		return bus.Publish(topic, data)
	}

	eb.mu.RLock()
	handlers := eb.subscribers[topic]
	var wildcardHandlers []subscriptionImpl
	if topic != wildcardTopic {
		wildcardHandlers = eb.subscribers[wildcardTopic]
	}
	eb.mu.RUnlock()

	allHandlers := make([]subscriptionImpl, 0, len(handlers)+len(wildcardHandlers))
	allHandlers = append(allHandlers, handlers...)
	allHandlers = append(allHandlers, wildcardHandlers...)

	if len(allHandlers) == 0 {
		return nil
	}

	var dropped bool
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for _, sub := range allHandlers {
		select {
		case eb.workerPool <- struct{}{}:
		case <-timer.C:
			cnt := eb.droppedCount.Add(1)
			logger.Warnf("[EventBus] PublishWithTimeout timed out for topic %s (total dropped: %d)", topic, cnt)
			dropped = true
			continue
		}
		go func(h EventHandler) {
			defer func() {
				<-eb.workerPool
				if r := recover(); r != nil {
					logger.Errorf("[EventBus] Panic in event handler: %v", r)
				}
			}()
			h(data)
		}(sub.handler)

		if !timer.Stop() {
			<-timer.C
		}
		timer.Reset(timeout)
	}

	eb.publishCount.Add(1)
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

// --- 类型安全的 EventBus 泛型辅助函数 (P2-6) ---
//
// 设计目标：
//   - 消除 EventHandler(func(any)) 中的运行时类型断言
//   - 提供编译期类型检查，类型不匹配时直接编译失败（而非运行时 panic/静默忽略）
//   - 向后兼容：所有泛型函数均构建在现有 EventBus 接口之上，不破坏旧代码
//
// 推荐用法：
//
//	// 1. 函数级辅助（轻量）
//	sub, err := plugin.Subscribe[MyEvent](ctx.EventBus, "my.event", func(e MyEvent) { ... })
//	err = plugin.PublishTyped(ctx.EventBus, "my.event", MyEvent{...})
//
//	// 2. TypedChannel（强约束，推荐跨插件事件契约）
//	ch := plugin.NewTypedChannel[MyEvent](ctx.EventBus, "my.event")
//	sub, err := ch.Subscribe(func(e MyEvent) { ... })
//	err  = ch.Publish(MyEvent{...})

// Subscribe 以类型安全的方式订阅事件。
//
// 只有当事件数据可成功断言为 T 时，handler 才会被调用。
// 类型不匹配的事件被静默跳过（不 panic、不报错），便于同一 topic 传递多种类型时分层处理。
//
// 示例：
//
//	sub, err := plugin.Subscribe[string](ctx.EventBus, "plugin.loaded", func(name string) {
//	    log.Printf("plugin loaded: %s", name)
//	})
func Subscribe[T any](bus EventBus, topic string, handler func(T)) (Subscription, error) {
	if bus == nil {
		return nil, fmt.Errorf("eventbus: bus is nil")
	}
	if handler == nil {
		return nil, fmt.Errorf("eventbus: handler is nil")
	}
	return bus.Subscribe(topic, func(data any) {
		if v, ok := data.(T); ok {
			handler(v)
		}
	})
}

// MustSubscribe 订阅事件，失败时 panic。
//
// 适用于初始化阶段（Setup）确信不会失败的订阅；
// 返回 Subscription 方便链式调用或显式取消。
//
// 示例：
//
//	sub := plugin.MustSubscribe[UserEvent](ctx.EventBus, "user.login", func(e UserEvent) {
//	    // handle login
//	})
func MustSubscribe[T any](bus EventBus, topic string, handler func(T)) Subscription {
	sub, err := Subscribe[T](bus, topic, handler)
	if err != nil {
		panic(fmt.Sprintf("eventbus: MustSubscribe[%T] on topic %q failed: %v", *new(T), topic, err))
	}
	return sub
}

// SubscribeAllTyped 以类型安全的方式订阅所有主题（通配符订阅）。
//
// 收到任意 topic 的事件后，若 data 可断言为 T，则调用 handler；否则静默跳过。
// 适用于"监听所有同类事件"的场景，如统计所有 *MetricEvent 类型的事件。
//
// 示例：
//
//	plugin.SubscribeAllTyped[MetricEvent](ctx.EventBus, func(e MetricEvent) {
//	    metrics.Record(e)
//	})
func SubscribeAllTyped[T any](bus EventBus, handler func(T)) (Subscription, error) {
	if bus == nil {
		return nil, fmt.Errorf("eventbus: bus is nil")
	}
	if handler == nil {
		return nil, fmt.Errorf("eventbus: handler is nil")
	}
	return bus.SubscribeAll(func(data any) {
		if v, ok := data.(T); ok {
			handler(v)
		}
	})
}

// PublishTyped 发布强类型事件。
//
// 语义等同于 bus.Publish(topic, data)，但增加编译期类型检查：
// 调用点的 data 类型必须与泛型参数 T 匹配，否则编译失败。
//
// 示例：
//
//	err := plugin.PublishTyped(ctx.EventBus, "user.banned", userID) // userID 为 string
func PublishTyped[T any](bus EventBus, topic string, data T) error {
	if bus == nil {
		return fmt.Errorf("eventbus: bus is nil")
	}
	return bus.Publish(topic, data)
}

// MustPublishTyped 发布强类型事件，失败时 panic。
//
// 适用于确信 Publish 不会失败（无 worker pool 溢出风险）的场景。
func MustPublishTyped[T any](bus EventBus, topic string, data T) {
	if err := PublishTyped[T](bus, topic, data); err != nil {
		panic(fmt.Sprintf("eventbus: MustPublishTyped[%T] on topic %q failed: %v", data, topic, err))
	}
}

// TypedChannel 将 topic 与类型 T 绑定为一个具名契约对象。
//
// TypedChannel 是跨插件事件契约的推荐方式：
//   - 在共享的 types 包（或插件公共接口）中定义 TypedChannel 常量
//   - 发布方和订阅方引用同一个 TypedChannel，杜绝 topic 字符串拼写错误
//   - 编译期保证发布和订阅的类型一致
//
// 示例（跨插件事件契约）：
//
//	// 在共享类型文件中定义（如 events.go）
//	var UserLoginEvent = plugin.NewTypedChannel[UserLogin](nil, "user.login")
//
//	// 发布方（在 Setup 中绑定真实的 EventBus）
//	ch := UserLoginEvent.WithBus(ctx.EventBus)
//	ch.Publish(UserLogin{UserID: "u123"})
//
//	// 订阅方
//	ch := UserLoginEvent.WithBus(ctx.EventBus)
//	ch.Subscribe(func(e UserLogin) { ... })
type TypedChannel[T any] struct {
	bus   EventBus
	topic string
}

// NewTypedChannel 创建绑定了 topic 的类型安全通道。
//
// bus 可为 nil（用于定义全局契约常量），使用前需调用 WithBus 绑定真实 EventBus。
func NewTypedChannel[T any](bus EventBus, topic string) TypedChannel[T] {
	return TypedChannel[T]{bus: bus, topic: topic}
}

// WithBus 返回绑定了指定 EventBus 的新 TypedChannel（不修改原对象，线程安全）。
func (tc TypedChannel[T]) WithBus(bus EventBus) TypedChannel[T] {
	return TypedChannel[T]{bus: bus, topic: tc.topic}
}

// Topic 返回此通道绑定的 topic 字符串。
func (tc TypedChannel[T]) Topic() string { return tc.topic }

// Publish 向此通道发布一条强类型消息。
func (tc TypedChannel[T]) Publish(data T) error {
	return PublishTyped[T](tc.bus, tc.topic, data)
}

// Subscribe 订阅此通道的强类型消息。
func (tc TypedChannel[T]) Subscribe(handler func(T)) (Subscription, error) {
	return Subscribe[T](tc.bus, tc.topic, handler)
}

// MustPublish 发布消息，失败时 panic。
func (tc TypedChannel[T]) MustPublish(data T) {
	MustPublishTyped[T](tc.bus, tc.topic, data)
}

// MustSubscribe 订阅消息，失败时 panic。
func (tc TypedChannel[T]) MustSubscribe(handler func(T)) Subscription {
	return MustSubscribe[T](tc.bus, tc.topic, handler)
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
