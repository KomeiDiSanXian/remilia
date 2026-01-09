package remilia

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestContextClone 测试 Context 克隆功能
func TestContextClone(t *testing.T) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-1",
	}

	ctx := NewContext(event, nil)
	ctx.Set("key1", "value1")
	ctx.Set("key2", 42)

	// 克隆 context
	clonedCtx := ctx.Clone()

	// 验证克隆的 context 是独立的实例
	assert.NotSame(t, ctx, clonedCtx, "cloned context should be a different instance")

	// 验证 state 被复制
	assert.Equal(t, "value1", clonedCtx.GetString("key1"), "state should be copied")
	assert.Equal(t, 42, clonedCtx.GetInt("key2"), "state should be copied")

	// 验证 state map 是独立的（修改不会影响原 context）
	// 修改克隆的 V2 store，不应影响原 context
	clonedCtx.Set("key1", "modified")
	assert.Equal(t, "value1", ctx.GetString("key1"), "modifying cloned context should not affect original")
	assert.Equal(t, "modified", clonedCtx.GetString("key1"), "cloned context should be modified")

	// 验证 event 和 api 是共享的（浅拷贝）
	assert.Same(t, ctx.event, clonedCtx.event, "event should be shared")
	// Note: api is nil in this test, so we just verify both are nil
	assert.Equal(t, ctx.api, clonedCtx.api, "api should be the same (both nil)")

	// 验证 stdContext 是共享的
	// Note: Both contexts share the same stdContext reference
	assert.Equal(t, ctx.ctx, clonedCtx.ctx, "std context should be shared")
}

// TestContextCloneInGoroutine 测试在 goroutine 中使用 Clone
func TestContextCloneInGoroutine(t *testing.T) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-2",
	}

	ctx := NewContext(event, nil)
	ctx.Set("counter", 0)

	var wg sync.WaitGroup
	var completed atomic.Int32

	// 启动多个 goroutine，每个使用 Clone
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 克隆 context
			asyncCtx := ctx.Clone()

			// 修改克隆的 context（不会影响其他 goroutine）
			asyncCtx.Set("goroutine_id", id)
			asyncCtx.Set("counter", id*10)

			// 模拟工作
			time.Sleep(10 * time.Millisecond)

			// 验证独立性
			assert.Equal(t, id, asyncCtx.GetInt("goroutine_id"))
			assert.Equal(t, id*10, asyncCtx.GetInt("counter"))

			completed.Add(1)
		}(i)
	}

	wg.Wait()

	// 验证原 context 未被修改
	assert.Equal(t, 0, ctx.GetInt("counter"), "original context should not be modified")
	assert.Equal(t, int32(10), completed.Load(), "all goroutines should complete")
}

// TestContextCloneEmpty 测试克隆空 state 的 context
func TestContextCloneEmpty(t *testing.T) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-4",
	}

	ctx := NewContext(event, nil)

	// 克隆没有 state 的 context
	clonedCtx := ctx.Clone()

	// 验证克隆成功
	assert.NotNil(t, clonedCtx)

	// 在克隆的 context 中添加 state
	clonedCtx.Set("new_key", "new_value")
	assert.Equal(t, "new_value", clonedCtx.GetString("new_key"))

	// 原 context 不应受影响
	assert.Equal(t, "", ctx.GetString("new_key"))
}

// TestContextCloneMultipleTimes 测试多次克隆
func TestContextCloneMultipleTimes(t *testing.T) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-5",
	}

	ctx := NewContext(event, nil)
	ctx.Set("generation", 0)

	// 第一次克隆
	clone1 := ctx.Clone()
	clone1.Set("generation", 1)

	// 从克隆再克隆
	clone2 := clone1.Clone()
	clone2.Set("generation", 2)

	// 验证每个 context 都是独立的
	assert.Equal(t, 0, ctx.GetInt("generation"))
	assert.Equal(t, 1, clone1.GetInt("generation"))
	assert.Equal(t, 2, clone2.GetInt("generation"))
}

// TestContextCloneWithComplexState 测试克隆包含复杂数据的 context
func TestContextCloneWithComplexState(t *testing.T) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "test-event-6",
	}

	ctx := NewContext(event, nil)

	// 设置复杂状态
	ctx.Set("string", "value")
	ctx.Set("int", 42)
	ctx.Set("bool", true)
	ctx.Set("float", 3.14)
	ctx.Set("slice", []int{1, 2, 3})
	ctx.Set("map", map[string]int{"a": 1, "b": 2})

	// 克隆
	clonedCtx := ctx.Clone()

	// 验证所有类型都被正确复制
	assert.Equal(t, "value", clonedCtx.GetString("string"))
	assert.Equal(t, 42, clonedCtx.GetInt("int"))
	assert.Equal(t, true, clonedCtx.GetBool("bool"))
	assert.Equal(t, 3.14, clonedCtx.GetFloat64("float"))

	// 注意：slice 和 map 是引用类型，会共享底层数据
	// 这是浅拷贝的预期行为
	sliceAny, ok := clonedCtx.Get("slice")
	assert.True(t, ok)
	slice := sliceAny.([]int)
	assert.Equal(t, []int{1, 2, 3}, slice)
}

// BenchmarkContextClone 基准测试 Clone 性能
func BenchmarkContextClone(b *testing.B) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "bench-event",
	}

	ctx := NewContext(event, nil)
	ctx.Set("key1", "value1")
	ctx.Set("key2", 42)
	ctx.Set("key3", true)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ctx.Clone()
	}
}

// BenchmarkContextCloneWithLargeState 测试大 state 的克隆性能
func BenchmarkContextCloneWithLargeState(b *testing.B) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "bench-event",
	}

	ctx := NewContext(event, nil)

	// 添加大量 state
	for i := 0; i < 100; i++ {
		ctx.Set("key"+string(rune(i)), i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ctx.Clone()
	}
}

// TestContextCloneNilEvent 测试克隆 nil event 的 context
func TestContextCloneNilEvent(t *testing.T) {
	ctx := &Context{}
	ctx.Set("key", "value")

	// 克隆
	clonedCtx := ctx.Clone()

	// 验证克隆成功
	assert.NotNil(t, clonedCtx)
	assert.Equal(t, "value", clonedCtx.GetString("key"))
	assert.Nil(t, clonedCtx.event)
}
