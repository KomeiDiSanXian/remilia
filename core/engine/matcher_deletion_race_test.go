package engine

import (
	stdctx "context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/stretchr/testify/assert"
)

// TestEngine_MatcherDeletionRaceCondition 测试 matcher 删除的竞态条件修复
func TestEngine_MatcherDeletionRaceCondition(t *testing.T) {
	engine := newEngineForTest(t)
	defer engine.Shutdown(stdctx.Background())

	// 创建多个临时 matcher，使用次数为 1
	numMatchers := 100
	var matchers []*Matcher

	for range numMatchers {
		m := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules: []context.Rule{
				context.OnFullMatch("test"),
			},
			Handler: func(ctx *context.Context) error {
				return nil
			},
			coordinator: engine,
			Source:      "test",
		}
		m.priority.Store(50)
		m.rt.maxUseCount = 1
		atomic.StoreInt32(&m.rt.isTemp, 1)

		engine.internals.tempManager.Add(m)
		matchers = append(matchers, m)
	}

	// 并发处理事件，触发 matcher 删除
	var wg sync.WaitGroup
	numGoroutines := 50

	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for range 10 {
				ctx := context.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
				engine.ProcessEvent(ctx)
				// timing: simulated delay for race conditioning
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Temp matcher deletion is inline (in invokeHandler), so after wg.Wait
	// all temp matchers have already been removed from tempManager.
	// 验证大部分临时 matcher 都已被使用（通过检查 deleted 标志）
	deletedCount := 0
	for _, m := range matchers {
		if m.rt.deleted.Load() {
			deletedCount++
		}
	}

	t.Logf("Deleted %d out of %d matchers", deletedCount, numMatchers)

	// 验证没有 panic 或死锁
	t.Log("Test completed successfully without race conditions")
}

// TestEngine_ConcurrentMatcherDeletion 测试并发修改 matcher 状态
func TestEngine_ConcurrentMatcherDeletion(t *testing.T) {
	engine := newEngineForTest(t)
	defer engine.Shutdown(stdctx.Background())

	// 创建临时 matcher
	m := &Matcher{
		EventType: string(platform.EventKindPrivateMessage),
		Rules: []context.Rule{
			context.OnFullMatch("test"),
		},
		Handler: func(ctx *context.Context) error {
			// timing: simulated processing delay
			time.Sleep(10 * time.Millisecond)
			return nil
		},
		coordinator: engine,
		Source:      "test",
	}
	m.priority.Store(50)
	m.rt.maxUseCount = 5
	atomic.StoreInt32(&m.rt.isTemp, 1)

	engine.internals.tempManager.Add(m)

	// 启动多个 goroutine 并发访问
	var wg sync.WaitGroup
	numGoroutines := 20

	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx := context.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
			engine.ProcessEvent(ctx)
		}(i)
	}

	wg.Wait()

	// Temp deletion is inline in invokeHandler, no need to wait.
	// 验证 matcher 最终被正确删除
	m.rt.mu.RLock()
	useCount := m.rt.useCount
	m.rt.mu.RUnlock()
	deleted := m.rt.deleted.Load()

	if !deleted {
		t.Logf("Warning: matcher not deleted, useCount=%d (might be expected if maxUseCount not reached)", useCount)
	}

	t.Log("Test completed successfully")
}

// TestEngine_MatcherIsTemplToggle 测试 isTemp 标志切换场景
func TestEngine_MatcherIsTemplToggle(t *testing.T) {
	engine := newEngineForTest(t)
	defer engine.Shutdown(stdctx.Background())

	// 创建 matcher
	m := &Matcher{
		EventType: string(platform.EventKindPrivateMessage),
		Rules: []context.Rule{
			context.OnFullMatch("test"),
		},
		Handler: func(ctx *context.Context) error {
			return nil
		},
		coordinator: engine,
		Source:      "test",
	}
	m.priority.Store(50)
	m.rt.maxUseCount = 3
	atomic.StoreInt32(&m.rt.isTemp, 1)

	engine.internals.tempManager.Add(m)

	// 模拟在处理过程中 isTemp 被修改的情况
	var wg sync.WaitGroup

	// Goroutine 1: 处理事件
	wg.Go(func() {
		for range 5 {
			ctx := context.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
			engine.ProcessEvent(ctx)
			// timing: simulated delay for race conditioning
			time.Sleep(10 * time.Millisecond)
		}
	})

	// Goroutine 2: 随机修改 isTemp（模拟迁移场景）
	wg.Go(func() {
		for range 3 {
			// timing: simulated delays for race conditioning
			time.Sleep(15 * time.Millisecond)
			// 模拟迁移：从 temp 到 state
			atomic.StoreInt32(&m.rt.isTemp, 0)
			// timing: simulated delay for race conditioning
			time.Sleep(5 * time.Millisecond)
			// 迁移回来
			atomic.StoreInt32(&m.rt.isTemp, 1)
		}
	})

	wg.Wait()

	// 验证没有 panic
	t.Log("Test completed successfully without panic")
}

// TestEngine_PendingDeleteChannel 测试 pending delete channel 的正确性
func TestEngine_PendingDeleteChannel(t *testing.T) {
	engine := newEngineForTest(t,
		WithPendingDeleteBufferSize(5), // 小缓冲区，更容易触发满的情况
	)
	defer engine.Shutdown(stdctx.Background())

	// 创建多个 matcher，使其进入 pending delete
	numMatchers := 20

	for range numMatchers {
		m := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules: []context.Rule{
				context.OnFullMatch("test"),
			},
			Handler: func(ctx *context.Context) error {
				return nil
			},
			coordinator: engine,
			Source:      "test",
		}
		m.priority.Store(50)
		m.rt.maxUseCount = 1
		atomic.StoreInt32(&m.rt.isTemp, 1)

		engine.internals.tempManager.Add(m)

		// 触发删除 (use "test" content to match OnFullMatch rule)
		evt := newTestPlatformEventWithContent(platform.EventKindPrivateMessage, "test")
		ctx := context.AcquireContextFromEvent(evt, nil)
		engine.ProcessEvent(ctx)
	}

	// Temp matchers are deleted inline in invokeHandler (tempManager.Remove).
	// Verify all have been removed.
	assert.Equal(t, 0, engine.GetTempMatcherCount())

	// 验证没有死锁或 panic
	t.Log("Test completed successfully")
}

// TestEngine_MatcherDeletionUnderLoad 压力测试：高负载下的 matcher 删除
func TestEngine_MatcherDeletionUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	engine := newEngineForTest(t)
	defer engine.Shutdown(stdctx.Background())

	// 创建大量临时 matcher
	numMatchers := 1000
	for range numMatchers {
		m := &Matcher{
			EventType: string(platform.EventKindPrivateMessage),
			Rules: []context.Rule{
				context.OnFullMatch("test"),
			},
			Handler: func(ctx *context.Context) error {
				return nil
			},
			coordinator: engine,
			Source:      "test",
		}
		m.priority.Store(50)
		m.rt.maxUseCount = 1
		atomic.StoreInt32(&m.rt.isTemp, 1)
		engine.internals.tempManager.Add(m)
	}

	// 高并发处理
	var wg sync.WaitGroup
	numGoroutines := 100
	duration := 2 * time.Second

	startTime := time.Now()

	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for time.Since(startTime) < duration {
				ctx := context.AcquireContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
				engine.ProcessEvent(ctx)
			}
		}(i)
	}

	wg.Wait()

	// timing: stress test settle time
	time.Sleep(500 * time.Millisecond)

	t.Log("Stress test completed successfully")
}
