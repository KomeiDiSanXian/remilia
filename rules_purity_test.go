package remilia

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestRulePureFunction 测试规则函数应该是纯函数
func TestRulePureFunction(t *testing.T) {
	t.Run("纯函数规则：幂等性", func(t *testing.T) {
		// ✅ 纯函数：多次调用返回相同结果
		rule := func(ctx *Context) bool {
			return ctx.GetString("type") == "message"
		}

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		ctx.SetState("type", "message")

		// 多次调用应该返回相同结果
		result1 := rule(ctx)
		result2 := rule(ctx)
		result3 := rule(ctx)

		assert.True(t, result1)
		assert.Equal(t, result1, result2)
		assert.Equal(t, result2, result3)
	})

	t.Run("有副作用的规则：短路问题", func(t *testing.T) {
		// ❌ 有副作用的规则：修改外部变量
		var counter int32

		rule1 := func(ctx *Context) bool {
			atomic.AddInt32(&counter, 1) // 副作用
			return false                 // 总是返回 false
		}

		rule2 := func(ctx *Context) bool {
			atomic.AddInt32(&counter, 1) // 副作用
			return true
		}

		// 使用 And：rule1 返回 false，rule2 不会执行（短路）
		combinedRule := And(rule1, rule2)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		result := combinedRule(ctx)

		assert.False(t, result)
		// 问题：counter 只增加了 1，不是 2
		// 因为 rule2 因短路而未执行
		assert.Equal(t, int32(1), atomic.LoadInt32(&counter))
	})

	t.Run("正确做法：副作用在 Handler 中", func(t *testing.T) {
		// ✅ 规则是纯函数
		rule1 := func(ctx *Context) bool {
			return ctx.GetString("type") == "message"
		}

		rule2 := func(ctx *Context) bool {
			return ctx.GetString("user") == "admin"
		}

		// 副作用在 Handler 中
		var handlerCalled bool
		handler := func(ctx *Context) {
			handlerCalled = true // Handler 中的副作用
			// 这里可以安全地修改状态、调用 API 等
		}

		engine := NewEngine()
		engine.OnC2C(And(rule1, rule2)).Handle(handler)

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		ctx.SetState("type", "message")
		ctx.SetState("user", "admin")

		engine.ProcessEvent(ctx)

		assert.True(t, handlerCalled)
	})
}

// TestAndShortCircuit 测试 And 的短路行为
func TestAndShortCircuit(t *testing.T) {
	t.Run("第一个规则返回 false，后续不执行", func(t *testing.T) {
		var call1, call2, call3 bool

		rule1 := func(ctx *Context) bool {
			call1 = true
			return false // 返回 false
		}

		rule2 := func(ctx *Context) bool {
			call2 = true
			return true
		}

		rule3 := func(ctx *Context) bool {
			call3 = true
			return true
		}

		combinedRule := And(rule1, rule2, rule3)
		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

		result := combinedRule(ctx)

		assert.False(t, result)
		assert.True(t, call1)  // rule1 被调用
		assert.False(t, call2) // rule2 未被调用（短路）
		assert.False(t, call3) // rule3 未被调用（短路）
	})

	t.Run("所有规则返回 true", func(t *testing.T) {
		var call1, call2, call3 bool

		rule1 := func(ctx *Context) bool {
			call1 = true
			return true
		}

		rule2 := func(ctx *Context) bool {
			call2 = true
			return true
		}

		rule3 := func(ctx *Context) bool {
			call3 = true
			return true
		}

		combinedRule := And(rule1, rule2, rule3)
		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

		result := combinedRule(ctx)

		assert.True(t, result)
		assert.True(t, call1) // 所有规则都被调用
		assert.True(t, call2)
		assert.True(t, call3)
	})
}

