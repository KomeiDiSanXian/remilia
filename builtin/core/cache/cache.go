package cache

import (
	"container/list"
	"sync"
	"time"

	"github.com/KomeiDiSanXian/remilia/plugin"
)

// Plugin 缓存插件 API
type Plugin struct {
	cache *LRUCache
}

// Entry 缓存条目
type Entry struct {
	Key       string
	Value     []byte
	ExpiresAt time.Time
	Hits      int64
}

// LRUCache LRU 缓存实现
type LRUCache struct {
	capacity int
	items    map[string]*list.Element
	order    *list.List
	mu       sync.RWMutex
	stats    Stats
}

// Stats 缓存统计
type Stats struct {
	Hits        int64
	Misses      int64
	Evictions   int64
	Expirations int64
	mu          sync.RWMutex
}

// New 创建缓存插件（v2 API）
func New() *plugin.Descriptor {
	return NewV2WithCapacity(1000)
}

// NewV2WithCapacity 创建指定容量的缓存插件（v2 API）
func NewV2WithCapacity(capacity int) *plugin.Descriptor {
	// 创建缓存实例（闭包捕获）
	cache := NewLRUCache(capacity)

	// 创建 Plugin 包装器供其他插件使用
	pluginAPI := &Plugin{
		cache: cache,
	}

	return &plugin.Descriptor{
		Name:    "cache",
		Version: "2.0.0",
		Deps:    []string{},
		Meta: &plugin.Metadata{
			Author:      "Remilia Team",
			Description: "高性能 LRU 缓存插件，减少重复计算和外部请求",
			Category:    "核心",
			Tags:        []string{"缓存", "性能", "LRU", "核心"},
			HelpText: `缓存插件使用说明：
  cachePlugin := plugin.Require[cache.Plugin](ctx, "cache")
  cachePlugin.Set(key, value, ttl)
  cachePlugin.Get(key)
  cachePlugin.Delete(key)
  cachePlugin.Clear()
  cachePlugin.Stats()`,
		},

		Setup: func(ctx *plugin.SetupContext) (any, error) {
			ctx.Log.Infof("Loading cache plugin (capacity=%d)", capacity)
			return pluginAPI, nil // 框架自动 ExportAs("cache", pluginAPI)
		},

		Teardown: func(ctx *plugin.TeardownContext) error {
			ctx.Log.Info("Unloading cache plugin")
			cache.Clear()
			return nil
		},
	}
}

// NewLRUCache 创建 LRU 缓存
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Get 获取缓存
func (p *Plugin) Get(key string) ([]byte, bool) {
	return p.cache.Get(key)
}

// Set 设置缓存
func (p *Plugin) Set(key string, value []byte, ttl time.Duration) {
	p.cache.Set(key, value, ttl)
}

// Delete 删除缓存
func (p *Plugin) Delete(key string) {
	p.cache.Delete(key)
}

// Clear 清空缓存
func (p *Plugin) Clear() {
	p.cache.Clear()
}

// Stats 获取统计信息
func (p *Plugin) Stats() Stats {
	return p.cache.Stats()
}

// Size 获取当前缓存大小
func (p *Plugin) Size() int {
	return p.cache.Size()
}

// Get 获取缓存值
func (c *LRUCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		c.recordMiss()
		return nil, false
	}

	entry := elem.Value.(*Entry)

	// 检查是否过期
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		c.removeElement(elem)
		c.recordExpiration()
		c.recordMiss()
		return nil, false
	}

	// 更新 LRU 顺序
	c.order.MoveToFront(elem)
	entry.Hits++

	c.recordHit()

	// 返回副本
	result := make([]byte, len(entry.Value))
	copy(result, entry.Value)
	return result, true
}

// Set 设置缓存值
func (c *LRUCache) Set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 复制值
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	entry := &Entry{
		Key:   key,
		Value: valueCopy,
	}

	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}

	// 如果键已存在，更新它
	if elem, exists := c.items[key]; exists {
		c.order.MoveToFront(elem)
		elem.Value = entry
		return
	}

	// 检查容量
	if c.order.Len() >= c.capacity {
		c.evictOldest()
	}

	// 添加新条目
	elem := c.order.PushFront(entry)
	c.items[key] = elem
}

// Delete 删除缓存
func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.items[key]; exists {
		c.removeElement(elem)
	}
}

// Clear 清空缓存
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.order = list.New()
}

// Size 返回当前缓存大小
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.order.Len()
}

// Stats 返回缓存统计
func (c *LRUCache) Stats() Stats {
	c.stats.mu.RLock()
	defer c.stats.mu.RUnlock()

	return Stats{
		Hits:        c.stats.Hits,
		Misses:      c.stats.Misses,
		Evictions:   c.stats.Evictions,
		Expirations: c.stats.Expirations,
	}
}

// CleanExpired 清理过期条目
func (c *LRUCache) CleanExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	now := time.Now()

	// 从后往前遍历（最久未使用的在后面）
	for elem := c.order.Back(); elem != nil; {
		entry := elem.Value.(*Entry)
		prev := elem.Prev()

		if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
			c.removeElement(elem)
			c.recordExpiration()
			count++
		}

		elem = prev
	}

	return count
}

// evictOldest 淘汰最久未使用的条目
func (c *LRUCache) evictOldest() {
	elem := c.order.Back()
	if elem != nil {
		c.removeElement(elem)
		c.recordEviction()
	}
}

// removeElement 移除元素
func (c *LRUCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*Entry)
	delete(c.items, entry.Key)
	c.order.Remove(elem)
}

// 统计记录方法
func (c *LRUCache) recordHit() {
	c.stats.mu.Lock()
	c.stats.Hits++
	c.stats.mu.Unlock()
}

func (c *LRUCache) recordMiss() {
	c.stats.mu.Lock()
	c.stats.Misses++
	c.stats.mu.Unlock()
}

func (c *LRUCache) recordEviction() {
	c.stats.mu.Lock()
	c.stats.Evictions++
	c.stats.mu.Unlock()
}

func (c *LRUCache) recordExpiration() {
	c.stats.mu.Lock()
	c.stats.Expirations++
	c.stats.mu.Unlock()
}

// HitRate 计算命中率
func (s *Stats) HitRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}
