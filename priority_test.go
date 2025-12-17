package remilia

import (
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestMatcherPrioritySorting 测试 Matcher 优先级排序
func TestMatcherPrioritySorting(t *testing.T) {
	t.Run("按优先级顺序执行", func(t *testing.T) {
		engine := NewEngine()

		var executionOrder []int

		// 注册不同优先级的 matchers（倒序注册）
		m3 := engine.OnC2C()
		m3.SetPriority(30)
		m3.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, 30)
		})

		m1 := engine.OnC2C()
		m1.SetPriority(10)
		m1.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, 10)
		})

		m2 := engine.OnC2C()
		m2.SetPriority(20)
		m2.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, 20)
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 验证执行顺序：10 -> 20 -> 30
		assert.Equal(t, []int{10, 20, 30}, executionOrder)
	})

	t.Run("优先级相同时保持注册顺序", func(t *testing.T) {
		engine := NewEngine()

		var executionOrder []string

		m1 := engine.OnC2C()
		m1.SetPriority(10)
		m1.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, "first")
		})

		m2 := engine.OnC2C()
		m2.SetPriority(10) // 相同优先级
		m2.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, "second")
		})

		m3 := engine.OnC2C()
		m3.SetPriority(10) // 相同优先级
		m3.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, "third")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 优先级相同时按注册顺序执行
		assert.Equal(t, []string{"first", "second", "third"}, executionOrder)
	})

	t.Run("Priority = 0 优先级最高", func(t *testing.T) {
		engine := NewEngine()

		var executionOrder []int

		m1 := engine.OnC2C()
		m1.SetPriority(50)
		m1.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, 50)
		})

		m2 := engine.OnC2C()
		m2.SetPriority(0) // 最高优先级
		m2.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, 0)
		})

		m3 := engine.OnC2C()
		m3.SetPriority(10)
		m3.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, 10)
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 验证执行顺序：0 -> 10 -> 50
		assert.Equal(t, []int{0, 10, 50}, executionOrder)
	})

	t.Run("默认优先级 = 50", func(t *testing.T) {
		engine := NewEngine()

		m := engine.OnC2C()
		// 不设置 Priority，使用默认值

		assert.Equal(t, uint(50), m.Priority)
	})
}

