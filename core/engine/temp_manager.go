package engine

import (
	"container/heap"
	"context"
	"maps"
	"slices"
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
//
// Add/Remove 通过 COW 增量更新（仅替换受影响 eventType 的切片），
// CleanExpired/水位线清理等批量路径做全量重建。
type TempSnapshot struct {
	specific map[EventType][]*Matcher // 按 eventType 预归并、排序
	generic  []*Matcher               // eventType=="" 的结果
}

// withList 返回一个把 et 对应列表替换为 list 的新快照（COW：
// 其余 eventType 的切片与原快照共享；specific map 本身复制以保证
// 旧快照对无锁读者保持不可变）。list 为空时删除该键。
func (s *TempSnapshot) withList(et EventType, list []*Matcher) *TempSnapshot {
	ns := &TempSnapshot{generic: s.generic}
	if et == "" {
		ns.generic = list
		ns.specific = s.specific
		return ns
	}
	spec := make(map[EventType][]*Matcher, len(s.specific)+1)
	maps.Copy(spec, s.specific)
	if len(list) == 0 {
		delete(spec, et)
	} else {
		spec[et] = list
	}
	ns.specific = spec
	return ns
}

// snapshotList 返回快照中 et 对应的列表（et=="" 时为 generic）。
func (s *TempSnapshot) snapshotList(et EventType) []*Matcher {
	if et == "" {
		return s.generic
	}
	return s.specific[et]
}

// tempMatcherManager manage temporary matchers with sharding and optimized insertion
type tempMatcherManager struct {
	shards [tempMatcherShardCount]*tempMatcherShard
	config TempManagerConfig
	count  atomic.Int32 // 原子计数，避免频繁加锁统计

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
	newCount := m.count.Add(1)

	// 增量更新 RCU 快照：仅 COW 替换该 eventType 的切片，
	// 摆脱此前每次 Add 都全量收集 8 shard + 整体排序的 O(N log N) 写放大
	m.snapshotInsert(matcher)

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

// SetExpiration 在对应 shard 锁内写入 matcher 的 createdAt/expiresAt。
//
// 由 Matcher.SetTempWithTimeout 通过 Engine（tempExpirationSetter）调用：
//   - 两个字段的读取方（CleanExpired、过期堆比较、cleanToWatermark）都在 shard 锁内，
//     写入走同一把锁即可消除数据竞争；
//   - matcher 已由本管理器持有时（byID 命中，如 OnTemp 之后再设超时）同时补登过期堆，
//     修复"已是 temp 再设超时 → 永不入堆、永不过期"的泄漏。
//
// 重复调用（延长超时）会为同一 matcher 产生多个堆条目：CleanExpired 弹出时
// 以 matcher 的实时 expiresAt 为准做判定，旧条目不会导致提前删除，
// 只会让其后条目的清理时机被推迟到新的过期时间。
func (m *tempMatcherManager) SetExpiration(matcher *Matcher, createdAt, expiresAt time.Time) {
	shard := m.getShard(matcher)
	shard.mu.Lock()
	matcher.rt.createdAt = createdAt
	matcher.rt.expiresAt = expiresAt
	if _, ok := shard.byID[matcher]; ok && !expiresAt.IsZero() {
		heap.Push(shard.expiration, matcher)
	}
	shard.mu.Unlock()
}

// ForEach 对当前管理的所有临时 matcher 依次调用 fn。
//
// fn 在 shard 锁之外执行（fn 通常会获取 matcher 自身的锁，如 invalidateCombinedChain），
// 因此遍历到的集合是弱一致快照：期间新增/移除的 matcher 可能被包含或跳过。
// 供 Engine.Use/UseForGroup/ResetGroupMiddleware 失效临时 matcher 的中间件链缓存。
func (m *tempMatcherManager) ForEach(fn func(*Matcher)) {
	for i := range tempMatcherShardCount {
		shard := m.shards[i]
		shard.mu.RLock()
		batch := make([]*Matcher, 0, len(shard.byID))
		for matcher := range shard.byID {
			batch = append(batch, matcher)
		}
		shard.mu.RUnlock()
		for _, matcher := range batch {
			fn(matcher)
		}
	}
}

// waitCleanups 等待所有在途的水位线清理 goroutine 退出，或 ctx 结束。
// 由 Engine.Shutdown 调用，保证清理 goroutine 不在关闭后残留。
func (m *tempMatcherManager) waitCleanups(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		m.cleanupWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Count returns the number of temporary matchers
func (m *tempMatcherManager) Count() int {
	return int(m.count.Load())
}

// HasAny reports whether there are any temporary matchers registered.
//
// O(1) — single atomic load, avoids the 8-shard RLock/map-lookup path of Get.
// Use this as a fast-path guard before calling Get to skip the shard scan
// entirely when temp matchers are absent (the common case in production bots).
func (m *tempMatcherManager) HasAny() bool {
	return m.count.Load() > 0
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
		m.count.Add(-1)
		// 增量更新 RCU 快照（COW 移除，见 snapshotRemove）
		m.snapshotRemove(matcher)
	} else {
		shard.mu.Unlock()
	}
}

// snapshotInsert 把 matcher 按优先级 COW 插入快照中对应 eventType 的列表。
//
// 一致性协议：在 snapMu 内以 shard.byID 为准做二次校验——若 matcher 已被
// 并发 Remove（shard 侧先删），放弃插入，避免快照残留"幽灵" matcher。
// snapshotInsert/snapshotRemove/rebuildSnapshot 都遵循 snapMu → shard.mu
// 的加锁顺序，与 Add/Remove（先释放 shard 锁再进 snapMu）无死锁交叉。
func (m *tempMatcherManager) snapshotInsert(matcher *Matcher) {
	m.snapMu.Lock()
	defer m.snapMu.Unlock()

	shard := m.getShard(matcher)
	shard.mu.RLock()
	_, present := shard.byID[matcher]
	shard.mu.RUnlock()
	if !present {
		return // 已被并发 Remove，快照不应包含它
	}

	old := m.snapshot.Load()
	et := matcher.EventType
	oldList := old.snapshotList(et)

	// 去重：与 CleanExpired 等全量重建并发时，重建可能已把本 matcher
	// 收入快照（shard 侧先插入完成），此处再插会造成同事件双重执行
	if slices.Contains(oldList, matcher) {
		return
	}

	// 有序插入（稳定：等优先级插在已有元素之后），与 shard 内插入规则一致
	prio := matcher.getPriority()
	idx := sort.Search(len(oldList), func(i int) bool {
		return oldList[i].getPriority() > prio
	})
	newList := make([]*Matcher, 0, len(oldList)+1)
	newList = append(newList, oldList[:idx]...)
	newList = append(newList, matcher)
	newList = append(newList, oldList[idx:]...)

	m.snapshot.Store(old.withList(et, newList))
}

// snapshotRemove 从快照中 COW 移除 matcher（幂等：不存在时为空操作）。
func (m *tempMatcherManager) snapshotRemove(matcher *Matcher) {
	m.snapMu.Lock()
	defer m.snapMu.Unlock()

	old := m.snapshot.Load()
	et := matcher.EventType
	oldList := old.snapshotList(et)

	idx := -1
	for i, v := range oldList {
		if v == matcher {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	newList := make([]*Matcher, 0, len(oldList)-1)
	newList = append(newList, oldList[:idx]...)
	newList = append(newList, oldList[idx+1:]...)

	m.snapshot.Store(old.withList(et, newList))
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

// CleanExpired removes expired matchers and returns them.
//
// 被移除的 matcher 会统一在此标记 deleted（所有清理路径行为一致），
// 且在有条目被移除时重建 RCU 快照——否则周期清理路径的快照会继续
// 保留已移除的 matcher（此前只有 Add/Remove 会重建快照）。
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

			// If expired or deleted.
			// 注意：判定使用 matcher 的实时 expiresAt（shard 锁内读取），
			// 因此 SetExpiration 延长超时后残留的旧堆条目不会导致提前删除。
			if matcher.rt.deleted.Load() || (!matcher.rt.expiresAt.IsZero() && now.After(matcher.rt.expiresAt)) {
				heap.Pop(shard.expiration)

				// Verify services still in this shard and in index before removal
				// (Handling race where it might have been removed concurrently?
				// Lock protects us, but deleted flag might be set by Remove)
				if _, ok := shard.byID[matcher]; ok {
					m.removeLocked(shard, matcher)
					matcher.rt.deleted.Store(true)
					expired = append(expired, matcher)
					m.count.Add(-1)
				}
			} else {
				break
			}
		}
		shard.mu.Unlock()
	}

	if len(expired) > 0 {
		m.snapMu.Lock()
		m.rebuildSnapshot()
		m.snapMu.Unlock()
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

		// 删除最旧的（同样标记 deleted，防止外部持引用者把已清理的 matcher 当作存活）
		for _, matcher := range matchers {
			if removed >= toRemove {
				break
			}
			m.removeLocked(shard, matcher)
			matcher.rt.deleted.Store(true)
			m.count.Add(-1)
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
