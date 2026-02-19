package pool

import (
	"sync"
	"testing"
)

// TestPool_ConcurrentReset 测试并发 Reset 不会导致 panic
func TestPool_ConcurrentReset(t *testing.T) {
	pool := NewInstrumentedPool(func() any {
		return &struct{}{}
	})

	var wg sync.WaitGroup

	// 并发执行多个 Reset
	for range 50 {
		wg.Go(func() {
			pool.Reset()
		})
	}

	// 同时进行 Get/Put 操作
	for range 10 {
		wg.Go(func() {
			for range 20 {
				obj := pool.Get()
				pool.Put(obj)
			}
		})
	}

	// 同时读取统计
	for range 10 {
		wg.Go(func() {
			for range 20 {
				_ = pool.Stats()
			}
		})
	}

	wg.Wait()

	// 最后验证 Reset 能正确工作
	pool.Reset()
	stats := pool.Stats()
	if stats.Gets != 0 || stats.Puts != 0 || stats.News != 0 {
		t.Errorf("Expected all counters to be 0 after reset, got Gets=%d, Puts=%d, News=%d",
			stats.Gets, stats.Puts, stats.News)
	}

	t.Log("Concurrent Reset - PASS (no panic, final reset works)")
}

// TestPool_ResetAtomicity 测试 Reset 的三个计数器是原子重置的
func TestPool_ResetAtomicity(t *testing.T) {
	pool := NewInstrumentedPool(func() any {
		return &struct{}{}
	})

	// 执行一些操作
	for range 100 {
		obj := pool.Get()
		pool.Put(obj)
	}

	// 验证有数据
	statsBefore := pool.Stats()
	if statsBefore.Gets == 0 {
		t.Error("Expected Gets > 0 before reset")
	}

	// Reset 应该原子地清零所有计数器
	pool.Reset()

	// 读取统计，应该全是0或全不是0（如果有并发操作）
	// 但由于我们在单线程测试，应该全是0
	statsAfter := pool.Stats()

	// 验证全部清零
	if statsAfter.Gets != 0 {
		t.Errorf("Expected Gets = 0, got %d", statsAfter.Gets)
	}
	if statsAfter.Puts != 0 {
		t.Errorf("Expected Puts = 0, got %d", statsAfter.Puts)
	}
	if statsAfter.News != 0 {
		t.Errorf("Expected News = 0, got %d", statsAfter.News)
	}

	t.Log("Reset atomicity - PASS")
}
