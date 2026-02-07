package plugin

import (
	"fmt"
	"sync"

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
	publishCount int64
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
	for _, sub := range handlers {
		go func(h EventHandler) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[EventBus] Panic in event handler: %v", r)
				}
			}()
			h(data)
		}(sub.handler)
	}

	eb.mu.Lock()
	eb.publishCount++
	eb.mu.Unlock()

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

	// 生成订阅 ID
	id := fmt.Sprintf("%s-%d", topic, len(eb.subscribers[topic]))

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
		PublishCount: eb.publishCount,
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