// TestPriorityWithIsBlock 测试优先级与 Block 的交互
func TestPriorityWithIsBlock(t *testing.T) {
	t.Run("高优先级 Block 阻止低优先级执行", func(t *testing.T) {
		engine := NewEngine()

		var executed []string

		m1 := engine.OnC2C()
		m1.SetPriority(10)
		m1.SetBlock(true)
		m1.Handle(func(ctx *Context) {
			executed = append(executed, "high-block")
		})

		m2 := engine.OnC2C()
		m2.SetPriority(20)
		m2.Handle(func(ctx *Context) {
			executed = append(executed, "low")
		})

		m3 := engine.OnC2C()
		m3.SetPriority(30)
		m3.Handle(func(ctx *Context) {
			executed = append(executed, "lowest")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 只有 high-block 执行
		assert.Equal(t, []string{"high-block"}, executed)
	})

	t.Run("低优先级 matcher 不影响高优先级", func(t *testing.T) {
		engine := NewEngine()

		var executed []string

		m1 := engine.OnC2C()
		m1.SetPriority(10)
		m1.Handle(func(ctx *Context) {
			executed = append(executed, "high")
		})

		m2 := engine.OnC2C()
		m2.SetPriority(20)
		m2.SetBlock(true)
		m2.Handle(func(ctx *Context) {
			executed = append(executed, "low-block")
		})

		m3 := engine.OnC2C()
		m3.SetPriority(30)
		m3.Handle(func(ctx *Context) {
			executed = append(executed, "lowest")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// high 和 low-block 执行，lowest 被阻止
		assert.Equal(t, []string{"high", "low-block"}, executed)
	})
}

// TestPriorityWithRules 测试优先级与规则匹配
func TestPriorityWithRules(t *testing.T) {
	t.Run("高优先级先匹配", func(t *testing.T) {
		engine := NewEngine()

		var executionOrder []string

		// 低优先级，匹配所有
		m1 := engine.OnC2C()
		m1.SetPriority(50)
		m1.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, "low")
		})

		// 高优先级，也匹配所有
		m2 := engine.OnC2C()
		m2.SetPriority(10)
		m2.Handle(func(ctx *Context) {
			executionOrder = append(executionOrder, "high")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 高优先级先执行，然后低优先级
		assert.Equal(t, []string{"high", "low"}, executionOrder)
	})

	t.Run("高优先级不匹配，低优先级执行", func(t *testing.T) {
		engine := NewEngine()

		var executed []string

		// 高优先级，但规则不匹配
		m1 := engine.OnC2C(OnCommand("/test"))
		m1.SetPriority(10)
		m1.Handle(func(ctx *Context) {
			executed = append(executed, "high")
		})

		// 低优先级，匹配所有
		m2 := engine.OnC2C()
		m2.SetPriority(50)
		m2.Handle(func(ctx *Context) {
			executed = append(executed, "low")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 只有 low 执行（high 规则不匹配）
		assert.Equal(t, []string{"low"}, executed)
	})
}

// TestPriorityWithMixedEventTypes 测试不同事件类型的优先级
func TestPriorityWithMixedEventTypes(t *testing.T) {
	t.Run("同事件类型按优先级排序", func(t *testing.T) {
		engine := NewEngine()

		var executed []int

		m1 := engine.OnC2C()
		m1.SetPriority(30)
		m1.Handle(func(ctx *Context) {
			executed = append(executed, 30)
		})

		m2 := engine.OnC2C()
		m2.SetPriority(10)
		m2.Handle(func(ctx *Context) {
			executed = append(executed, 10)
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		assert.Equal(t, []int{10, 30}, executed)
	})

	t.Run("通用匹配器和特定类型混合", func(t *testing.T) {
		engine := NewEngine()

		var executed []string

		// 通用匹配器
		m1 := engine.OnAny()
		m1.SetPriority(5)
		m1.Handle(func(ctx *Context) {
			executed = append(executed, "any-high")
		})

		// C2C 特定类型
		m2 := engine.OnC2C()
		m2.SetPriority(10)
		m2.Handle(func(ctx *Context) {
			executed = append(executed, "c2c")
		})

		// 通用匹配器（低优先级）
		m3 := engine.OnAny()
		m3.SetPriority(50)
		m3.Handle(func(ctx *Context) {
			executed = append(executed, "any-low")
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := NewContext(event, nil)

		engine.ProcessEvent(ctx)

		// 按优先级顺序执行
		assert.Equal(t, []string{"any-high", "c2c", "any-low"}, executed)
	})
}

// TestPriorityConcurrent 测试并发场景下的优先级
func TestPriorityConcurrent(t *testing.T) {
	t.Run("并发处理事件时优先级正确", func(t *testing.T) {
		engine := NewEngine()

		var counter atomic.Int32

		// 注册多个不同优先级的 matcher
		for i := 0; i < 10; i++ {
			priority := uint(i * 10)
			engine.OnC2C().SetPriority(priority).Handle(func(ctx *Context) {
				counter.Add(1)
			})
		}

		// 并发处理多个事件
		const numEvents = 100
		done := make(chan struct{})

		for i := 0; i < numEvents; i++ {
			go func() {
				event := &dto.Payload{Type: dto.C2CMessageCreate}
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
				done <- struct{}{}
			}()
		}

		for i := 0; i < numEvents; i++ {
			<-done
		}

		// 每个事件应该触发所有 10 个 matcher
		assert.Equal(t, int32(numEvents*10), counter.Load())
	})
}
