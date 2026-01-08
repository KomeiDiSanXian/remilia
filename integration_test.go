package remilia_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_FullLifecycle 测试完整的生命周期：创建 Bot -> 注册 Handler -> 处理事件 -> 关闭
func TestIntegration_FullLifecycle(t *testing.T) {
	// 1. 创建独立的 Engine（避免全局状态污染）
	engine := remilia.NewEngine()

	// 创建 Bot 并使用独立的 Engine
	bot := remilia.New(
		&dto.BotInfo{
			AppID: 123456,
			Token: "test-token",
		},
		remilia.WithEngine(engine),
	)
	require.NotNil(t, bot)

	// 2. 注册中间件
	var middlewareExecuted bool
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			middlewareExecuted = true
			return next(ctx)
		}
	})

	// 3. 注册 Handler
	var handlerExecuted bool
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		handlerExecuted = true
		return nil
	})

	// 4. 模拟事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-1",
	}
	ctx := remilia.NewContext(event, nil)

	// 5. 处理事件
	engine.ProcessEvent(ctx)

	// 6. 验证结果
	assert.True(t, middlewareExecuted, "Middleware should be executed")
	assert.True(t, handlerExecuted, "Handler should be executed")

	// 7. 优雅关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	bot.Shutdown(shutdownCtx)
}

// TestIntegration_MultipleMatchers 测试多个 Matcher 的优先级和执行顺序
func TestIntegration_MultipleMatchers(t *testing.T) {
	engine := remilia.NewEngine()

	var executionOrder []int

	// 注册不同优先级的 Matcher
	engine.OnC2C().SetPriority(100).HandleE(func(ctx *remilia.Context) error {
		executionOrder = append(executionOrder, 100)
		return nil
	})

	engine.OnC2C().SetPriority(10).HandleE(func(ctx *remilia.Context) error {
		executionOrder = append(executionOrder, 10)
		return nil
	})

	engine.OnC2C().SetPriority(50).HandleE(func(ctx *remilia.Context) error {
		executionOrder = append(executionOrder, 50)
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-priority",
	}
	ctx := remilia.NewContext(event, nil)

	engine.ProcessEvent(ctx)

	// 验证执行顺序（低数字 = 高优先级）
	assert.Equal(t, []int{10, 50, 100}, executionOrder)
}

// TestIntegration_MiddlewareChain 测试中间件链
func TestIntegration_MiddlewareChain(t *testing.T) {
	engine := remilia.NewEngine()

	var executionLog []string

	// 中间件 1
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			executionLog = append(executionLog, "middleware1:before")
			err := next(ctx)
			executionLog = append(executionLog, "middleware1:after")
			return err
		}
	})

	// 中间件 2
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			executionLog = append(executionLog, "middleware2:before")
			err := next(ctx)
			executionLog = append(executionLog, "middleware2:after")
			return err
		}
	})

	// Handler
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		executionLog = append(executionLog, "handler")
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-middleware",
	}
	ctx := remilia.NewContext(event, nil)

	engine.ProcessEvent(ctx)

	// 验证执行顺序
	expected := []string{
		"middleware1:before",
		"middleware2:before",
		"handler",
		"middleware2:after",
		"middleware1:after",
	}
	assert.Equal(t, expected, executionLog)
}

// TestIntegration_AsyncHandlers 测试异步 Handler
func TestIntegration_AsyncHandlers(t *testing.T) {
	engine := remilia.NewEngine()

	var counter int32
	var wg sync.WaitGroup

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		// 使用 Clone 在异步场景中安全使用 Context
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 使用克隆的 context，避免并发访问原始 context
			clonedCtx := ctx.Clone()
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&counter, 1)
			_ = clonedCtx // 使用克隆的 context
		}()
		return nil
	})

	// 处理多个事件
	for i := 0; i < 5; i++ {
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID("test-event-async-" + string(rune('0'+i))),
		}
		ctx := remilia.NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}

	// 等待所有异步任务完成
	wg.Wait()

	// 验证计数
	assert.Equal(t, int32(5), atomic.LoadInt32(&counter))
}

