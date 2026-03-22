package engine

import (
	"container/heap"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const tempMatcherShardCount = 8

// TempManagerConfig 临时匹配器管理器配置
type TempManagerConfig struct {
	// WatermarkHigh 高水位线，达到后触发清理
	WatermarkHigh int

	// WatermarkLow 低水位线，清理到这个数量
	WatermarkLow int

	// CleanupInterval 定期清理间隔
	CleanupInterval time.Duration

	// EnableAdaptiveCleanup 启用自适应清理
	EnableAdaptiveCleanup bool
}

// DefaultTempManagerConfig 返回默认配置
func DefaultTempManagerConfig() TempManagerConfig {
	return TempManagerConfig{
		WatermarkHigh:         10000, // 10K 匹配器触发清理
		WatermarkLow:          8000,  // 清理到 8K
		CleanupInterval:       30 * time.Second,
		EnableAdaptiveCleanup: true,
	}
}

// tempMatcherShard holds a subset of temp matchers
type tempMatcherShard struct {
	mu           sync.RWMutex
	matcherIndex map[EventType][]*Matcher // Sorted by priority
	expiration   *matcherHeap             // Min-heap for expiration
	byID         map[*Matcher]struct{}    // Fast existence check
}

func newTempMatcherShard() *tempMatcherShard {
	return &tempMatcherShard{
		matcherIndex: make(map[EventType][]*Matcher),
		expiration:   &matcherHeap{},
		byID:         make(map[*Matcher]struct{}),
	}
}

// tempMatcherManager manage temporary matchers with sharding and optimized insertion
type tempMatcherManager struct {
	shards [tempMatcherShardCount]*tempMatcherShard
	config TempManagerConfig
	count  int32 // 原子计数，避免频繁加锁统计
}

func newTempMatcherManager() *tempMatcherManager {
	return newTempMatcherManagerWithConfig(DefaultTempManagerConfig())
}

func newTempMatcherManagerWithConfig(config TempManagerConfig) *tempMatcherManager {
	tm := &tempMatcherManager{
		config: config,
	}
	for i := range tempMatcherShardCount {
		tm.shards[i] = newTempMatcherShard()
	}
	return tm
}

func (m *tempMatcherManager) getShard(matcher *Matcher) *tempMatcherShard {
	// Use FNV-1a hash for better distribution
	// This avoids potential clustering from Go's memory allocator
	ptr := uintptr(unsafe.Pointer(matcher))
	hash := hashPtr(ptr)
	idx := hash % tempMatcherShardCount
	return m.shards[idx]
}

// hashPtr implements FNV-1a hash for pointer values
func hashPtr(ptr uintptr) uintptr {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	hash := uint64(offset64)
	// Hash the pointer value byte by byte
	for i := range 8 {
		hash ^= uint64((ptr >> (i * 8)) & 0xFF)
		hash *= prime64
	}
	return uintptr(hash)
}

// Add adds a temp matcher using insertion sort (O(N)) and sharded lock
func (m *tempMatcherManager) Add(matcher *Matcher) {
	shard := m.getShard(matcher)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Add to index with Insertion Sort
	list := shard.matcherIndex[matcher.EventType]
	priority := matcher.getPriority()

	// Find insertion point to maintain stable sort (insert after items with <= priority)
	// We want list[i] <= list[i+1]. Stable: if equal, new one comes after old ones.
	// So we need to find first index where list[i].priority > priority.

	// Optimization: Check if it should be appended (common case, appending default priority)
	insertIdx := len(list)
	if len(list) > 0 && list[len(list)-1].getPriority() <= priority {
		// Appending is O(1)
	} else {
		// Binary search for insertion point
		insertIdx = sort.Search(len(list), func(i int) bool {
			return list[i].getPriority() > priority
		})
	}

	// Insert
	if insertIdx == len(list) {
		list = append(list, matcher)
	} else {
		// Grow capacity
		list = append(list, nil)
		// Shift elements to the right
		copy(list[insertIdx+1:], list[insertIdx:])
		list[insertIdx] = matcher
	}
	shard.matcherIndex[matcher.EventType] = list

	// Add to heap if it has expiration
	if !matcher.rt.expiresAt.IsZero() {
		heap.Push(shard.expiration, matcher)
	}

	shard.byID[matcher] = struct{}{}

	// 增加计数
	newCount := atomic.AddInt32(&m.count, 1)

	// 检查是否触发水位线清理
	if m.config.EnableAdaptiveCleanup && int(newCount) >= m.config.WatermarkHigh {
		// 释放锁后触发清理（避免死锁）
		go m.cleanToWatermark()
	}
}

// Count returns the number of temporary matchers
func (m *tempMatcherManager) Count() int {
	return int(atomic.LoadInt32(&m.count))
}

// HasAny reports whether there are any temporary matchers registered.
//
// O(1) — single atomic load, avoids the 8-shard RLock/map-lookup path of Get.
// Use this as a fast-path guard before calling Get to skip the shard scan
// entirely when temp matchers are absent (the common case in production bots).
func (m *tempMatcherManager) HasAny() bool {
	return atomic.LoadInt32(&m.count) > 0
}

// CountAccurate returns accurate count by scanning all shards (slower)
func (m *tempMatcherManager) CountAccurate() int {
	count := 0
	for i := range tempMatcherShardCount {
		shard := m.shards[i]
		shard.mu.RLock()
		count += len(shard.byID)
		shard.mu.RUnlock()
	}
	return count
}

// Remove removes a temp matcher
func (m *tempMatcherManager) Remove(matcher *Matcher) {
	shard := m.getShard(matcher)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Check if it exists before removing
	if _, exists := shard.byID[matcher]; exists {
		m.removeLocked(shard, matcher)
		atomic.AddInt32(&m.count, -1)
	}
}

// removeLocked removes matcher assuming lock is held
func (m *tempMatcherManager) removeLocked(shard *tempMatcherShard, matcher *Matcher) {
	if _, ok := shard.byID[matcher]; !ok {
		return
	}
	delete(shard.byID, matcher)

	list := shard.matcherIndex[matcher.EventType]
	for i, v := range list {
		if v == matcher {
			// Delete preserving order
			copy(list[i:], list[i+1:])
			list[len(list)-1] = nil
			shard.matcherIndex[matcher.EventType] = list[:len(list)-1]
			break
		}
	}
	// Lazy delete from heap (handled in CleanExpired)
}

// Get returns sorted matchers for an event type
// Consolidates results from all shards
func (m *tempMatcherManager) Get(eventType EventType) []*Matcher {
	// Collect from all shards
	lists := make([][]*Matcher, 0, tempMatcherShardCount)
	totalLen := 0

	// Lock one by one and copy list to avoid holding all locks
	for i := range tempMatcherShardCount {
		shard := m.shards[i]
		shard.mu.RLock()
		src := shard.matcherIndex[eventType]
		if len(src) > 0 {
			// Copy to avoid race after unlock
			dst := make([]*Matcher, len(src))
			copy(dst, src)
			lists = append(lists, dst)
			totalLen += len(src)
		}
		shard.mu.RUnlock()
	}

	if totalLen == 0 {
		return nil
	}

	// Merge K sorted lists
	return mergeKLists(lists, totalLen)
}

// mergeKLists merges multiple sorted matcher lists into one
func mergeKLists(lists [][]*Matcher, totalLen int) []*Matcher {
	res := make([]*Matcher, 0, totalLen)
	indices := make([]int, len(lists))

	// Since K is small (8), linear scan for min is efficient enough
	for {
		minP := uint(999999999)
		winner := -1

		for k, list := range lists {
			if indices[k] < len(list) {
				p := list[indices[k]].getPriority()
				// Stable sort: if priorities are equal, we should strictly speaking preserve order based on... shards?
				// Since shards are random, order between shards is arbitrary but consistent.
				// We pick the first one encountered (lowest k).
				if p < minP {
					minP = p
					winner = k
				}
			}
		}

		if winner == -1 {
			break
		}

		res = append(res, lists[winner][indices[winner]])
		indices[winner]++
	}
	return res
}

// CleanExpired removes expired matchers and returns them
func (m *tempMatcherManager) CleanExpired() []*Matcher {
	var expired []*Matcher
	now := time.Now()

	// Iterate all shards
	for i := range tempMatcherShardCount {
		shard := m.shards[i]
		shard.mu.Lock()

		for shard.expiration.Len() > 0 {
			// Peek first
			matcher := (*shard.expiration)[0]

			// If expired or deleted
			if matcher.rt.deleted.Load() || (!matcher.rt.expiresAt.IsZero() && now.After(matcher.rt.expiresAt)) {
				heap.Pop(shard.expiration)

				// Verify services still in this shard and in index before removal
				// (Handling race where it might have been removed concurrently?
				// Lock protects us, but deleted flag might be set by Remove)
				if _, ok := shard.byID[matcher]; ok {
					m.removeLocked(shard, matcher)
					expired = append(expired, matcher)
					atomic.AddInt32(&m.count, -1)
				}
			} else {
				break
			}
		}
		shard.mu.Unlock()
	}
	return expired
}

// cleanToWatermark 清理到低水位线
func (m *tempMatcherManager) cleanToWatermark() {
	currentCount := m.Count()
	if currentCount <= m.config.WatermarkLow {
		return
	}

	// 先清理过期的
	_ = m.CleanExpired() // 忽略返回值，只关心清理动作

	currentCount = m.Count()
	if currentCount <= m.config.WatermarkLow {
		return
	}

	// 如果还是超过低水位线，清理最旧的一些（FIFO）
	toRemove := currentCount - m.config.WatermarkLow
	removed := 0

	for i := 0; i < tempMatcherShardCount && removed < toRemove; i++ {
		shard := m.shards[i]
		shard.mu.Lock()

		// 收集所有 matcher 并按创建时间排序
		matchers := make([]*Matcher, 0, len(shard.byID))
		for matcher := range shard.byID {
			matchers = append(matchers, matcher)
		}

		// 按创建时间排序（最旧的在前）
		sort.Slice(matchers, func(i, j int) bool {
			return matchers[i].rt.createdAt.Before(matchers[j].rt.createdAt)
		})

		// 删除最旧的
		for _, matcher := range matchers {
			if removed >= toRemove {
				break
			}
			m.removeLocked(shard, matcher)
			atomic.AddInt32(&m.count, -1)
			removed++
		}

		shard.mu.Unlock()
	}
}

// GetStats 获取临时匹配器统计信息
func (m *tempMatcherManager) GetStats() TempManagerStats {
	return TempManagerStats{
		Count:         m.Count(),
		WatermarkHigh: m.config.WatermarkHigh,
		WatermarkLow:  m.config.WatermarkLow,
	}
}

// TempManagerStats 临时匹配器管理器统计信息
type TempManagerStats struct {
	Count         int
	WatermarkHigh int
	WatermarkLow  int
}

// matcherHeap implements heap.Interface
type matcherHeap []*Matcher

func (h matcherHeap) Len() int { return len(h) }
func (h matcherHeap) Less(i, j int) bool {
	// Min-heap based on expiresAt
	return h[i].rt.expiresAt.Before(h[j].rt.expiresAt)
}
func (h matcherHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *matcherHeap) Push(x any) {
	*h = append(*h, x.(*Matcher))
}

func (h *matcherHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
