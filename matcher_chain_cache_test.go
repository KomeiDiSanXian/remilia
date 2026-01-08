package remilia

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestMatcherChainCacheInvalidation_ConcurrentModification 测试在Handler执行期间并发修改中间件链导致的缓存失效
func TestMatcherChainCacheInvalidation_ConcurrentModification(t *testing.T) {
	engine := NewEngine()

	// 1. 设置一个基础 Matcher
	var handler1Called atomic.Int32
	m1 := engine.OnC2C().Handle(func(ctx *Context) {
		handler1Called.Add(1)
		// 模拟耗时操作，让 Handler 执行期间更容易发生并发修改
		time.Sleep(10 * time.Millisecond)
	})

	// 2. 初始状态：无中间件
	chain1, gGen1, pGen1 := m1.getChainCache()
	assert.Len(t, chain1, 0)
	// global generation starts at 1 in newMiddlewareState
	assert.GreaterOrEqual(t, gGen1, uint64(1))
	assert.Equal(t, uint64(0), pGen1)

	// 3. 并发执行：
	// - Goroutine A: 不断触发事件 (ProcessEvent) -> 触发 ensureMatcherChainWithState
	// - Goroutine B: 不断添加全局中间件 -> 触发 Generation ID 变更

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mwAddedCount atomic.Int32

	// Goroutine A: 触发事件
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				engine.ProcessEvent(NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil))
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Goroutine B: 添加中间件
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			time.Sleep(5 * time.Millisecond)
			engine.Use(func(next HandlerE) HandlerE {
				return func(ctx *Context) error {
					return next(ctx)
				}
			})
			mwAddedCount.Add(1)
		}
	}()

	// 等待 B 完成
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Force a final sync to ensure the matcher picks up the latest middleware state.
	// The matcher updates its chain lazily (on execution), and since the event loop (Goroutine A)
	// might have stopped before processing the very last middleware addition from Goroutine B,
	// we need to trigger an update manually to assert the final consistent state.
	engine.RebuildMatcherChain(m1)

	// 4. 验证最终状态
	// 最终的 cache 应该是包含了所有中间件的
	chainFinal, gGenFinal, _ := m1.getChainCache()

	// 中间件总数应该等于添加的数量
	assert.Equal(t, int(mwAddedCount.Load()), len(chainFinal), "Final chain length should match added middlewares")

	// Generation ID 应该增加了 (初始为0, 每次Use增加)
	// 注意: engine.Use 可能会合并多次变更，未必每次都 +1，但肯定大于0
	assert.Greater(t, gGenFinal, uint64(0), "Global generation should increased")
}

// TestMatcherChainCache_GenerationID_Correctness 测试 Generation ID 是否正确触发重组
func TestMatcherChainCache_GenerationID_Correctness(t *testing.T) {
	engine := NewEngine()
	m := engine.OnC2C().Handle(func(ctx *Context) {})

	// 1. 初始状态
	engine.RebuildMatcherChain(m)
	chain, gGen, _ := m.getChainCache()
	assert.Len(t, chain, 0)
	initialGen := gGen

	// 2. 添加全局中间件
	engine.Use(func(next HandlerE) HandlerE { return next })

	// 触发更新 (模拟ProcessEvent中的调用)
	mwState := engine.middleware.Load().(*middlewareState)
	engine.ensureMatcherChainWithState(m, mwState)

	// 3. 检查 Generation ID 变更
	chain2, gGen2, _ := m.getChainCache()
	assert.Len(t, chain2, 1)
	assert.NotEqual(t, initialGen, gGen2, "Generation should change after adding global middleware")

	// 4. 添加局部中间件 (Matcher级别)
	m.Use(func(next HandlerE) HandlerE { return next })

	// 注意：m.Use 会调用 invalidateCombinedChain，也就是 cache 被清空
	// 这里通过 ensureMatcherChainWithState 重建
	mwState = engine.middleware.Load().(*middlewareState)
	engine.ensureMatcherChainWithState(m, mwState)

	chain3, _, _ := m.getChainCache()
	assert.Len(t, chain3, 2, "Should include both global and local middleware")
}

// TestMatcherChainCache_ConcurrentEnsureChain 测试并发调用 ensureChain 的稳定性
func TestMatcherChainCache_ConcurrentEnsureChain(t *testing.T) {
	m := &Matcher{
		rt: matcherRuntime{},
	}
	// 手动初始化 m.combinedChain 防止 nil pointer?
	// Make sure combinedChain is properly initialized as in constructor (though zero value is nil, Store handles it)

	// 模拟大量的 ensureChain 调用，使用不同的 generation
	var wg sync.WaitGroup
	workers := 20
	iterations := 1000

	globalChain := []HandlerMiddleware{func(next HandlerE) HandlerE { return next }}
	groupChain := []HandlerMiddleware{func(next HandlerE) HandlerE { return next }}

	// 模拟 generation 不断增长
	var currentGen atomic.Uint64

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				gen := currentGen.Load()
				// 偶尔增加 generation
				if j%10 == 0 {
					gen = currentGen.Add(1)
				}

				// 模拟传入的链和 generation
				m.ensureChain(globalChain, gen, groupChain, 0)

				// 读取验证
				c, g, _ := m.getChainCache()
				if c != nil && g < gen {
					// 这是一个允许的情况：因为并发，可能读取到旧的，
					// 但 ensureChain 应该保证后续读取到新的（如果 gen 确实更新了）
					// 这里主要为了跑并发检测是否有 panic 或数据竞争
				}
			}
		}()
	}

	wg.Wait()

	// 最后确保它是最新的
	finalGen := currentGen.Load()
	m.ensureChain(globalChain, finalGen, groupChain, 0)
	_, g, _ := m.getChainCache()
	assert.Equal(t, finalGen, g, "Final generation should match")
}

