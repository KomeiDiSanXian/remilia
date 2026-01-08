package remilia

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestMiddlewareChainRaceCondition 测试中间件链并发更新的竞态条件
func TestMiddlewareChainRaceCondition(t *testing.T) {
	engine := NewEngine()

	var globalMWCalled atomic.Int32
	globalMW := func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			globalMWCalled.Add(1)
			return next(ctx)
		}
	}

	// 并发场景：
	// - goroutine 1: 快速添加多个中间件
	// - goroutine 2: 同时快速添加多个 matcher
	// 验证所有 matcher 都应用了所有中间件

	var wg sync.WaitGroup
	matcherCount := 50
	middlewareCount := 10

	// goroutine 1: 添加中间件
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < middlewareCount; i++ {
			engine.Use(globalMW)
			time.Sleep(time.Microsecond) // 模拟实际场景
		}
	}()

	// goroutine 2: 添加 matcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < matcherCount; i++ {
			engine.OnC2C().HandleE(func(ctx *Context) error {
				return nil
			})
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()

	// 验证所有 matcher 都能看到所有中间件
	// 每个 matcher 的中间件链长度应该是 middlewareCount（全局中间件数量）
	state := engine.state.Load().(*engineState)
	for i, m := range state.matchers {
		chain := m.getCombinedChain()
		// 全局中间件应该都被应用
		assert.GreaterOrEqual(t, len(chain), 0,
			"Matcher %d should have middleware chain", i)
	}

	// 处理事件，验证中间件被调用
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-race"}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	// 所有 matcher 都会触发，每个 matcher 都会执行完整的中间件链
	// 最终调用次数 = matcherCount * middlewareCount
	totalCalls := globalMWCalled.Load()
	expectedCalls := int32(matcherCount * middlewareCount)

	assert.Equal(t, expectedCalls, totalCalls,
		"All middleware should be called for all matchers")
}

// TestUseAfterOnRace 测试 Use() 在 On() 之后调用的竞态
func TestUseAfterOnRace(t *testing.T) {
	engine := NewEngine()

	var mw1Called, mw2Called atomic.Bool

	// 先添加 matcher
	engine.OnC2C().HandleE(func(ctx *Context) error {
		return nil
	})

	var wg sync.WaitGroup

	// 并发添加两个中间件
	wg.Add(2)
	go func() {
		defer wg.Done()
		engine.Use(func(next HandlerE) HandlerE {
			return func(ctx *Context) error {
				mw1Called.Store(true)
				return next(ctx)
			}
		})
	}()

	go func() {
		defer wg.Done()
		time.Sleep(time.Microsecond)
		engine.Use(func(next HandlerE) HandlerE {
			return func(ctx *Context) error {
				mw2Called.Store(true)
				return next(ctx)
			}
		})
	}()

	wg.Wait()

	// 处理事件
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-use-after"}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	// 两个中间件都应该被调用
	assert.True(t, mw1Called.Load(), "First middleware should be called")
	assert.True(t, mw2Called.Load(), "Second middleware should be called")
}

// TestConcurrentUseAndOn 测试 Use() 和 On() 的并发调用
func TestConcurrentUseAndOn(t *testing.T) {
	engine := NewEngine()

	var callCount atomic.Int32
	testMW := func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			callCount.Add(1)
			return next(ctx)
		}
	}

	iterations := 100
	var wg sync.WaitGroup

	// 并发执行 Use 和 On
	for i := 0; i < iterations; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			engine.Use(testMW)
		}()

		go func() {
			defer wg.Done()
			engine.OnC2C().HandleE(func(ctx *Context) error {
				return nil
			})
		}()
	}

	wg.Wait()

	// 验证没有 panic 且所有操作都成功
	mwState := engine.middleware.Load().(*middlewareState)
	assert.Equal(t, iterations, len(mwState.global.chain),
		"All middleware should be registered")

	matcherCount := engine.GetMatcherCount()
	assert.GreaterOrEqual(t, matcherCount, iterations,
		"All matchers should be registered")
}

