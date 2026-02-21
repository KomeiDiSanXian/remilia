package plugin

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/KomeiDiSanXian/remilia/infra/logger"
)

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
	TopicStats        map[string]int // 每个主题的订阅数
}

// eventBus 事件总线实现
type eventBus struct {
	subscribers  map[string][]subscriptionImpl // topic -> handlers
	publishCount atomic.Int64                  // 发布事件总数（原子操作，无需写锁）
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

// NewEventBus 创建事件总线
func NewEventBus() EventBus {
	return &eventBus{
		subscribers: make(map[string][]subscriptionImpl),
		workerPool:  make(chan struct{}, 100), // 限制最多 100 个并发 goroutine
	}
}

// Publish 发布事件
func (eb *eventBus) Publish(topic string, data any) error {
	eb.mu.RLock()
	handlers := eb.subscribers[topic]
	eb.mu.RUnlock()

	if len(handlers) == 0 {
		logger.Debugf("[EventBus] No subscribers for topic: %s", topic)
		return nil
	}

	// 异步通知所有订阅者
	// 修复 #1：使用非阻塞 select 避免当 workerPool 满时卡死调用方（主事件循环）。
	// 池满时直接启动 goroutine（不受并发限制），并记录警告。
	for _, sub := range handlers {
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
			// 池已满：不阻塞调用方，直接启动 goroutine（不计入池）
			logger.Warnf("[EventBus] Worker pool full for topic %s, running handler without pool limit", topic)
			go func(h EventHandler) {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[EventBus] Panic in event handler: %v", r)
					}
				}()
				h(data)
			}(sub.handler)
		}
	}

	// 修复 #18：使用 atomic 操作替代写锁，减少锁竞争
	eb.publishCount.Add(1)

	logger.Debugf("[EventBus] Published event to topic: %s, subscribers: %d", topic, len(handlers))
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
		PublishCount: eb.publishCount.Load(), // 使用 atomic.Load
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
