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
	// 注意：sync.Pool 在 GC/调度下不保证 Put 一定会被后续 Get 命中，
	// 特别是在 -race 下调度更激进，因此不能断言精确命中率。
	for i := 0; i < 10; i++ {
		pool.Put(&struct{}{})
	}
	for i := 0; i < 10; i++ {
		pool.Get()
	}
	stats = pool.Stats()
	if stats.HitRate <= 0 {
		t.Errorf("Expected hit rate to improve after Put(), got %.1f%%", stats.HitRate)
	}
	if stats.HitRate > 100 {
		t.Errorf("HitRate should be within [0,100], got %.1f%%", stats.HitRate)
	}
}
