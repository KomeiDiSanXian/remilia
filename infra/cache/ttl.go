// Package cache 提供框架级通用缓存工具。
//
// 目前包含：
//   - [Map]：泛型 TTL Map，每个键独立过期，支持后台自动 GC。
//
// 与 [hashicorp/golang-lru] 中 Expirable LRU 的区别：
//   - LRU 基于**容量**淘汰（容量满时逐出最久未使用的条目）
//   - [Map] 仅基于**时间**淘汰，容量无上限，适合"缓存第三方 API 响应、
//     短期会话状态、推送去重令牌"等场景
//
// # 快速开始
//
//	// 创建 TTL Map，后台每 5 分钟执行一次 GC
//	m := cache.New[string, *UserSession](5 * time.Minute)
//	defer m.Stop()
//
//	m.Set("user-123", &UserSession{Name: "Alice"}, 30*time.Minute)
//
//	if sess, ok := m.Get("user-123"); ok {
//	    fmt.Println(sess.Name) // Alice
//	}
//
//	// 禁用后台 GC，手动触发
//	m2 := cache.New[string, int](0)
//	defer m2.Stop()
//	m2.Set("k", 1, time.Second)
//	removed := m2.GC() // 手动清理过期条目
package cache

import (
	"sync"
	"time"
)

// ttlEntry 内部存储单元，持有值和绝对到期时间。
type ttlEntry[V any] struct {
	val      V
	deadline time.Time
}

// Map 是泛型并发安全的 TTL Map，每个键独立过期。
//
// 过期策略（惰性 + 定期）：
//   - 惰性失效：[Map.Get] 时检查 deadline，过期则返回零值 + false，但不立即删除条目。
//   - 定期回收：后台 GC goroutine（或手动调用 [Map.GC]）扫描并删除过期条目，释放内存。
//
// 创建方式（推荐通过 [New] 而非直接 struct literal）：
//
//	m := cache.New[string, int](5 * time.Minute) // 后台每 5 分钟 GC
//	m := cache.New[string, int](0)               // 禁用后台 GC，手动调用 m.GC()
type Map[K comparable, V any] struct {
	mu       sync.RWMutex
	entries  map[K]*ttlEntry[V]
	stopCh   chan struct{}
	stopOnce sync.Once
}

// New 创建一个 TTL Map。
//
// gcInterval 为后台 GC goroutine 的运行间隔；传入 0 则禁用后台 GC（需手动调用 [Map.GC]）。
//
// # 后台 goroutine 说明（重要）
//
// 当 gcInterval > 0 时，New 会在构造时启动一个后台 GC goroutine。
// 调用方必须在使用结束后调用 [Map.Stop] 以停止该 goroutine，否则会导致 goroutine 泄漏。
// 推荐配合 defer 使用：
//
//	m := cache.New[string, int](5 * time.Minute)
//	defer m.Stop()
//
// 在单元测试中，推荐传入 0 禁用后台 GC 以避免 goroutine 泄漏：
//
//	m := cache.New[string, int](0) // 测试中禁用后台 GC
//	m.GC()                         // 手动触发 GC
func New[K comparable, V any](gcInterval time.Duration) *Map[K, V] {
	m := &Map[K, V]{
		entries: make(map[K]*ttlEntry[V]),
		stopCh:  make(chan struct{}),
	}
	if gcInterval > 0 {
		go m.gcLoop(gcInterval)
	}
	return m
}

// gcLoop 后台定期执行 GC 的 goroutine。
func (m *Map[K, V]) gcLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.GC()
		case <-m.stopCh:
			return
		}
	}
}

// Set 存储键值对及其 TTL 过期时间。
//
// 若 key 已存在，原有记录（含到期时间）会被完整覆盖。
// ttl 必须大于 0；若传入 0 或负值，条目将被视为立即过期。
func (m *Map[K, V]) Set(key K, val V, ttl time.Duration) {
	m.mu.Lock()
	m.entries[key] = &ttlEntry[V]{val: val, deadline: time.Now().Add(ttl)}
	m.mu.Unlock()
}

// Get 获取键对应的值。
//
// 若 key 不存在或已过期，返回零值和 false。
// 过期判断使用 >= 语义（deadline == now 视为已过期），因此 TTL=0 的条目在 Set 后立即过期。
// 过期 key 在此方法中不会从 map 删除（惰性失效），实际删除由 [Map.GC] 完成。
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	e, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok || !time.Now().Before(e.deadline) { // now >= deadline 视为已过期
		var zero V
		return zero, false
	}
	return e.val, true
}

