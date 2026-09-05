package minecraft

import (
	"sync"
	"time"
)

// ttlCache 简单的泛型 TTL 缓存：并发安全，读取时惰性过期。
// 容量超限时先清理一轮过期项；仍有余量不足则淘汰最早过期者。
type ttlCache[T any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	entries map[string]ttlEntry[T]
}

type ttlEntry[T any] struct {
	value  T
	expire time.Time
}

func newTTLCache[T any](ttl time.Duration, maxEntries int) *ttlCache[T] {
	if maxEntries < 1 {
		maxEntries = 1
	}
	return &ttlCache[T]{
		ttl:     ttl,
		max:     maxEntries,
		entries: make(map[string]ttlEntry[T]),
	}
}

func (c *ttlCache[T]) get(key string) (T, bool) {
	if c == nil {
		var zero T
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero T
	e, ok := c.entries[key]
	if !ok {
		return zero, false
	}
	if time.Now().After(e.expire) {
		delete(c.entries, key)
		return zero, false
	}
	return e.value, true
}

func (c *ttlCache[T]) set(key string, value T) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.evictLocked(time.Now())
	}
	c.entries[key] = ttlEntry[T]{value: value, expire: time.Now().Add(c.ttl)}
}

// evictLocked 清除全部过期项；容量仍满则淘汰最早过期者。
// 调用方必须持有 mu。
func (c *ttlCache[T]) evictLocked(now time.Time) {
	for k, e := range c.entries {
		if now.After(e.expire) {
			delete(c.entries, k)
		}
	}
	for len(c.entries) >= c.max {
		oldestKey := ""
		var oldest time.Time
		first := true
		for k, e := range c.entries {
			if first || e.expire.Before(oldest) {
				oldestKey, oldest, first = k, e.expire, false
			}
		}
		if oldestKey == "" {
			return
		}
		delete(c.entries, oldestKey)
	}
}
