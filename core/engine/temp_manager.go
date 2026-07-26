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

// FNV-1a hash constants
const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

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

// tempMatcherShard 持有一部分临时匹配器
type tempMatcherShard struct {
	mu           sync.RWMutex
	matcherIndex map[EventType][]*Matcher // 按优先级排序
	expiration   *matcherHeap             // 用于过期的最小堆
	byID         map[*Matcher]struct{}    // 快速存在性检查
}

func newTempMatcherShard() *tempMatcherShard {
	return &tempMatcherShard{
		matcherIndex: make(map[EventType][]*Matcher),
		expiration:   &matcherHeap{},
		byID:         make(map[*Matcher]struct{}),
	}
}

// TempSnapshot 是 TempManager 的只读一致性视图。
// 在每次 Add/Remove/CleanExpired 后原子替换。
type TempSnapshot struct {
	specific map[EventType][]*Matcher // 按 eventType 预归并、排序
	generic  []*Matcher               // eventType=="" 的结果
}

// tempMatcherManager manage temporary matchers with sharding and optimized insertion
type tempMatcherManager struct {
	shards [tempMatcherShardCount]*tempMatcherShard
	config TempManagerConfig
	count  int32 // 原子计数，避免频繁加锁统计

	snapshot atomic.Pointer[TempSnapshot] // RCU 只读快照
	snapMu   sync.Mutex                   // 保护 rebuildSnapshot

	cleanupWg sync.WaitGroup // 追踪水位线清理 goroutine，供 Shutdown 等待
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
	// 初始化空快照
	tm.snapshot.Store(&TempSnapshot{
		specific: make(map[EventType][]*Matcher),
	})
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
	hash := uint64(fnvOffset64)
	// Hash the pointer value byte by byte
	for i := range 8 {
		hash ^= uint64((ptr >> (i * 8)) & 0xFF)
		hash *= fnvPrime64
	}
	return uintptr(hash)
}

// Add adds a temp matcher using insertion sort (O(N)) and sharded lock
func (m *tempMatcherManager) Add(matcher *Matcher) {
	shard := m.getShard(matcher)
	shard.mu.Lock()

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
	shard.mu.Unlock()

	// 增加计数
	newCount := atomic.AddInt32(&m.count, 1)

	// 重建 RCU 快照（shard 锁已释放，不与 rebuildSnapshot 的 RLock 冲突）
	m.snapMu.Lock()
	m.rebuildSnapshot()
	m.snapMu.Unlock()

	// 水位线清理：计数超过高水位时触发过期清理。
	// rebuildSnapshot 在 Add 的 snapMu 段内执行，不与 shard 锁交叉，
	// 避免 lock ordering deadlock。
	if m.config.EnableAdaptiveCleanup && int(newCount) >= m.config.WatermarkHigh {
		m.cleanupWg.Go(func() {
			m.cleanToWatermark()
			m.snapMu.Lock()
			m.rebuildSnapshot()
			m.snapMu.Unlock()
		})
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

	// Check if it exists before removing
	if _, exists := shard.byID[matcher]; exists {
		m.removeLocked(shard, matcher)
		shard.mu.Unlock()
		atomic.AddInt32(&m.count, -1)
		m.snapMu.Lock()
		m.rebuildSnapshot()
		m.snapMu.Unlock()
	} else {
		shard.mu.Unlock()
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

// Get returns pre-merged, sorted matchers for an event type.
//
// RCU 读路径：O(1) atomic.LoadPointer，零分配。
// 快照由 rebuildSnapshot 在每次写入后原子替换。
func (m *tempMatcherManager) Get(eventType EventType) []*Matcher {
	snap := m.snapshot.Load()
	if snap == nil {
		return nil
	}
	if eventType == "" {
		return snap.generic
	}
	return snap.specific[eventType]
}

// rebuildSnapshot 从所有 shard 收集 matcher，构建只读快照。
//
// 写路径：在每次 Add/Remove/CleanExpired 后调用。
// snapMu 保证同一时间只有一个 rebuild 在执行。
func (m *tempMatcherManager) rebuildSnapshot() {
	// Collect from all shards
	allByType := make(map[EventType][]*Matcher)

	for i := range tempMatcherShardCount {
		shard := m.shards[i]
		shard.mu.RLock()
		for et, list := range shard.matcherIndex {
			if len(list) > 0 {
				dst := make([]*Matcher, len(list))
				copy(dst, list)
				allByType[et] = append(allByType[et], dst...)
			}
		}
		shard.mu.RUnlock()
	}

	snap := &TempSnapshot{
		specific: make(map[EventType][]*Matcher, len(allByType)),
	}
	for et, list := range allByType {
		sort.Slice(list, func(i, j int) bool {
			return list[i].getPriority() < list[j].getPriority()
		})
		if et == "" {
			snap.generic = list
		} else {
			snap.specific[et] = list
		}
	}

	m.snapshot.Store(snap)
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