// GetWithTTL 获取键对应的值及其剩余有效时间。
//
// 若 key 不存在或已过期，返回零值、0 和 false。
// remaining 为剩余有效时间（精确到纳秒），即 deadline - now。
func (m *Map[K, V]) GetWithTTL(key K) (V, time.Duration, bool) {
	m.mu.RLock()
	e, exists := m.entries[key]
	m.mu.RUnlock()
	var zero V
	if !exists {
		return zero, 0, false
	}
	remaining := time.Until(e.deadline)
	if remaining <= 0 {
		return zero, 0, false
	}
	return e.val, remaining, true
}

// Has 报告键是否存在且未过期。等价于 _, ok := m.Get(key)。
func (m *Map[K, V]) Has(key K) bool {
	_, ok := m.Get(key)
	return ok
}

// Delete 立即删除指定键（无论是否过期）。
// 若 key 不存在，此方法为空操作。
func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	delete(m.entries, key)
	m.mu.Unlock()
}

// Len 返回当前**有效**（未过期）条目数量。
//
// 此操作需要遍历所有条目，时间复杂度 O(n)，
// 包含已惰性失效但尚未被 GC 回收的过期条目不计入结果。
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, e := range m.entries {
		if now.Before(e.deadline) { // now < deadline → 未过期（与 Get 语义一致）
			count++
		}
	}
	return count
}

// Cap 返回内部 map 中所有条目数量（含已过期但未被 GC 的条目）。
//
// 比 [Map.Len] 更快（O(1)），适合用于粗略估算内存占用。
func (m *Map[K, V]) Cap() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// GC 扫描并删除所有已过期条目，返回清理的条目数量。
//
// 后台 GC goroutine 会自动周期性调用；也可在内存敏感场景下手动触发。
//
// 过期判断使用 >= 语义（deadline == now 视为已过期），与 [Map.Get] 保持一致。
func (m *Map[K, V]) GC() int {
	// 读锁下收集过期键，不阻塞并发的 Get/Set
	now := time.Now()
	m.mu.RLock()
	var toDelete []K
	for k, e := range m.entries {
		if !now.Before(e.deadline) {
			toDelete = append(toDelete, k)
		}
	}
	m.mu.RUnlock()

	if len(toDelete) == 0 {
		return 0
	}

	// 写锁下批量删除，二次校验防止误删已被 Set 刷新的条目
	now = time.Now()
	m.mu.Lock()
	removed := 0
	for _, k := range toDelete {
		if e, ok := m.entries[k]; ok && !now.Before(e.deadline) {
			delete(m.entries, k)
			removed++
		}
	}
	m.mu.Unlock()

	return removed
}

// Flush 清空所有条目（无论是否过期），不影响后台 GC goroutine 的运行状态。
func (m *Map[K, V]) Flush() {
	m.mu.Lock()
	m.entries = make(map[K]*ttlEntry[V])
	m.mu.Unlock()
}

// Keys 返回当前所有有效（未过期）键的快照切片。
//
// 注意：返回值是调用时刻的快照，不保证后续访问时仍有效。
func (m *Map[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	out := make([]K, 0, len(m.entries))
	for k, e := range m.entries {
		if now.Before(e.deadline) { // now < deadline → 未过期
			out = append(out, k)
		}
	}
	return out
}

// Range 遍历所有有效（未过期）的键值对。
//
// fn 返回 false 时停止遍历。遍历期间持有读锁，fn 内不可调用 Set/Delete/GC/Flush。
func (m *Map[K, V]) Range(fn func(key K, val V, remaining time.Duration) bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	for k, e := range m.entries {
		rem := e.deadline.Sub(now)
		if rem <= 0 {
			continue
		}
		if !fn(k, e.val, rem) {
			return
		}
	}
}

// Stop 停止后台 GC goroutine（幂等，多次调用安全）。
//
// 调用后 Map 本身仍可正常使用（Get/Set/Delete 不受影响），
// 只是不再自动执行 GC，需要时可手动调用 [Map.GC]。
//
// 通常配合 defer 使用：
//
//	m := cache.New[string, int](5 * time.Minute)
//	defer m.Stop()
func (m *Map[K, V]) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}
