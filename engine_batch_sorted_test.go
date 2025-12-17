package remilia

import (
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestProcessEventBatchUsesSortedCache 测试 ProcessEventBatch 使用排序缓存
func TestProcessEventBatchUsesSortedCache(t *testing.T) {
	engine := NewEngine()

	var executionOrder []int

	// 注册不同优先级的 matcher
	engine.OnC2C().SetPriority(100).HandleE(func(ctx *Context) error {
		executionOrder = append(executionOrder, 100)
		return nil
	})

	engine.OnC2C().SetPriority(10).HandleE(func(ctx *Context) error {
		executionOrder = append(executionOrder, 10)
		return nil
	})

	engine.OnC2C().SetPriority(50).HandleE(func(ctx *Context) error {
		executionOrder = append(executionOrder, 50)
		return nil
	})

	// 批量处理事件
	events := []*dto.Payload{
		{Type: dto.C2CMessageCreate, ID: "1"},
		{Type: dto.C2CMessageCreate, ID: "2"},
	}

	engine.ProcessEventBatch(events, nil)

	// 验证按优先级排序执行（低数字 = 高优先级）
	expected := []int{10, 50, 100, 10, 50, 100}
	assert.Equal(t, expected, executionOrder, "should execute in priority order for each event")
}

// TestProcessEventBatchConsistentWithProcessEvent 测试批量处理与单个处理行为一致
func TestProcessEventBatchConsistentWithProcessEvent(t *testing.T) {
	// 测试单个处理
	engine1 := NewEngine()
	var order1 []int

	engine1.OnC2C().SetPriority(30).HandleE(func(ctx *Context) error {
		order1 = append(order1, 30)
		return nil
	})
	engine1.OnC2C().SetPriority(10).HandleE(func(ctx *Context) error {
		order1 = append(order1, 10)
		return nil
	})
	engine1.OnC2C().SetPriority(20).HandleE(func(ctx *Context) error {
		order1 = append(order1, 20)
		return nil
	})

	ctx1 := NewContext(&dto.Payload{Type: dto.C2CMessageCreate, ID: "1"}, nil)
	engine1.ProcessEvent(ctx1)

	// 测试批量处理
	engine2 := NewEngine()
	var order2 []int

	engine2.OnC2C().SetPriority(30).HandleE(func(ctx *Context) error {
		order2 = append(order2, 30)
		return nil
	})
	engine2.OnC2C().SetPriority(10).HandleE(func(ctx *Context) error {
		order2 = append(order2, 10)
		return nil
	})
	engine2.OnC2C().SetPriority(20).HandleE(func(ctx *Context) error {
		order2 = append(order2, 20)
		return nil
	})

	events := []*dto.Payload{{Type: dto.C2CMessageCreate, ID: "1"}}
	engine2.ProcessEventBatch(events, nil)

	// 验证执行顺序一致
	assert.Equal(t, order1, order2, "ProcessEvent and ProcessEventBatch should have same execution order")
}

// TestProcessEventBatchMixedEventTypes 测试批量处理混合事件类型
func TestProcessEventBatchMixedEventTypes(t *testing.T) {
	engine := NewEngine()

	var c2cOrder []int
	var groupOrder []int

	// C2C matcher
	engine.OnC2C().SetPriority(20).HandleE(func(ctx *Context) error {
		c2cOrder = append(c2cOrder, 20)
		return nil
	})
	engine.OnC2C().SetPriority(10).HandleE(func(ctx *Context) error {
		c2cOrder = append(c2cOrder, 10)
		return nil
	})

	// Group matcher
	engine.OnGroupAt().SetPriority(30).HandleE(func(ctx *Context) error {
		groupOrder = append(groupOrder, 30)
		return nil
	})
	engine.OnGroupAt().SetPriority(5).HandleE(func(ctx *Context) error {
		groupOrder = append(groupOrder, 5)
		return nil
	})

	// 批量处理混合事件
	events := []*dto.Payload{
		{Type: dto.C2CMessageCreate, ID: "1"},
		{Type: dto.GroupAtMessageCreate, ID: "2"},
		{Type: dto.C2CMessageCreate, ID: "3"},
		{Type: dto.GroupAtMessageCreate, ID: "4"},
	}

	engine.ProcessEventBatch(events, nil)

	// 验证各自的执行顺序
	assert.Equal(t, []int{10, 20, 10, 20}, c2cOrder, "C2C handlers should execute in priority order")
	assert.Equal(t, []int{5, 30, 5, 30}, groupOrder, "Group handlers should execute in priority order")
}

// TestProcessEventBatchWithGenericMatchers 测试批量处理包含通用 matcher
func TestProcessEventBatchWithGenericMatchers(t *testing.T) {
	engine := NewEngine()

	var executionOrder []string

	// 通用 matcher
	engine.OnAny().SetPriority(15).HandleE(func(ctx *Context) error {
		executionOrder = append(executionOrder, "any-15")
		return nil
	})

	// 特定类型 matcher
	engine.OnC2C().SetPriority(10).HandleE(func(ctx *Context) error {
		executionOrder = append(executionOrder, "c2c-10")
		return nil
	})

	engine.OnC2C().SetPriority(20).HandleE(func(ctx *Context) error {
		executionOrder = append(executionOrder, "c2c-20")
		return nil
	})

	// 批量处理
	events := []*dto.Payload{
		{Type: dto.C2CMessageCreate, ID: "1"},
	}

	engine.ProcessEventBatch(events, nil)

	// 验证执行顺序（通用和特定类型混合，按优先级）
	expected := []string{"c2c-10", "any-15", "c2c-20"}
	assert.Equal(t, expected, executionOrder, "should merge and sort generic and specific matchers")
}

// TestProcessEventBatchCacheBuilding 测试批量处理时缓存构建
func TestProcessEventBatchCacheBuilding(t *testing.T) {
	engine := NewEngine()

	engine.OnC2C().SetPriority(50).HandleE(func(ctx *Context) error { return nil })
	engine.OnGroupAt().SetPriority(50).HandleE(func(ctx *Context) error { return nil })

	// COW 模式下，sortedCache 在添加 matcher 时自动构建
	// 验证状态包含正确的 matcher
	state := engine.state.Load().(*engineState)
	assert.NotNil(t, state.sortedCache[dto.C2CMessageCreate], "should have C2C cache")
	assert.NotNil(t, state.sortedCache[dto.GroupAtMessageCreate], "should have Group cache")

	// 批量处理混合事件类型
	events := []*dto.Payload{
		{Type: dto.C2CMessageCreate, ID: "1"},
		{Type: dto.GroupAtMessageCreate, ID: "2"},
	}

	engine.ProcessEventBatch(events, nil)

	// 验证批量处理正常工作（没有 panic 或错误）
	// COW 模式下缓存始终存在且正确
	assert.Equal(t, 2, engine.GetMatcherCount())
}

// TestProcessEventBatchEmptyEvents 测试空事件列表
func TestProcessEventBatchEmptyEvents(t *testing.T) {
	engine := NewEngine()

	var executed bool
	engine.OnC2C().HandleE(func(ctx *Context) error {
		executed = true
		return nil
	})

	// 处理空列表
	engine.ProcessEventBatch([]*dto.Payload{}, nil)

	assert.False(t, executed, "should not execute handler for empty batch")
}

// BenchmarkProcessEventBatchSorted 基准测试批量处理排序性能
func BenchmarkProcessEventBatchSorted(b *testing.B) {
	engine := NewEngine()

	// 注册多个 matcher
	for i := 0; i < 10; i++ {
		engine.OnC2C().SetPriority(uint(i * 10)).HandleE(func(ctx *Context) error {
			return nil
		})
	}

	// 准备事件
	events := make([]*dto.Payload, 100)
	for i := 0; i < 100; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate, ID: dto.EventID("event-" + string(rune(i)))}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessEventBatch(events, nil)
	}
}

// BenchmarkProcessEventBatchVsProcessEvent 对比批量处理和单个处理性能
func BenchmarkProcessEventBatchVsProcessEvent(b *testing.B) {
	engine := NewEngine()

	for i := 0; i < 10; i++ {
		engine.OnC2C().SetPriority(uint(i * 10)).HandleE(func(ctx *Context) error {
			return nil
		})
	}

	events := make([]*dto.Payload, 100)
	for i := 0; i < 100; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate, ID: dto.EventID("event-" + string(rune(i)))}
	}

	b.Run("ProcessEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, event := range events {
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx) // autoRelease 会自动释放
			}
		}
	})

	b.Run("ProcessEventBatch", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			engine.ProcessEventBatch(events, nil)
		}
	})
}
