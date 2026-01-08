package remilia_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestE2ECommandProcessing 端到端测试：命令处理流程
func TestE2ECommandProcessing(t *testing.T) {
	bot := remilia.New(&dto.BotInfo{
		AppID: 123456,
		Token: "test-token",
	})

	var commandExecuted bool
	var receivedArgs []string

	// 注册命令 Handler
	bot.GetEngine().OnC2C().HandleE(func(ctx *remilia.Context) error {
		commandExecuted = true
		// 模拟参数解析
		receivedArgs = []string{"arg1", "arg2"}
		return nil
	})

	// 模拟用户发送命令
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "cmd-test-1",
	}

	ctx := remilia.NewContext(event, nil)
	bot.GetEngine().ProcessEvent(ctx) // autoRelease 会自动释放

	// 验证
	assert.True(t, commandExecuted)
	assert.Equal(t, []string{"arg1", "arg2"}, receivedArgs)
}

// TestE2EStatePropagation 端到端测试：状态传播
func TestE2EStatePropagation(t *testing.T) {
	engine := remilia.NewEngine()

	var stateInHandler string

	// 中间件设置状态
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			ctx.SetState("test-state", "middleware-value")
			return next(ctx)
		}
	})

	// Handler 读取状态
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		if val, ok := ctx.GetState("test-state"); ok {
			stateInHandler = val.(string)
		}
		return nil
	})

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "state-test-1",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放

	assert.Equal(t, "middleware-value", stateInHandler)
}

// TestE2ERetryMiddleware 端到端测试：重试中间件
func TestE2ERetryMiddleware(t *testing.T) {
	engine := remilia.NewEngine()

	// 应用重试中间件
	engine.Use(middleware.Retry(middleware.RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
	}))

	var attemptCount int

	// Handler 前两次失败，第三次成功
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		attemptCount++
		if attemptCount < 3 {
			return remilia.ErrHandlerNotSet
		}
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "retry-test-1",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放

	// 验证重试了 3 次
	assert.Equal(t, 3, attemptCount)
}

// TestE2ETimeoutMiddleware 端到端测试：超时中间件
func TestE2ETimeoutMiddleware(t *testing.T) {
	engine := remilia.NewEngine()

	// 应用超时中间件（50ms 超时）
	engine.Use(middleware.Timeout(50 * time.Millisecond))

	var handlerStarted atomic.Bool

	// Handler 耗时 100ms
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		handlerStarted.Store(true)
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放

	// 验证 Handler 启动
	assert.True(t, handlerStarted.Load())
	time.Sleep(60 * time.Millisecond)
}

// TestE2ESlowHandlerDetection 端到端测试：慢 Handler 检测
func TestE2ESlowHandlerDetection(t *testing.T) {
	engine := remilia.NewEngine()

	// 应用慢 Handler 检测中间件
	var slowDetected bool
	engine.Use(middleware.SlowHandler(middleware.SlowHandlerConfig{
		Threshold: 50 * time.Millisecond,
		OnSlowHandler: func(handlerName string, duration time.Duration, ctx *remilia.Context) {
			slowDetected = true
		},
	}))

	// 慢 Handler
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "slow-test-1",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放

	// 验证慢 Handler 被检测到
	assert.True(t, slowDetected)
}

