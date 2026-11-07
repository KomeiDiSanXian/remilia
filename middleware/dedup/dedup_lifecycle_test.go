package dedup

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDedupFilter_WithContext_StopsOnContextCancel 验证修复 #2：parent ctx 取消时 cleanup goroutine 退出
func TestDedupFilter_WithContext_StopsOnContextCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		cfg := DedupConfig{
			MaxSize:         100,
			DefaultTTL:      1 * time.Second,
			CleanupInterval: 50 * time.Millisecond,
		}
		filter := NewDedupFilterWithContext(ctx, cfg)
		require.NotNil(t, filter)

		isDup, err := filter.CheckDuplicate("event-1")
		assert.NoError(t, err)
		assert.False(t, isDup, "First check should not be duplicate")

		isDup2, _ := filter.CheckDuplicate("event-1")
		assert.True(t, isDup2, "Second check should be duplicate")

		cancel()
		synctest.Wait()

		stats := filter.GetStats()
		assert.Equal(t, 100, stats["max_size"])
	})
}

// TestDedupFilter_NewDedupFilter_BackwardCompatible 确认 NewDedupFilter 向后兼容
func TestDedupFilter_NewDedupFilter_BackwardCompatible(t *testing.T) {
	cfg := DefaultDedupConfig()
	filter := NewDedupFilter(cfg)
	require.NotNil(t, filter)
	defer filter.Stop()

	isDup, err := filter.CheckDuplicate("compat-event")
	assert.NoError(t, err)
	assert.False(t, isDup)

	isDup2, _ := filter.CheckDuplicate("compat-event")
	assert.True(t, isDup2)
}

// TestDedupFilter_Stop_MultipleCalls 确认 Stop 多次调用安全
func TestDedupFilter_Stop_MultipleCalls(t *testing.T) {
	filter := NewDedupFilter(DefaultDedupConfig())
	// 多次调用不应 panic
	assert.NotPanics(t, func() {
		filter.Stop()
		filter.Stop()
		filter.Stop()
	})
}
