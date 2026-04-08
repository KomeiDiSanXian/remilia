// Package syncx 提供泛型并发数据结构，是标准库 sync 包的类型安全扩展。
//
// 目前提供：
//   - [Map]：基于 RWMutex 的泛型并发映射，API 对齐 sync.Map 并扩展 Compute 等方法。
//   - [Lazy]：基于 sync.Once 的泛型懒初始化容器。
package syncx

import "sync"

// Map 是基于 sync.RWMutex 的泛型并发映射。
//
// 相比 sync.Map，提供编译期类型安全（无需运行时类型断言），
// 并附加 [Map.Compute]、[Map.Len]、[Map.Has]、[Map.Clear] 等实用方法。
//
// 零值直接可用，无需显式初始化。Map 在首次使用后不得复制。
//
// 适用场景：
//   - 混合读写负载，通过 RWMutex 实现并发安全。
//   - 需要原子读-改-写操作（[Map.Compute]）。
//   - 消除 mu sync.RWMutex + map[K]V 模板样板代码。
//
// 示例：
//
//	var m syncx.Map[string, int]
//	m.Store("a", 1)
//	v, ok := m.Load("a")  // v=1, ok=true
//	m.Compute("a", func(old int, exists bool) (int, bool) {
//	    return old + 1, true  // 原子自增
//	})
type Map[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

// Store 存储键值对。
func (m *Map[K, V]) Store(key K, val V) {
	m.mu.Lock()
	m.initLocked()
	m.m[key] = val
	m.mu.Unlock()
}

// Load 返回 key 对应的值及是否存在。
func (m *Map[K, V]) Load(key K) (V, bool) {
	m.mu.RLock()
	v, ok := m.m[key]
	m.mu.RUnlock()
	return v, ok
}

// LoadOrStore 若 key 已存在则返回现有值（loaded=true），
// 否则存入 val 并返回 val（loaded=false）。
func (m *Map[K, V]) LoadOrStore(key K, val V) (actual V, loaded bool) {
	m.mu.Lock()
	m.initLocked()
	if v, ok := m.m[key]; ok {
		m.mu.Unlock()
		return v, true
	}
	m.m[key] = val
	m.mu.Unlock()
	return val, false
}

// Delete 删除 key；若 key 不存在则为空操作。
func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	delete(m.m, key)
	m.mu.Unlock()
}

// LoadAndDelete 删除 key 并返回删除前的值及是否存在。
func (m *Map[K, V]) LoadAndDelete(key K) (V, bool) {
	m.mu.Lock()
	v, ok := m.m[key]
	if ok {
		delete(m.m, key)
	}
	m.mu.Unlock()
	return v, ok
}

// Has 报告 key 是否存在。
func (m *Map[K, V]) Has(key K) bool {
	m.mu.RLock()
	_, ok := m.m[key]
	m.mu.RUnlock()
	return ok
}

// Len 返回当前条目数。
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	n := len(m.m)
	m.mu.RUnlock()
	return n
}

// Clear 删除所有键值对。
func (m *Map[K, V]) Clear() {
	m.mu.Lock()
	m.m = nil
	m.mu.Unlock()
}

// Range 遍历所有键值对，顺序不确定。若 fn 返回 false，提前终止迭代。
//
// Range 在调用时先对内部 map 做浅拷贝快照，随后释放读锁再迭代。
// 因此 fn 内部可以安全地调用 Store / Delete / Compute 等方法，不会死锁。
//
// 注意：由于快照语义，在 Range 执行期间对 Map 的修改不会反映到本次迭代中。
func (m *Map[K, V]) Range(fn func(key K, val V) bool) {
	m.mu.RLock()
	snapshot := make(map[K]V, len(m.m))
	for k, v := range m.m {
		snapshot[k] = v
	}
	m.mu.RUnlock()

	for k, v := range snapshot {
		if !fn(k, v) {
			break
		}
	}
}

// Compute 在持有写锁的情况下对 key 执行原子读-改-写操作。
//
// fn 接收当前值 old 及是否存在 exists，返回：
//   - (newVal, true)  → 将 newVal 写入 map
//   - (_, false)      → 从 map 删除 key（key 不存在时为空操作）
//
// 返回 fn 的返回值 (newVal, store)，供调用方检查最终状态。
// 若需在调用方获取额外结果，可在 fn 内通过闭包捕获变量。
func (m *Map[K, V]) Compute(key K, fn func(old V, exists bool) (newVal V, store bool)) (V, bool) {
	m.mu.Lock()
	m.initLocked()
	old, exists := m.m[key]
	newVal, store := fn(old, exists)
	if store {
		m.m[key] = newVal
	} else {
		delete(m.m, key)
	}
	m.mu.Unlock()
	return newVal, store
}

// initLocked 在持有写锁的情况下懒初始化内部 map。
func (m *Map[K, V]) initLocked() {
	if m.m == nil {
		m.m = make(map[K]V)
	}
}