// TestMatcherChainCache_GroupMiddleware_Invalidation 测试插件级别中间件更新导致的缓存失效
func TestMatcherChainCache_GroupMiddleware_Invalidation(t *testing.T) {
	engine := NewEngine()

	// 创建一个属于特定插件的 Matcher
	// 注意：Engine.On... 不直接暴露 Source/Group 设置，需要通过 Source Helper 或者 mock
	// 但我们可以利用 engine.UsePlugin (如果存在) 或者手动构造 Matcher

	// 简单起见，我们直接操作 internal state (mocking) 或者通过 public API
	// 假设我们使用 OnC2C 并标记 Source 为 plugin:test

	m := engine.OnC2C().Handle(func(ctx *Context) {})
	// 这是一个hack，为了测试 group logic，我们需要设置 group
	// 在 engine.go 中：
	// if groupName == "" && strings.HasPrefix(m.Source, "plugin:") {
	//    groupName = strings.TrimPrefix(m.Source, "plugin:")
	// }
	// 由于 Matcher.Source 是 public string，我们可以直接修改它 (虽然不推荐，但在测试中可以)
	// 但 engine.On... 创建后已经加入 state，修改 Source 可能不会重新索引 group map 如果有的话
	// 不过 middleware state 是按 groupName 查找的。

	// 获取 Matcher 指针并修改 runtime 属性 (仅为了测试)
	// 更好的方式是使用 Plugin API (如果可用)
	// Remilia 似乎有 Plugin Support

	// 1. 初始状态
	engine.RebuildMatcherChain(m)
	chain, _, pGen := m.getChainCache()
	assert.Len(t, chain, 0)
	_ = pGen

	// 2. 模拟 Group Middleware 更新带来的失效
	// 由于直接模拟 Plugin 较为复杂，这里直接测试 ensureChain 对 generation 变化的响应

	groupChain := []HandlerMiddleware{func(n HandlerE) HandlerE { return n }}
	globalChain := []HandlerMiddleware{}

	// 确保 reset
	m.ensureChain(globalChain, 0, nil, 0)
	c, _, g := m.getChainCache()
	assert.Len(t, c, 0)
	assert.Equal(t, uint64(0), g)

	// Update group gen - should trigger updates
	m.ensureChain(globalChain, 0, groupChain, 1)
	c, _, g = m.getChainCache()
	assert.Len(t, c, 1)
	assert.Equal(t, uint64(1), g)
}

// TestMatcherChain_UpdateIsolation_DuringExecution 测试 Handler 执行期间中间件更新的隔离性
// 正在执行的 Handler 应该继续使用旧链，新事件才使用新链
func TestMatcherChain_UpdateIsolation_DuringExecution(t *testing.T) {
	engine := NewEngine()

	var chainExecCount atomic.Int32
	// 定义一个中间件，记录执行次数
	mw1 := func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			chainExecCount.Add(1)
			// 模拟在中间件链执行过程中这里卡住一下
			time.Sleep(20 * time.Millisecond)
			return next(ctx)
		}
	}

	engine.Use(mw1)

	// 定义 Handler
	m := engine.OnC2C().Handle(func(ctx *Context) {
		// Handler 逻辑
	})

	var wg sync.WaitGroup
	wg.Add(1)

	// 1. 发送第一个事件，触发执行
	go func() {
		defer wg.Done()
		engine.ProcessEvent(NewContext(&dto.Payload{Type: dto.C2CMessageCreate, ID: "event-1"}, nil))
	}()

	// 确保第一个事件已经开始执行 (sleep 稍微短一点，确保进入 mw1)
	time.Sleep(5 * time.Millisecond)

	// 2. 修改中间件链：添加第二个中间件
	// 预期：这次修改不影响 event-1 的执行
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			// 这个不应该被 event-1 执行
			return next(ctx)
		}
	})

	wg.Wait()
	// event-1 结束

	assert.Equal(t, int32(1), chainExecCount.Load(), "Middleware 1 should be executed once for event 1")

	// 3. 发送第二个事件
	// 预期：应用新的中间件链
	chainExecCount.Store(0)

	// 这里我们需要确保 rebuild 发生， ProcessEvent 会自动做
	engine.ProcessEvent(NewContext(&dto.Payload{Type: dto.C2CMessageCreate, ID: "event-2"}, nil))

	// 检查当前链长度
	chain, _, _ := m.getChainCache()
	// engine.Use 是 append，所以现在应该有 2 个全局中间件
	assert.Len(t, chain, 2, "Should have 2 middlewares for event 2")
	assert.Equal(t, int32(1), chainExecCount.Load(), "Middleware 1 should be executed once for event 2")
}