// TestIntegration_BatchProcessing 测试批量处理
func TestIntegration_BatchProcessing(t *testing.T) {
	engine := remilia.NewEngine()

	var processedCount int32

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&processedCount, 1)
		return nil
	})

	// 创建批量事件
	events := make([]*dto.Payload, 100)
	for i := 0; i < 100; i++ {
		events[i] = &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID("batch-event"),
		}
	}

	// 批量处理
	engine.ProcessEventBatch(events, nil)

	// 验证所有事件都被处理
	assert.Equal(t, int32(100), atomic.LoadInt32(&processedCount))
}

// TestIntegration_ContextStateManagement 测试 Context 状态管理
func TestIntegration_ContextStateManagement(t *testing.T) {
	engine := remilia.NewEngine()

	// 中间件设置状态
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			ctx.SetState("middleware_data", "test-value")
			ctx.SetState("counter", 42)
			return next(ctx)
		}
	})

	// Handler 读取状态
	var retrievedString string
	var retrievedInt int

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		if val, ok := ctx.GetState("middleware_data"); ok {
			retrievedString = val.(string)
		}
		if val, ok := ctx.GetState("counter"); ok {
			retrievedInt = val.(int)
		}
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-state",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx)

	// 验证状态
	assert.Equal(t, "test-value", retrievedString)
	assert.Equal(t, 42, retrievedInt)
}

// TestIntegration_TempMatcherLifecycle 测试临时 Matcher 生命周期
func TestIntegration_TempMatcherLifecycle(t *testing.T) {
	engine := remilia.NewEngine()

	var executeCount int32

	// 注册临时 Matcher（最多执行 2 次）
	engine.OnC2C().SetTempWithMaxUse(2).HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&executeCount, 1)
		return nil
	})

	// 第一次执行
	ctx1 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, ID: "1"}, nil)
	engine.ProcessEvent(ctx1)

	// 第二次执行
	ctx2 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, ID: "2"}, nil)
	engine.ProcessEvent(ctx2)

	// 第三次执行（临时 Matcher 应该已被删除）
	ctx3 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, ID: "3"}, nil)
	engine.ProcessEvent(ctx3)

	// 验证只执行了 2 次
	assert.Equal(t, int32(2), atomic.LoadInt32(&executeCount))
}

// TestIntegration_DedupMiddleware 测试去重中间件集成
func TestIntegration_DedupMiddleware(t *testing.T) {
	engine := remilia.NewEngine()

	// 创建去重过滤器
	filter := middleware.NewDedupFilter(middleware.DedupConfig{
		MaxSize:         100,
		DefaultTTL:      time.Minute,
		CleanupInterval: time.Minute,
	})
	defer filter.Stop()

	// 应用去重中间件
	engine.Use(middleware.Dedup(filter))

	var executeCount int32

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&executeCount, 1)
		return nil
	})

	// 处理相同事件 3 次
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "duplicate-event",
	}

	for i := 0; i < 3; i++ {
		ctx := remilia.NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}

	// 验证只执行了 1 次
	assert.Equal(t, int32(1), atomic.LoadInt32(&executeCount))
}

// TestIntegration_ErrorHandling 测试错误处理流程
func TestIntegration_ErrorHandling(t *testing.T) {
	engine := remilia.NewEngine()

	var errorCaught bool

	// 中间件捕获错误
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			err := next(ctx)
			if err != nil {
				errorCaught = true
			}
			return err
		}
	})

	// Handler 返回错误
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		return remilia.ErrHandlerNotSet
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-error",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx)

	// 验证错误被捕获
	assert.True(t, errorCaught)
}

// TestIntegration_ConcurrentEventProcessing 测试并发事件处理
func TestIntegration_ConcurrentEventProcessing(t *testing.T) {
	engine := remilia.NewEngine()

	var processedCount int32

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		time.Sleep(10 * time.Millisecond) // 模拟处理时间
		atomic.AddInt32(&processedCount, 1)
		return nil
	})

	// 并发处理多个事件
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			event := &dto.Payload{
				Type: dto.C2CMessageCreate,
				ID:   dto.EventID("concurrent-event"),
			}
			ctx := remilia.NewContext(event, nil)
			engine.ProcessEvent(ctx)
		}(i)
	}

	wg.Wait()

	// 验证所有事件都被处理
	assert.Equal(t, int32(50), atomic.LoadInt32(&processedCount))
}

