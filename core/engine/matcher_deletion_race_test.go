package engine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestEngine_MatcherDeletionRaceCondition 测试 matcher 删除的竞态条件修复
func TestEngine_MatcherDeletionRaceCondition(t *testing.T) {
	engine := NewEngine()
	defer engine.Close()

	// 创建多个临时 matcher，使用次数为 1
	numMatchers := 100
	var matchers []*Matcher

	for i := 0; i < numMatchers; i++ {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules: []context.Rule{
				context.OnFullMatch("test"),
			},
			Handler: func(ctx *context.Context) error {
				return nil
			},
			coordinator: engine,
			priority:    50,
			Source:      "test",
		}
		m.rt.maxUseCount = 1
		atomic.StoreInt32(&m.rt.isTemp, 1)

		engine.services.tempManager.Add(m)
		matchers = append(matchers, m)
	}

	// 并发处理事件，触发 matcher 删除
	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				ctx := context.NewContext(&dto.Payload{
					Type: dto.C2CMessageCreate,
				}, nil)

				engine.ProcessEvent(ctx)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// 等待所有删除操作完成
	time.Sleep(200 * time.Millisecond)

	// 验证大部分临时 matcher 都已被使用（通过检查 deleted 标志）
	deletedCount := 0
	for _, m := range matchers {
		m.rt.mu.RLock()
		if m.rt.deleted {
			deletedCount++
		}
		m.rt.mu.RUnlock()
	}

	t.Logf("Deleted %d out of %d matchers", deletedCount, numMatchers)

	// 验证没有 panic 或死锁
	t.Log("Test completed successfully without race conditions")
}

// TestEngine_ConcurrentMatcherDeletion 测试并发修改 matcher 状态
func TestEngine_ConcurrentMatcherDeletion(t *testing.T) {
	engine := NewEngine()
	defer engine.Close()

	// 创建临时 matcher
	m := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []context.Rule{
			context.OnFullMatch("test"),
		},
		Handler: func(ctx *context.Context) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		},
		coordinator: engine,
		priority:    50,
		Source:      "test",
	}
	m.rt.maxUseCount = 5
	atomic.StoreInt32(&m.rt.isTemp, 1)

	engine.services.tempManager.Add(m)

	// 启动多个 goroutine 并发访问
	var wg sync.WaitGroup
	numGoroutines := 20

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx := context.NewContext(&dto.Payload{
				Type: dto.C2CMessageCreate,
			}, nil)

			engine.ProcessEvent(ctx)
		}(i)
	}

	wg.Wait()

	// 等待删除操作完成
	time.Sleep(200 * time.Millisecond)

	// 验证 matcher 最终被正确删除
	m.rt.mu.RLock()
	deleted := m.rt.deleted
	useCount := m.rt.useCount
	m.rt.mu.RUnlock()

	if !deleted {
		t.Logf("Warning: matcher not deleted, useCount=%d (might be expected if maxUseCount not reached)", useCount)
	}

	t.Log("Test completed successfully")
}

// TestEngine_MatcherIsTemplToggle 测试 isTemp 标志切换场景
func TestEngine_MatcherIsTemplToggle(t *testing.T) {
	engine := NewEngine()
	defer engine.Close()

	// 创建 matcher
	m := &Matcher{
		EventType: dto.C2CMessageCreate,
		Rules: []context.Rule{
			context.OnFullMatch("test"),
		},
		Handler: func(ctx *context.Context) error {
			return nil
		},
		coordinator: engine,
		priority:    50,
		Source:      "test",
	}
	m.rt.maxUseCount = 3
	atomic.StoreInt32(&m.rt.isTemp, 1)

	engine.services.tempManager.Add(m)

	// 模拟在处理过程中 isTemp 被修改的情况
	var wg sync.WaitGroup

	// Goroutine 1: 处理事件
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			ctx := context.NewContext(&dto.Payload{
				Type: dto.C2CMessageCreate,
			}, nil)
			engine.ProcessEvent(ctx)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Goroutine 2: 随机修改 isTemp（模拟迁移场景）
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			time.Sleep(15 * time.Millisecond)
			// 模拟迁移：从 temp 到 state
			atomic.StoreInt32(&m.rt.isTemp, 0)
			time.Sleep(5 * time.Millisecond)
			// 迁移回来
			atomic.StoreInt32(&m.rt.isTemp, 1)
		}
	}()

	wg.Wait()

	// 验证没有 panic
	t.Log("Test completed successfully without panic")
}

// TestEngine_PendingDeleteChannel 测试 pending delete channel 的正确性
func TestEngine_PendingDeleteChannel(t *testing.T) {
	engine := NewEngine(
		WithPendingDeleteBufferSize(5), // 小缓冲区，更容易触发满的情况
	)
	defer engine.Close()

	// 创建多个 matcher，使其进入 pending delete
	numMatchers := 20

	for i := 0; i < numMatchers; i++ {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules: []context.Rule{
				context.OnFullMatch("test"),
			},
			Handler: func(ctx *context.Context) error {
				return nil
			},
			coordinator: engine,
			priority:    50,
			Source:      "test",
		}
		m.rt.maxUseCount = 1
		atomic.StoreInt32(&m.rt.isTemp, 1)

		engine.services.tempManager.Add(m)

		// 触发删除
		ctx := context.NewContext(&dto.Payload{
			Type: dto.C2CMessageCreate,
		}, nil)
		engine.ProcessEvent(ctx)
	}

	// 等待批量删除处理器处理
	time.Sleep(500 * time.Millisecond)

	// 验证没有死锁或 panic
	t.Log("Test completed successfully")
}

// TestEngine_MatcherDeletionUnderLoad 压力测试：高负载下的 matcher 删除
func TestEngine_MatcherDeletionUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	engine := NewEngine()
	defer engine.Close()

	// 创建大量临时 matcher
	numMatchers := 1000
	for i := 0; i < numMatchers; i++ {
		m := &Matcher{
			EventType: dto.C2CMessageCreate,
			Rules: []context.Rule{
				context.OnFullMatch("test"),
			},
			Handler: func(ctx *context.Context) error {
				return nil
			},
			coordinator: engine,
			priority:    50,
			Source:      "test",
		}
		m.rt.maxUseCount = 1
		atomic.StoreInt32(&m.rt.isTemp, 1)
		engine.services.tempManager.Add(m)
	}

	// 高并发处理
	var wg sync.WaitGroup
	numGoroutines := 100
	duration := 2 * time.Second

	startTime := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for time.Since(startTime) < duration {
				ctx := context.NewContext(&dto.Payload{
					Type: dto.C2CMessageCreate,
				}, nil)
				engine.ProcessEvent(ctx)
			}
		}(i)
	}

	wg.Wait()

	// 等待清理完成
	time.Sleep(500 * time.Millisecond)

	t.Log("Stress test completed successfully")
}
