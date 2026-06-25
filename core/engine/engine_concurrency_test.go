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

// TestEngineRaceConditions 测试并发注册和删除匹配器
func TestEngineRaceConditions(t *testing.T) {
	t.Run("concurrent_register_and_delete", func(t *testing.T) {
		engine := newEngineForTest(t)
		defer engine.Shutdown(stdctx.Background())

		const goroutines = 10
		const matchersPerGoroutine = 20
		var wg sync.WaitGroup

		// 并发注册匹配器
		for i := range goroutines {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for range matchersPerGoroutine {
					matcher := engine.On(string(platform.EventKindPrivateMessage), context.OnFullMatch("test"))
					matcher.Handle(func(ctx *context.Context) error {
						return nil
					})
					// timing: simulated delay for race conditioning
					time.Sleep(1 * time.Millisecond)
				}
			}(i)
		}

		wg.Wait()

		// 验证匹配器数量
		count := engine.GetMatcherCount()
		assert.Equal(t, goroutines*matchersPerGoroutine, count, "Should register all matchers")

		// 并发删除匹配器 - 无需类型断言
		state := engine.state.Load()
		matchers := append([]*Matcher(nil), state.matchers...)

		for i := range goroutines {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				start := id * matchersPerGoroutine
				end := min(start+matchersPerGoroutine, len(matchers))
				for _, m := range matchers[start:end] {
					engine.DeleteMatcher(m)
					// timing: simulated delay for race conditioning
					time.Sleep(1 * time.Millisecond)
				}
			}(i)
		}

		wg.Wait()

		// 验证所有匹配器已删除
		finalCount := engine.GetMatcherCount()
		assert.Equal(t, 0, finalCount, "Should delete all matchers")
	})

	t.Run("concurrent_process_and_modify", func(t *testing.T) {
		engine := newEngineForTest(t)
		defer engine.Shutdown(stdctx.Background())

		var processCount atomic.Int32

		// 注册一些匹配器
		for range 10 {
			matcher := engine.OnAny()
			matcher.Handle(func(ctx *context.Context) error {
				processCount.Add(1)
				return nil
			})
		}

		var wg sync.WaitGroup

		// 并发处理事件
		for range 5 {
			wg.Go(func() {
				for range 10 {
					ctx := context.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
					engine.ProcessEvent(ctx)
				}
			})
		}

		// 同时并发注册新匹配器
		for range 3 {
			wg.Go(func() {
				for range 5 {
					matcher := engine.OnAny()
					matcher.Handle(func(ctx *context.Context) error {
						return nil
					})
				}
			})
		}

		wg.Wait()

		// 验证没有崩溃，且事件被处理
		assert.Greater(t, processCount.Load(), int32(0), "Should process some events")
	})
}