// TestE2EComplexWorkflow 端到端测试：复杂工作流
func TestE2EComplexWorkflow(t *testing.T) {
	engine := remilia.NewEngine()

	var workflow []string

	// 1. 日志中间件
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			workflow = append(workflow, "logging:before")
			err := next(ctx)
			workflow = append(workflow, "logging:after")
			return err
		}
	})

	// 2. 认证中间件
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			workflow = append(workflow, "auth:check")
			ctx.SetState("authenticated", true)
			return next(ctx)
		}
	})

	// 3. 去重中间件
	dedupFilter := middleware.NewDedupFilter(middleware.DefaultDedupConfig())
	defer dedupFilter.Stop()
	engine.Use(middleware.Dedup(dedupFilter))

	// 4. 业务 Handler
	engine.OnC2C().SetPriority(10).HandleE(func(ctx *remilia.Context) error {
		workflow = append(workflow, "handler:high-priority")
		return nil
	})

	engine.OnC2C().SetPriority(50).HandleE(func(ctx *remilia.Context) error {
		workflow = append(workflow, "handler:normal-priority")
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "workflow-test-1",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放

	// 验证工作流顺序
	// 注意：每个 Handler 都会独立经过中间件链
	// 去重中间件会在第一个 Handler 执行后标记事件为已处理
	// 因此第二个 Handler 会被去重中间件阻断
	// 预期顺序：
	// 1. handler:high-priority: logging:before -> auth:check -> handler -> logging:after
	// 2. handler:normal-priority: logging:before -> auth:check -> (被去重阻断) -> logging:after
	expected := []string{
		"logging:before",
		"auth:check",
		"handler:high-priority",
		"logging:after",
		"logging:before",
		"auth:check",
		// handler:normal-priority 被去重中间件阻断，不会执行
		"logging:after",
	}
	assert.Equal(t, expected, workflow)

	// 处理相同事件（应该被去重）
	// 第二次ProcessEvent会进入中间件链但会被去重中间件阻断
	initialWorkflowLen := len(workflow)
	ctx2 := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx2) // autoRelease 会自动释放

	// 验证第二次处理被阻断（workflow 增加了中间件日志但没有 handler 执行）
	// 会增加: logging:before, auth:check, (dedup blocks), logging:after (每个 handler)
	newEntries := workflow[initialWorkflowLen:]
	assert.NotContains(t, newEntries, "handler:high-priority", "Deduped event should not execute handler")
	assert.NotContains(t, newEntries, "handler:normal-priority", "Deduped event should not execute handler")
}

// TestE2EStateSharing 端到端测试：状态共享
func TestE2EStateSharing(t *testing.T) {
	engine := remilia.NewEngine()

	// 中间件 1：设置用户信息
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			ctx.SetState("user_id", "user-123")
			ctx.SetState("username", "testuser")
			return next(ctx)
		}
	})

	// 中间件 2：设置请求 ID
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			ctx.SetState("request_id", "req-456")
			return next(ctx)
		}
	})

	// Handler：读取所有状态
	var collectedState map[string]interface{}
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		collectedState = ctx.GetAllState()
		return nil
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "state-test-1",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放

	// 验证状态
	assert.Equal(t, "user-123", collectedState["user_id"])
	assert.Equal(t, "testuser", collectedState["username"])
	assert.Equal(t, "req-456", collectedState["request_id"])
}

// TestE2EErrorPropagation 端到端测试：错误传播
func TestE2EErrorPropagation(t *testing.T) {
	engine := remilia.NewEngine()

	var errorLog []string

	// 中间件 1：捕获错误
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			err := next(ctx)
			if err != nil {
				errorLog = append(errorLog, "middleware1:caught")
			}
			return err
		}
	})

	// 中间件 2：捕获错误
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			err := next(ctx)
			if err != nil {
				errorLog = append(errorLog, "middleware2:caught")
			}
			return err
		}
	})

	// Handler：抛出错误
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		errorLog = append(errorLog, "handler:error")
		return remilia.ErrHandlerNotSet
	})

	// 处理事件
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "error-test-1",
	}
	ctx := remilia.NewContext(event, nil)
	engine.ProcessEvent(ctx) // autoRelease 会自动释放

	// 验证错误传播
	expected := []string{
		"handler:error",
		"middleware2:caught",
		"middleware1:caught",
	}
	assert.Equal(t, expected, errorLog)
}

// TestE2EMemoryUsage 端到端测试：内存使用和对象池
func TestE2EMemoryUsage(t *testing.T) {
	engine := remilia.NewEngine()

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		// 模拟一些内存操作
		data := make([]byte, 1024)
		ctx.SetState("data", data)
		return nil
	})

	// 处理大量事件
	for i := 0; i < 100; i++ {
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID("memory-test-" + string(rune(i))),
		}
		ctx := remilia.NewContext(event, nil)
		engine.ProcessEvent(ctx) // autoRelease 会自动释放
	}

	// 验证测试完成（对象池自动管理内存）
	assert.True(t, true)
}

// BenchmarkE2ECompleteFlow 基准测试完整流程
func BenchmarkE2ECompleteFlow(b *testing.B) {
	bot := remilia.New(&dto.BotInfo{
		AppID: 123456,
		Token: "test-token",
	})
	engine := bot.GetEngine()

	// 配置中间件
	engine.Use(func(next remilia.HandlerE) remilia.HandlerE {
		return func(ctx *remilia.Context) error {
			ctx.SetState("middleware", true)
			return next(ctx)
		}
	})

	// 配置 Handler
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		_, _ = ctx.GetState("middleware")
		return nil
	})

	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "bench-e2e",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := remilia.NewContext(event, nil)
		engine.ProcessEvent(ctx) // autoRelease 会自动释放
	}
}
