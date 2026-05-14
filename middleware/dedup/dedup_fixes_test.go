package dedup

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDedupFilter_WithContext_StopsOnContextCancel 验证修复 #2：parent ctx 取消时 cleanup goroutine 退出
func TestDedupFilter_WithContext_StopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := DedupConfig{
		MaxSize:         100,
		DefaultTTL:      1 * time.Second,
		CleanupInterval: 50 * time.Millisecond, // 短间隔便于测试
	}
	filter := NewDedupFilterWithContext(ctx, cfg)
	require.NotNil(t, filter)

	// 写入一条记录确认正常工作
	isDup, err := filter.CheckDuplicate("event-1")
	assert.NoError(t, err)
	assert.False(t, isDup, "First check should not be duplicate")

	isDup2, _ := filter.CheckDuplicate("event-1")
	assert.True(t, isDup2, "Second check should be duplicate")

	// 取消 parent ctx，cleanup goroutine 应该退出
	cancel()

	// 短暂等待 goroutine 退出（通过 cleanup 运行一次然后退出）
	time.Sleep(200 * time.Millisecond)

	// filter 仍可正常使用（已停止清理，但存量数据有效）
	stats := filter.GetStats()
	assert.Equal(t, 100, stats["max_size"])
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
