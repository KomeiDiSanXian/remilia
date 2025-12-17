package remilia

import (
	"testing"
)

// TestInstrumentedPoolHitRate 测试命中率计算
func TestInstrumentedPoolHitRate(t *testing.T) {
	pool := NewInstrumentedPool(func() interface{} {
		return &struct{}{}
	})

	// 情况1: 空池，命中率应该是 0%
	for i := 0; i < 10; i++ {
		pool.Get()
	}
	stats := pool.Stats()
	if stats.HitRate != 0 {
		t.Errorf("Expected 0%% hit rate for empty pool, got %.1f%%", stats.HitRate)
	}

	// 情况2: 放回后再获取，命中率应该提升
	for i := 0; i < 10; i++ {
		pool.Put(&struct{}{})
	}
	for i := 0; i < 10; i++ {
		pool.Get()
	}
	stats = pool.Stats()
	// (20-10)/20*100 = 50%
	expectedHitRate := 50.0
	if stats.HitRate < expectedHitRate-1 || stats.HitRate > expectedHitRate+1 {
		t.Errorf("Expected hit rate around %.1f%%, got %.1f%%", expectedHitRate, stats.HitRate)
	}
}
