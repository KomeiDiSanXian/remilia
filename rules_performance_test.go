package remilia

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestWithTimeout 测试超时包装器
func TestWithTimeout(t *testing.T) {
	t.Run("快速规则正常返回", func(t *testing.T) {
		fastRule := func(ctx *Context) bool {
			return true
		}

		wrappedRule := WithTimeout(fastRule, 100*time.Millisecond)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		result := wrappedRule(ctx)

		assert.True(t, result)
	})

	t.Run("慢规则超时返回 false", func(t *testing.T) {
		slowRule := func(ctx *Context) bool {
			time.Sleep(200 * time.Millisecond) // 慢规则
			return true
		}

		wrappedRule := WithTimeout(slowRule, 50*time.Millisecond)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		start := time.Now()
		result := wrappedRule(ctx)
		duration := time.Since(start)

		assert.False(t, result)                          // 超时返回 false
		assert.Less(t, duration, 100*time.Millisecond)   // 在超时时间附近返回
		assert.Greater(t, duration, 50*time.Millisecond) // 至少等待了超时时间
	})

	t.Run("规则 panic 应该返回 false", func(t *testing.T) {
		panicRule := func(ctx *Context) bool {
			panic("intentional panic")
		}

		wrappedRule := WithTimeout(panicRule, 100*time.Millisecond)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		result := wrappedRule(ctx)

		assert.False(t, result) // panic 后返回 false
	})

	t.Run("超时后 goroutine 仍在运行", func(t *testing.T) {
		var executed atomic.Bool
		slowRule := func(ctx *Context) bool {
			time.Sleep(100 * time.Millisecond)
			executed.Store(true) // 即使超时，这里仍会执行
			return true
		}

		wrappedRule := WithTimeout(slowRule, 10*time.Millisecond)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		result := wrappedRule(ctx)

		assert.False(t, result) // 超时返回 false

		// 等待 goroutine 完成
		time.Sleep(150 * time.Millisecond)
		assert.True(t, executed.Load()) // goroutine 仍然执行完成了
	})
}

// TestMonitorRule 测试规则监控
func TestMonitorRule(t *testing.T) {
	t.Run("快速规则不记录警告", func(t *testing.T) {
		fastRule := func(ctx *Context) bool {
			return true
		}

		monitoredRule := MonitorRule("fastRule", fastRule, 10*time.Millisecond)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		result := monitoredRule(ctx)

		assert.True(t, result)
		// 快速规则不会记录警告（检查日志）
	})

	t.Run("慢规则记录警告", func(t *testing.T) {
		slowRule := func(ctx *Context) bool {
			time.Sleep(20 * time.Millisecond) // 慢于阈值
			return true
		}

		monitoredRule := MonitorRule("slowRule", slowRule, 10*time.Millisecond)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		result := monitoredRule(ctx)

		assert.True(t, result) // 仍然返回正确结果
		// 应该记录警告日志（需要手动检查）
	})

	t.Run("使用默认阈值", func(t *testing.T) {
		rule := func(ctx *Context) bool {
			time.Sleep(5 * time.Millisecond)
			return true
		}

		// 不指定阈值，使用默认 10ms
		monitoredRule := MonitorRule("rule", rule, 0)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		result := monitoredRule(ctx)

		assert.True(t, result)
	})
}

// TestRulePerformance 测试规则性能
func TestRulePerformance(t *testing.T) {
	t.Run("简单规则应该很快", func(t *testing.T) {
		rule := func(ctx *Context) bool {
			return ctx.GetString("type") == "message"
		}

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		ctx.SetState("type", "message")

		start := time.Now()
		for i := 0; i < 1000; i++ {
			rule(ctx)
		}
		duration := time.Since(start)

		// 1000 次调用应该在 10ms 内完成
		assert.Less(t, duration, 10*time.Millisecond)
		t.Logf("1000 calls took %v (avg: %v)", duration, duration/1000)
	})
}

// TestWithTimeoutInEngine 测试在 Engine 中使用 WithTimeout
func TestWithTimeoutInEngine(t *testing.T) {
	t.Run("正常规则", func(t *testing.T) {
		engine := NewEngine()

		var executed bool
		engine.OnC2C().Handle(func(ctx *Context) {
			executed = true
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		assert.True(t, executed)
	})

	t.Run("慢规则使用超时保护", func(t *testing.T) {
		engine := NewEngine()

		slowRule := func(ctx *Context) bool {
			time.Sleep(100 * time.Millisecond)
			return true
		}

		var executed bool
		engine.OnC2C(WithTimeout(slowRule, 50*time.Millisecond)).Handle(func(ctx *Context) {
			executed = true
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 由于规则超时返回 false，Handler 不应该执行
		assert.False(t, executed)
	})
}

// BenchmarkRulePerformance 基准测试
func BenchmarkRulePerformance(b *testing.B) {
	ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	ctx.SetState("type", "message")

	b.Run("SimpleRule", func(b *testing.B) {
		rule := func(ctx *Context) bool {
			return ctx.GetString("type") == "message"
		}
		for i := 0; i < b.N; i++ {
			rule(ctx)
		}
	})

	b.Run("WithTimeout", func(b *testing.B) {
		rule := func(ctx *Context) bool {
			return ctx.GetString("type") == "message"
		}
		wrappedRule := WithTimeout(rule, 100*time.Millisecond)
		for i := 0; i < b.N; i++ {
			wrappedRule(ctx)
		}
	})

	b.Run("MonitorRule", func(b *testing.B) {
		rule := func(ctx *Context) bool {
			return ctx.GetString("type") == "message"
		}
		monitoredRule := MonitorRule("test", rule, 10*time.Millisecond)
		for i := 0; i < b.N; i++ {
			monitoredRule(ctx)
		}
	})
}
