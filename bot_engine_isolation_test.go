package remilia

import (
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestNew_IndependentEngines(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	// 创建两个 Bot 实例
	bot1 := New(info)
	bot2 := New(info)

	// 验证每个 Bot 都有自己的 Engine
	engine1 := bot1.GetEngine()
	engine2 := bot2.GetEngine()

	assert.NotNil(t, engine1)
	assert.NotNil(t, engine2)
	assert.NotSame(t, engine1, engine2, "Each bot should have independent engine")
}

func TestNew_IsolatedMatchers(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	// 创建两个 Bot 实例
	bot1 := New(info)
	bot2 := New(info)

	engine1 := bot1.GetEngine()
	engine2 := bot2.GetEngine()

	// 在第一个 Engine 添加 Matcher
	engine1.On(dto.C2CMessageCreate).Handle(func(ctx *Context) {})

	// 验证 Matcher 隔离
	assert.Equal(t, 1, engine1.GetMatcherCount())
	assert.Equal(t, 0, engine2.GetMatcherCount(), "Matchers should be isolated between engines")
}

func TestNew_WithCustomEngine(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	customEngine := NewEngine()

	// New() 应该允许通过 WithEngine 使用自定义 Engine
	bot := New(info, WithEngine(customEngine))

	// 验证使用的是传入的 Engine
	assert.Same(t, bot.GetEngine(), customEngine, "New() should use provided custom engine")
}

func TestNew_AutoCreatesEngine(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	// New() 应该自动创建 Engine
	bot := New(info)

	assert.NotNil(t, bot.GetEngine(), "New() should auto-create Engine")
}

func TestEngineIsolation_Middleware(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	bot1 := New(info)
	bot2 := New(info)

	engine1 := bot1.GetEngine()
	engine2 := bot2.GetEngine()

	// 在第一个 Engine 添加中间件
	middleware1Called := false
	engine1.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			middleware1Called = true
			return next(ctx)
		}
	})

	// 第二个 Engine 应该不受影响
	engine2.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		return nil
	})

	// 模拟事件
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	engine2.ProcessEvent(ctx)

	assert.False(t, middleware1Called, "Engine 2 should not be affected by Engine 1's middleware")
}

func TestEngineIsolation_MetricsCollector(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	bot1 := New(info)
	bot2 := New(info)

	engine1 := bot1.GetEngine()
	engine2 := bot2.GetEngine()

	// 为第一个 Engine 设置指标收集器
	mc1 := NewMetricsCollector("bot1")
	engine1.SetMetricsCollector(mc1)

	// 第二个 Engine 应该没有指标收集器
	assert.NotNil(t, engine1.GetMetricsCollector())
	assert.Nil(t, engine2.GetMetricsCollector())
}

func TestEngineIsolation_TempMatcherCleaner(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	bot1 := New(info)
	bot2 := New(info)

	engine1 := bot1.GetEngine()
	engine2 := bot2.GetEngine()

	// 两个 Engine 应该有独立的清理间隔
	engine1.SetTempMatcherCleanInterval(10 * time.Minute)
	engine2.SetTempMatcherCleanInterval(5 * time.Minute)

	assert.Equal(t, 10*time.Minute, engine1.GetTempMatcherCleanInterval())
	assert.Equal(t, 5*time.Minute, engine2.GetTempMatcherCleanInterval())
}

func TestMultipleBots_IndependentExecution(t *testing.T) {
	info := &dto.BotInfo{
		AppID: 123,
	}

	bot1 := New(info)
	bot2 := New(info)

	engine1 := bot1.GetEngine()
	engine2 := bot2.GetEngine()

	// 两个 Bot 的计数器
	bot1Count := 0
	bot2Count := 0

	// 添加不同的 handler
	engine1.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		bot1Count++
		return nil
	})

	engine2.On(dto.C2CMessageCreate).HandleE(func(ctx *Context) error {
		bot2Count++
		return nil
	})

	// 模拟事件
	event := &dto.Payload{Type: dto.C2CMessageCreate}

	ctx1 := NewContext(event, nil)

	engine1.ProcessEvent(ctx1)

	ctx2 := NewContext(event, nil)

	engine2.ProcessEvent(ctx2)

	// 验证计数器独立
	assert.Equal(t, 1, bot1Count)
	assert.Equal(t, 1, bot2Count)
}
