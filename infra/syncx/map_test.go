package syncx

import (
	"sync"
	"testing"
)

func TestMap_StoreLoad(t *testing.T) {
	var m Map[string, int]

	m.Store("a", 1)
	v, ok := m.Load("a")
	if !ok || v != 1 {
		t.Fatalf("expected (1, true), got (%d, %v)", v, ok)
	}

	_, ok = m.Load("missing")
	if ok {
		t.Fatal("expected missing key to return ok=false")
	}
}

func TestMap_ZeroValue(t *testing.T) {
	// 零值 Map 应直接可用
	var m Map[int, string]
	m.Store(1, "hello")
	v, ok := m.Load(1)
	if !ok || v != "hello" {
		t.Fatalf("got (%q, %v)", v, ok)
	}
}

func TestMap_LoadOrStore(t *testing.T) {
	var m Map[string, int]

	actual, loaded := m.LoadOrStore("k", 10)
	if loaded || actual != 10 {
		t.Fatalf("first LoadOrStore: expected (10, false), got (%d, %v)", actual, loaded)
	}

	actual, loaded = m.LoadOrStore("k", 99)
	if !loaded || actual != 10 {
		t.Fatalf("second LoadOrStore: expected (10, true), got (%d, %v)", actual, loaded)
	}
}

func TestMap_Delete(t *testing.T) {
	var m Map[string, int]
	m.Store("x", 42)
	m.Delete("x")

	_, ok := m.Load("x")
	if ok {
		t.Fatal("key should have been deleted")
	}

	// 删除不存在的 key 不应 panic
	m.Delete("not-exist")
}

func TestMap_LoadAndDelete(t *testing.T) {
	var m Map[string, int]
	m.Store("y", 7)

	v, ok := m.LoadAndDelete("y")
	if !ok || v != 7 {
		t.Fatalf("expected (7, true), got (%d, %v)", v, ok)
	}

	_, ok = m.LoadAndDelete("y")
	if ok {
		t.Fatal("second LoadAndDelete should return ok=false")
	}
}

func TestMap_Has(t *testing.T) {
	var m Map[string, bool]
	if m.Has("k") {
		t.Fatal("empty map should not have any key")
	}
	m.Store("k", true)
	if !m.Has("k") {
		t.Fatal("key should exist after Store")
	}
	m.Delete("k")
	if m.Has("k") {
		t.Fatal("key should be gone after Delete")
	}
}

func TestMap_Len(t *testing.T) {
	var m Map[int, int]
	if m.Len() != 0 {
		t.Fatal("new map should have len 0")
	}
	m.Store(1, 1)
	m.Store(2, 2)
	if m.Len() != 2 {
		t.Fatalf("expected len 2, got %d", m.Len())
	}
	m.Delete(1)
	if m.Len() != 1 {
		t.Fatalf("expected len 1 after delete, got %d", m.Len())
	}
}

func TestMap_Clear(t *testing.T) {
	var m Map[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Clear()
	if m.Len() != 0 {
		t.Fatalf("expected len 0 after Clear, got %d", m.Len())
	}
	// 清空后仍可继续使用
	m.Store("c", 3)
	if m.Len() != 1 {
		t.Fatalf("expected len 1 after re-use, got %d", m.Len())
	}
}

func TestMap_Range(t *testing.T) {
	var m Map[string, int]
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)

	sum := 0
	m.Range(func(k string, v int) bool {
		sum += v
		return true
	})
	if sum != 6 {
		t.Fatalf("expected sum 6, got %d", sum)
	}

	// Range 内部修改 Map 不应死锁
	m.Range(func(k string, v int) bool {
		m.Store(k+"_copy", v*10) // 内部可安全调用 Store
		return true
	})
}

func TestMap_RangeEarlyExit(t *testing.T) {
	var m Map[int, int]
	for i := range 10 {
		m.Store(i, i)
	}

	count := 0
	m.Range(func(k, v int) bool {
		count++
		return count < 3 // 迭代 3 个后停止
	})
	if count != 3 {
		t.Fatalf("expected 3 iterations, got %d", count)
	}
}

func TestMap_Compute_Store(t *testing.T) {
	var m Map[string, int]

	// 不存在时写入
	newVal, stored := m.Compute("counter", func(old int, exists bool) (int, bool) {
		return old + 1, true
	})
	if !stored || newVal != 1 {
		t.Fatalf("expected (1, true), got (%d, %v)", newVal, stored)
	}

	// 存在时自增
	newVal, stored = m.Compute("counter", func(old int, exists bool) (int, bool) {
		return old + 1, true
	})
	if !stored || newVal != 2 {
		t.Fatalf("expected (2, true), got (%d, %v)", newVal, stored)
	}
}

func TestMap_Compute_Delete(t *testing.T) {
	var m Map[string, int]
	m.Store("k", 42)

	// 返回 store=false 应删除 key
	_, stored := m.Compute("k", func(old int, exists bool) (int, bool) {
		return 0, false
	})
	if stored {
		t.Fatal("expected store=false (delete)")
	}
	if m.Has("k") {
		t.Fatal("key should have been deleted by Compute")
	}

	// 对不存在的 key 返回 store=false 是空操作，不应 panic
	_, stored = m.Compute("not-exist", func(old int, exists bool) (int, bool) {
		return 0, false
	})
	if stored {
		t.Fatal("expected store=false for non-existent key")
	}
}

func TestMap_Compute_Closure(t *testing.T) {
	var m Map[string, int]
	m.Store("v", 100)

	var captured int
	m.Compute("v", func(old int, exists bool) (int, bool) {
		captured = old
		return old, true
	})
	if captured != 100 {
		t.Fatalf("closure should capture 100, got %d", captured)
	}
}

func TestMap_Concurrent(t *testing.T) {
	var m Map[int, int]
	const goroutines = 100
	const ops = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range ops {
				key := j % 10
				m.Store(key, id*ops+j)
				m.Load(key)
				m.Has(key)
				m.Compute(key, func(old int, exists bool) (int, bool) {
					return old + 1, true
				})
			}
		}(i)
	}
	wg.Wait()
}

func TestMap_RangeModifyConcurrent(t *testing.T) {
	// Range 拍快照后释放锁，fn 内 Store 不应死锁
	var m Map[string, int]
	m.Store("x", 1)

	done := make(chan struct{})
	go func() {
		m.Range(func(k string, v int) bool {
			m.Store("y", 2) // 内部修改应能获取锁
			return true
		})
		close(done)
	}()
	<-done
}