// TestEngineShutdownWithPendingEvents 测试关闭时的事件处理
func TestEngineShutdownWithPendingEvents(t *testing.T) {
	t.Run("shutdown_waits_for_events", func(t *testing.T) {
		engine := newEngineForTest(t)

		var processedCount atomic.Int32
		var processingCount atomic.Int32

		// 注册一个慢速处理器
		matcher := engine.OnAny()
		matcher.Handle(func(ctx *context.Context) error {
			processingCount.Add(1)
			// timing: simulated processing delay
			time.Sleep(100 * time.Millisecond)
			processedCount.Add(1)
			processingCount.Add(-1)
			return nil
		})

		// 启动多个事件处理，使用 channel 确保所有 goroutine 已调度后再 shutdown
		const eventCount = 5
		started := make(chan struct{}, eventCount)
		for range eventCount {
			go func() {
				started <- struct{}{}
				ctx := context.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
				engine.ProcessEvent(ctx)
			}()
		}

		// 等待所有 goroutine 已调度（避免 -race 下调度延迟导致 shutdown 先执行）
		for range eventCount {
			<-started
		}
		// 给事件一点时间进入处理状态
		assert.Eventually(t, func() bool {
			return processingCount.Load() > 0
		}, time.Second, 10*time.Millisecond)

		// 开始关闭（应该等待）
		shutdownStart := time.Now()
		err := engine.Shutdown(stdctx.Background())
		shutdownDuration := time.Since(shutdownStart)

		assert.NoError(t, err)
		assert.GreaterOrEqual(t, shutdownDuration, 20*time.Millisecond, "Stop should wait for events")
		assert.Equal(t, int32(eventCount), processedCount.Load(), "Should process all events before shutdown")
		assert.Equal(t, int32(0), processingCount.Load(), "No events should be processing after shutdown")
	})

	t.Run("shutdown_respects_context_timeout", func(t *testing.T) {
		engine := newEngineForTest(t)

		// 注册一个非常慢的处理器
		matcher := engine.OnAny()
		matcher.Handle(func(ctx *context.Context) error {
			time.Sleep(10 * time.Second)
			return nil
		})

		// 确保事件 goroutine 已调度后再 shutdown
		ready := make(chan struct{})
		go func() {
			ready <- struct{}{}
			ctx := context.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
			engine.ProcessEvent(ctx)
		}()
		<-ready

		// 使用有超时的 context 关闭；超时值比测试下界宽松以适应并行负载
		ctx, cancel := stdctx.WithTimeout(stdctx.Background(), 200*time.Millisecond)
		defer cancel()

		shutdownStart := time.Now()
		err := engine.Shutdown(ctx)
		shutdownDuration := time.Since(shutdownStart)

		assert.Error(t, err)
		assert.Less(t, shutdownDuration, 500*time.Millisecond, "Should timeout quickly")
	})

	t.Run("shutdown_is_idempotent", func(t *testing.T) {
		engine := newEngineForTest(t)

		// 第一次关闭
		err1 := engine.Shutdown(stdctx.Background())
		assert.NoError(t, err1)

		// 第二次关闭（应该是安全的）
		err2 := engine.Shutdown(stdctx.Background())
		assert.NoError(t, err2)

		// Close 方法也应该是安全的
		engine.Shutdown(stdctx.Background())
		engine.Shutdown(stdctx.Background())
	})
}

// TestEngineMemoryLeaks 测试内存泄漏
func TestEngineMemoryLeaks(t *testing.T) {
	t.Run("matcher_deletion_releases_memory", func(t *testing.T) {
		engine := newEngineForTest(t)
		defer engine.Shutdown(stdctx.Background())

		// 创建大量匹配器
		const matcherCount = 1000
		matchers := make([]*Matcher, matcherCount)

		for i := range matcherCount {
			matchers[i] = engine.OnAny()
			matchers[i].Handle(func(ctx *context.Context) error {
				return nil
			})
		}

		assert.Equal(t, matcherCount, engine.GetMatcherCount())

		// 删除所有匹配器
		for _, m := range matchers {
			engine.DeleteMatcher(m)
		}

		assert.Equal(t, 0, engine.GetMatcherCount())

		// 验证状态清理 - 无需类型断言
		state := engine.state.Load()
		assert.Equal(t, 0, len(state.matchers))
		assert.Equal(t, 0, len(state.matcherIndex))
	})

	t.Run("temp_matcher_cleanup", func(t *testing.T) {
		engine := newEngineForTest(t)
		defer engine.Shutdown(stdctx.Background())

		// 创建一些一次性临时匹配器
		const tempCount = 50
		for range tempCount {
			matcher := engine.OnTemp(string(platform.EventKindPrivateMessage))
			matcher.SetTempWithMaxUse(1)
			matcher.Handle(func(ctx *context.Context) error {
				return nil
			})
		}

		initialCount := engine.GetTempMatcherCount()
		assert.Equal(t, tempCount, initialCount)

		// 触发所有匹配器（每个使用1次后应该被删除）
		for range tempCount {
			ctx := context.NewContextFromEvent(newTestPlatformEvent(platform.EventKindPrivateMessage), nil)
			engine.ProcessEvent(ctx)
		}

		// Wait for temp matchers to be cleaned
		assert.Eventually(t, func() bool {
			return engine.GetTempMatcherCount() == 0
		}, time.Second, 50*time.Millisecond, "Used temp matchers should be cleaned")
	})
}