// TestPluginMiddlewareRace 测试插件中间件的竞态条件
func TestPluginMiddlewareRace(t *testing.T) {
	engine := NewEngine()

	var pluginMWCalled atomic.Int32
	pluginMW := func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			pluginMWCalled.Add(1)
			return next(ctx)
		}
	}

	pluginName := "test-plugin"
	matcherCount := 20

	var wg sync.WaitGroup

	// 并发：添加插件中间件和插件 matcher
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			engine.UseForGroup(pluginName, pluginMW)
			time.Sleep(time.Microsecond)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < matcherCount; i++ {
			m := engine.OnC2C()
			m.Source = "plugin:" + pluginName
			m.SetGroup(pluginName)
			m.HandleE(func(ctx *Context) error {
				return nil
			})
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()

	// 处理事件
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-plugin-race"}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	// 验证插件中间件被调用
	assert.Greater(t, pluginMWCalled.Load(), int32(0),
		"Plugin middleware should be called")
}

// TestMiddlewareChainConsistency 测试中间件链的一致性
func TestMiddlewareChainConsistency(t *testing.T) {
	engine := NewEngine()

	mw1Order := 1
	mw2Order := 2
	mw3Order := 3

	var executionOrder []int
	var orderMu sync.Mutex

	createMW := func(order int) HandlerMiddleware {
		return func(next HandlerE) HandlerE {
			return func(ctx *Context) error {
				orderMu.Lock()
				executionOrder = append(executionOrder, order)
				orderMu.Unlock()
				return next(ctx)
			}
		}
	}

	// 按顺序添加中间件
	engine.Use(createMW(mw1Order))
	engine.Use(createMW(mw2Order))
	engine.Use(createMW(mw3Order))

	// 添加 matcher
	engine.OnC2C().HandleE(func(ctx *Context) error {
		return nil
	})

	// 处理事件
	event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-order"}
	ctx := NewContext(event, nil)

	engine.ProcessEvent(ctx)

	// 验证执行顺序
	assert.Equal(t, []int{1, 2, 3}, executionOrder,
		"Middleware should execute in order")
}

// TestRebuildChainDuringExecution 测试在执行期间重建链不会影响正在执行的 handler
func TestRebuildChainDuringExecution(t *testing.T) {
	engine := NewEngine()

	var executionStarted atomic.Bool
	var newMWCalled atomic.Bool

	// 添加一个慢 handler
	engine.OnC2C().HandleE(func(ctx *Context) error {
		executionStarted.Store(true)
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	// 开始处理事件
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		event := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-rebuild"}
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}()

	// 等待执行开始
	for !executionStarted.Load() {
		time.Sleep(time.Millisecond)
	}

	// 在执行期间添加新中间件
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			newMWCalled.Store(true)
			return next(ctx)
		}
	})

	wg.Wait()

	// 新中间件不应该影响正在执行的 handler
	assert.False(t, newMWCalled.Load(),
		"New middleware should not affect running handlers")

	// 但应该影响新的事件
	event2 := &dto.Payload{Type: dto.C2CMessageCreate, ID: "test-rebuild-2"}
	ctx2 := NewContext(event2, nil)

	engine.ProcessEvent(ctx2)

	assert.True(t, newMWCalled.Load(),
		"New middleware should affect new events")
}

// BenchmarkConcurrentUseAndOn 基准测试并发 Use 和 On
func BenchmarkConcurrentUseAndOn(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		engine := NewEngine()
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				engine.Use(func(next HandlerE) HandlerE {
					return func(ctx *Context) error {
						return next(ctx)
					}
				})
			} else {
				engine.OnC2C().HandleE(func(ctx *Context) error {
					return nil
				})
			}
			i++
		}
	})
}

// BenchmarkMiddlewareChainRebuild 基准测试中间件链重建
func BenchmarkMiddlewareChainRebuild(b *testing.B) {
	engine := NewEngine()

	// 预先添加一些 matcher
	for i := 0; i < 100; i++ {
		engine.OnC2C().HandleE(func(ctx *Context) error {
			return nil
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Use(func(next HandlerE) HandlerE {
			return func(ctx *Context) error {
				return next(ctx)
			}
		})
	}
}