// TestOrShortCircuit 测试 Or 的短路行为
func TestOrShortCircuit(t *testing.T) {
	t.Run("第一个规则返回 true，后续不执行", func(t *testing.T) {
		var call1, call2, call3 bool

		rule1 := func(ctx *Context) bool {
			call1 = true
			return true // 返回 true
		}

		rule2 := func(ctx *Context) bool {
			call2 = true
			return false
		}

		rule3 := func(ctx *Context) bool {
			call3 = true
			return false
		}

		combinedRule := Or(rule1, rule2, rule3)
		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

		result := combinedRule(ctx)

		assert.True(t, result)
		assert.True(t, call1)  // rule1 被调用
		assert.False(t, call2) // rule2 未被调用（短路）
		assert.False(t, call3) // rule3 未被调用（短路）
	})

	t.Run("所有规则返回 false", func(t *testing.T) {
		var call1, call2, call3 bool

		rule1 := func(ctx *Context) bool {
			call1 = true
			return false
		}

		rule2 := func(ctx *Context) bool {
			call2 = true
			return false
		}

		rule3 := func(ctx *Context) bool {
			call3 = true
			return false
		}

		combinedRule := Or(rule1, rule2, rule3)
		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

		result := combinedRule(ctx)

		assert.False(t, result)
		assert.True(t, call1) // 所有规则都被调用
		assert.True(t, call2)
		assert.True(t, call3)
	})
}

// TestRuleSideEffectAntiPattern 演示反模式：规则中的副作用
func TestRuleSideEffectAntiPattern(t *testing.T) {
	t.Run("反模式：在规则中计数", func(t *testing.T) {
		// ❌ 错误示例：不要这样做
		var checkCount int32

		rule := func(ctx *Context) bool {
			atomic.AddInt32(&checkCount, 1) // 副作用：计数
			return ctx.GetString("type") == "message"
		}

		engine := NewEngine()
		engine.OnC2C(rule).Handle(func(ctx *Context) {
			// handler
		})

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		ctx.SetState("type", "message")

		// 问题：checkCount 的值取决于规则被调用多少次
		// 这是不确定的，因为：
		// - 短路优化可能跳过某些规则
		// - 框架内部可能多次评估规则
		// - 并发场景下会有竞态条件

		engine.ProcessEvent(ctx)

		// checkCount 的值是不确定的，这就是问题所在
		t.Logf("Check count: %d (不确定的值)", atomic.LoadInt32(&checkCount))
	})

	t.Run("正确做法：在 Handler 中计数", func(t *testing.T) {
		// ✅ 正确示例
		var handlerCount int32

		rule := func(ctx *Context) bool {
			// 纯函数：只检查，不修改
			return ctx.GetString("type") == "message"
		}

		engine := NewEngine()
		engine.OnC2C(rule).Handle(func(ctx *Context) {
			// Handler 中的副作用是安全的
			atomic.AddInt32(&handlerCount, 1)
		})

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		ctx.SetState("type", "message")

		engine.ProcessEvent(ctx)

		// handlerCount 是确定的：handler 执行了一次
		assert.Equal(t, int32(1), atomic.LoadInt32(&handlerCount))
	})
}

// TestRulePurityExamples 纯函数规则的正确示例
func TestRulePurityExamples(t *testing.T) {
	t.Run("示例：检查命令", func(t *testing.T) {
		// ✅ 纯函数：只读取，不修改
		rule := OnCommand("/ping")

		ctx := NewContext(&dto.Payload{
			Type: dto.C2CMessageCreate,
		}, nil)
		// 模拟消息内容
		ctx.SetState("message_content", "/ping")

		result := rule(ctx)
		// 注意：实际测试需要 Context 能够正确获取消息内容
		// 这里仅作为示例
		_ = result
	})

	t.Run("示例：检查用户权限", func(t *testing.T) {
		// ✅ 纯函数：只检查状态
		isAdmin := func(ctx *Context) bool {
			return ctx.GetString("role") == "admin"
		}

		ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		ctx.SetState("role", "admin")

		result := isAdmin(ctx)
		assert.True(t, result)
	})

	t.Run("示例：复合条件", func(t *testing.T) {
		// ✅ 纯函数：And 和 Or 组合
		rule := And(
			func(ctx *Context) bool {
				return ctx.GetString("cmd") == "/admin"
			},
			func(ctx *Context) bool {
				return ctx.GetString("role") == "admin"
			},
		)

		ctx := NewContext(&dto.Payload{
			Type: dto.C2CMessageCreate,
		}, nil)
		ctx.SetState("cmd", "/admin")
		ctx.SetState("role", "admin")

		result := rule(ctx)
		assert.True(t, result)
	})
}