// TestIntegration_BlockingMatcher 测试阻断 Matcher
func TestIntegration_BlockingMatcher(t *testing.T) {
	engine := remilia.NewEngine()

	var executionLog []string

	// 第一个 Matcher（阻断）
	engine.OnC2C().SetPriority(10).SetBlock(true).HandleE(func(ctx *remilia.Context) error {
		executionLog = append(executionLog, "blocking-matcher")
		return nil
	})

	// 第二个 Matcher（不应该执行）
	engine.OnC2C().SetPriority(20).HandleE(func(ctx *remilia.Context) error {
		executionLog = append(executionLog, "second-matcher")
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-blocking",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx)

	// 验证只执行了第一个 Matcher
	assert.Equal(t, []string{"blocking-matcher"}, executionLog)
}

// TestIntegration_ContextClone 测试 Context 克隆
func TestIntegration_ContextClone(t *testing.T) {
	engine := remilia.NewEngine()

	var originalID, clonedID string
	var originalState, clonedState string

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		originalID = string(ctx.GetEvent().ID)
		ctx.SetState("test", "original")

		// 克隆 Context
		cloned := ctx.Clone()
		clonedID = string(cloned.GetEvent().ID)

		// 修改克隆的状态不应影响原始 Context
		cloned.SetState("test", "cloned")

		// 验证状态独立
		originalState = ctx.GetString("test")
		clonedState = cloned.GetString("test")

		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-clone",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx)

	// 验证 ID 相同
	assert.Equal(t, originalID, clonedID)

	// 验证状态独立
	assert.Equal(t, "original", originalState)
	assert.Equal(t, "cloned", clonedState)
}

// TestIntegration_PluginIntegration 测试插件集成
func TestIntegration_PluginIntegration(t *testing.T) {
	engine := remilia.NewEngine()

	// 创建基础插件
	plugin := remilia.NewBasePlugin("test-plugin")

	var pluginHandlerExecuted bool

	engine.WithMatcherGroupBatch(func() {
		// 添加 Matcher 到插件
		matcher := engine.OnC2C()
		matcher.HandleE(func(ctx *remilia.Context) error {
			pluginHandlerExecuted = true
			return nil
		})
		plugin.AddMatcher(matcher)
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-plugin",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx)

	// 验证插件 Handler 被执行
	assert.True(t, pluginHandlerExecuted)

	// 卸载插件
	err := plugin.Unload(engine)
	assert.NoError(t, err)

	// 卸载后不应再执行
	pluginHandlerExecuted = false
	ctx2 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate, ID: "2"}, nil)
	engine.ProcessEvent(ctx2)
	assert.False(t, pluginHandlerExecuted)
}

// TestIntegration_CircuitBreakerMiddleware 测试熔断器中间件
func TestIntegration_CircuitBreakerMiddleware(t *testing.T) {
	engine := remilia.NewEngine()

	// 创建熔断器
	cb := middleware.NewCircuitBreaker(middleware.CircuitBreakerConfig{
		MaxFailures:         3,
		ResetTimeout:        time.Second,
		HalfOpenMaxRequests: 1,
	})

	// 应用熔断器中间件
	engine.Use(middleware.CircuitBreakerMiddleware(cb))

	var executeCount int32

	// Handler 总是失败
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&executeCount, 1)
		return assert.AnError
	})

	// 触发多次失败
	for i := 0; i < 5; i++ {
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID("cb-event"),
		}
		ctx := remilia.NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}

	// 验证熔断器开启后，handler 不再被执行
	// 前 3 次失败触发熔断，后 2 次被熔断器拦截
	assert.LessOrEqual(t, atomic.LoadInt32(&executeCount), int32(3))
	assert.Equal(t, middleware.StateOpen, cb.GetState())
}
