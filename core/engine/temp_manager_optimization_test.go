package engine

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
)

// TestTempManagerWatermarkCleanup 测试水位线触发清理
func TestTempManagerWatermarkCleanup(t *testing.T) {
	config := TempManagerConfig{
		WatermarkHigh:         10,
		WatermarkLow:          5,
		EnableAdaptiveCleanup: true,
	}

	tm := newTempMatcherManagerWithConfig(config)

	// 添加 15 个匹配器
	for i := range 15 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.rt.createdAt = time.Now().Add(time.Duration(i) * time.Millisecond)
		matcher.priority.Store(64)
		tm.Add(matcher)
	}

	// 等待异步清理完成
	time.Sleep(100 * time.Millisecond)

	// 检查数量是否降到低水位线附近
	count := tm.Count()
	t.Logf("Count after watermark cleanup: %d", count)

	if count > 10 {
		t.Errorf("Expected count <= 10 after cleanup, got %d", count)
	}
}

// TestTempManagerCount 测试计数功能
func TestTempManagerCount(t *testing.T) {
	tm := newTempMatcherManager()

	if tm.Count() != 0 {
		t.Errorf("Expected count 0, got %d", tm.Count())
	}

	// 添加匹配器
	for i := range 5 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i))
		tm.Add(matcher)
	}

	if tm.Count() != 5 {
		t.Errorf("Expected count 5, got %d", tm.Count())
	}

	// 验证精确计数
	accurate := tm.CountAccurate()
	if accurate != 5 {
		t.Errorf("Expected accurate count 5, got %d", accurate)
	}
}

// TestTempManagerCleanExpired 测试过期清理
func TestTempManagerCleanExpired(t *testing.T) {
	tm := newTempMatcherManager()

	now := time.Now()

	// 添加过期的匹配器
	for i := range 3 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i))
		matcher.rt.expiresAt = now.Add(-1 * time.Hour) // 已过期
		matcher.rt.createdAt = now
		tm.Add(matcher)
	}

	// 添加未过期的匹配器
	for i := range 2 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i + 10))
		matcher.rt.expiresAt = now.Add(1 * time.Hour) // 未过期
		matcher.rt.createdAt = now
		tm.Add(matcher)
	}

	if tm.Count() != 5 {
		t.Errorf("Expected count 5 before cleanup, got %d", tm.Count())
	}

	// 清理过期的
	expired := tm.CleanExpired()

	if len(expired) != 3 {
		t.Errorf("Expected 3 expired matchers, got %d", len(expired))
	}

	if tm.Count() != 2 {
		t.Errorf("Expected count 2 after cleanup, got %d", tm.Count())
	}
}

// TestTempManagerRemove 测试删除功能
func TestTempManagerRemove(t *testing.T) {
	tm := newTempMatcherManager()

	matchers := make([]*Matcher, 0, 3)
	for i := range 3 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i))
		matcher.rt.createdAt = time.Now()
		tm.Add(matcher)
		matchers = append(matchers, matcher)
	}

	if tm.Count() != 3 {
		t.Errorf("Expected count 3, got %d", tm.Count())
	}

	// 删除一个
	tm.Remove(matchers[1])

	if tm.Count() != 2 {
		t.Errorf("Expected count 2 after remove, got %d", tm.Count())
	}

	// 再次删除同一个（应该不影响计数）
	tm.Remove(matchers[1])

	if tm.Count() != 2 {
		t.Errorf("Expected count still 2 after double remove, got %d", tm.Count())
	}
}

// TestTempManagerStats 测试统计信息
func TestTempManagerStats(t *testing.T) {
	config := TempManagerConfig{
		WatermarkHigh: 100,
		WatermarkLow:  50,
	}

	tm := newTempMatcherManagerWithConfig(config)

	// 添加一些匹配器
	for i := range 10 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i))
		matcher.rt.createdAt = time.Now()
		tm.Add(matcher)
	}

	stats := tm.GetStats()

	if stats.Count != 10 {
		t.Errorf("Expected count 10, got %d", stats.Count)
	}

	if stats.WatermarkHigh != 100 {
		t.Errorf("Expected WatermarkHigh 100, got %d", stats.WatermarkHigh)
	}

	if stats.WatermarkLow != 50 {
		t.Errorf("Expected WatermarkLow 50, got %d", stats.WatermarkLow)
	}

	t.Logf("Stats: %+v", stats)
}

// TestTempManagerCleanToWatermark 测试清理到水位线
func TestTempManagerCleanToWatermark(t *testing.T) {
	config := TempManagerConfig{
		WatermarkHigh:         50,
		WatermarkLow:          30,
		EnableAdaptiveCleanup: false, // 手动触发
	}

	tm := newTempMatcherManagerWithConfig(config)

	// 添加 60 个匹配器
	for i := range 60 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i))
		matcher.rt.createdAt = time.Now().Add(time.Duration(i) * time.Millisecond)
		tm.Add(matcher)
	}

	if tm.Count() != 60 {
		t.Errorf("Expected count 60, got %d", tm.Count())
	}

	// 手动触发清理
	tm.cleanToWatermark()

	count := tm.Count()
	t.Logf("Count after cleanToWatermark: %d", count)

	// 应该清理到低水位线附近（允许一些误差）
	if count > 35 || count < 25 {
		t.Errorf("Expected count around 30, got %d", count)
	}
}

// BenchmarkTempManagerAdd 基准测试：添加性能
func BenchmarkTempManagerAdd(b *testing.B) {
	tm := newTempMatcherManager()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i % 100))
		matcher.rt.createdAt = time.Now()
		tm.Add(matcher)
	}
}

// BenchmarkTempManagerCount 基准测试：计数性能
func BenchmarkTempManagerCount(b *testing.B) {
	tm := newTempMatcherManager()

	// 添加一些匹配器
	for i := range 1000 {
		matcher := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules:     []context.Rule{func(ctx *context.Context) bool { return true }},
			Handler:   func(ctx *context.Context) error { return nil },
			Source:    "test",
		}
		matcher.priority.Store(uint64(i))
		matcher.rt.createdAt = time.Now()
		tm.Add(matcher)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tm.Count()
	}
}
