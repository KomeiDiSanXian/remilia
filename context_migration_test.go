package remilia

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestContext_NoPoolBehavior 测试无对象池时的行为
// 验证从 V1 迁移后的行为一致性
func TestContext_NoPoolBehavior(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}

	// 创建多个 Context，应该完全独立
	ctx1 := NewContext(event, nil)
	ctx1.SetState("key", "value1")

	ctx2 := NewContext(event, nil)
	ctx2.SetState("key", "value2")

	ctx3 := NewContext(event, nil)
	ctx3.SetState("key", "value3")

	// 验证完全独立，互不影响
	val1, ok1 := ctx1.GetState("key")
	val2, ok2 := ctx2.GetState("key")
	val3, ok3 := ctx3.GetState("key")

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.True(t, ok3)

	assert.Equal(t, "value1", val1)
	assert.Equal(t, "value2", val2)
	assert.Equal(t, "value3", val3)
}

// TestContext_MassCreation 测试大量创建的稳定性
// 验证 GC 模式下大量创建的性能和稳定性
func TestContext_MassCreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping mass creation test in short mode")
	}

	const count = 100000

	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	before := m.Alloc

	// 创建大量 Context
	for i := 0; i < count; i++ {
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID(fmt.Sprintf("mass-test-%d", i)),
		}
		ctx := NewContext(event, nil)
		ctx.SetState("index", i)
		ctx.SetState("data", make([]byte, 100)) // 模拟一些数据
		// 让 GC 回收
	}

	// 强制 GC 多次
	runtime.GC()
	runtime.GC()
	time.Sleep(10 * time.Millisecond) // 让 GC 完成

	runtime.ReadMemStats(&m)
	after := m.Alloc

	// 处理可能的负增长（GC 在测试期间运行）
	var growth uint64
	if after > before {
		growth = after - before
	} else {
		// GC 回收了内存，这是好事
		growth = 0
	}

	t.Logf("Memory: before=%d, after=%d, growth=%d bytes for %d contexts",
		before, after, growth, count)

	if growth > 0 {
		avgPerContext := growth / count
		t.Logf("Average per context: %d bytes", avgPerContext)

		// 验证内存没有持续增长
		// 平均每个 Context 应该不超过 2KB 的持久内存（考虑到临时对象）
		assert.Less(t, avgPerContext, uint64(2048),
			"Average memory per context should be less than 2KB")
	} else {
		t.Log("Memory decreased or stayed same - GC is working well!")
	}
}

// TestContext_ConcurrentCreationAndUsage 测试并发创建和使用
// 验证并发场景下的安全性
func TestContext_ConcurrentCreationAndUsage(t *testing.T) {
	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	errors := make(chan error, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for i := 0; i < iterations; i++ {
				event := &dto.Payload{
					Type: dto.C2CMessageCreate,
					ID:   dto.EventID(fmt.Sprintf("concurrent-%d-%d", id, i)),
				}
				ctx := NewContext(event, nil)

				// 模拟使用
				ctx.SetState("goroutine", id)
				ctx.SetState("iteration", i)
				ctx.SetState("data", fmt.Sprintf("data-%d-%d", id, i))

				// 验证状态正确
				val, ok := ctx.GetState("goroutine")
				if !ok {
					errors <- fmt.Errorf("failed to get state in goroutine %d", id)
					return
				}

				if val.(int) != id {
					errors <- fmt.Errorf("state corruption in goroutine %d: expected %d, got %v",
						id, id, val)
					return
				}

				// 验证其他状态
				iterVal, _ := ctx.GetState("iteration")
				if iterVal.(int) != i {
					errors <- fmt.Errorf("iteration state corruption in goroutine %d", id)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	// 检查错误
	errCount := 0
	for err := range errors {
		t.Error(err)
		errCount++
	}

	assert.Equal(t, 0, errCount, "Should have no errors in concurrent creation and usage")
}

// TestContext_AsyncNotCollected 测试异步场景下 Context 不被误回收
// 验证 Context 在异步场景下不会被 GC 误回收
func TestContext_AsyncNotCollected(t *testing.T) {
	event := &dto.Payload{
		Type: dto.C2CMessageCreate,
		ID:   "async-test",
	}
	ctx := NewContext(event, nil)
	ctx.SetState("test", "important-value")
	ctx.SetState("counter", 0)

	done := make(chan bool)

	// 在 goroutine 中使用 Context
	go func() {
		// 延迟一段时间，给主 goroutine 时间返回
		time.Sleep(100 * time.Millisecond)

		// 强制 GC 多次
		runtime.GC()
		runtime.GC()

		// Context 应该仍然可用（被 goroutine 持有）
		val, ok := ctx.GetState("test")
		assert.True(t, ok, "State should still be available")
		assert.Equal(t, "important-value", val, "State value should be correct")

		// 修改状态
		counter, _ := ctx.GetState("counter")
		ctx.SetState("counter", counter.(int)+1)

		time.Sleep(50 * time.Millisecond)

		// 再次验证
		val2, ok2 := ctx.GetState("test")
		assert.True(t, ok2)
		assert.Equal(t, "important-value", val2)

		done <- true
	}()

	// 主 goroutine 立即返回
	// Context 应该被异步 goroutine 持有，不会被回收

	<-done

	// 最终验证
	counter, _ := ctx.GetState("counter")
	assert.Equal(t, 1, counter.(int), "Counter should be incremented by async goroutine")
}

// TestEngine_ProcessEventNoLeak 测试 ProcessEvent 无内存泄漏
// 验证 ProcessEvent 后 Context 能被正常回收
func TestEngine_ProcessEventNoLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	engine := NewEngine()

	var processedCount int
	engine.OnC2C().Handle(func(ctx *Context) {
		ctx.SetState("processed", true)
		ctx.SetState("data", make([]byte, 200)) // 模拟一些数据
		processedCount++
	})

	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	before := m.Alloc

	// 处理大量事件
	const count = 10000
	for i := 0; i < count; i++ {
		event := &dto.Payload{
			Type: dto.C2CMessageCreate,
			ID:   dto.EventID(fmt.Sprintf("leak-test-%d", i)),
		}
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}

	// 强制 GC 多次，确保回收
	runtime.GC()
	runtime.GC()
	time.Sleep(20 * time.Millisecond)

	runtime.ReadMemStats(&m)
	after := m.Alloc

	growthSigned := int64(after) - int64(before)
	if growthSigned < 0 {
		growthSigned = 0 // 可能因分配模式导致 GC 后低于基线，视为无增长
	}
	growth := uint64(growthSigned)
	t.Logf("Processed %d events", processedCount)
	t.Logf("Memory growth: %d bytes for %d events", growth, count)
	avgPerEvent := growth / count
	t.Logf("Average per event: %d bytes", avgPerEvent)
	assert.Less(t, avgPerEvent, uint64(200),
		"Average memory per event should be less than 200 bytes")

	assert.Equal(t, count, processedCount, "All events should be processed")
}

// TestContext_MultipleGoroutinesSharedContext 测试多个 goroutine 共享同一个 Context
func TestContext_MultipleGoroutinesSharedContext(t *testing.T) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}
	ctx := NewContext(event, nil)

	const goroutines = 50
	var wg sync.WaitGroup

	// 多个 goroutine 并发读写同一个 Context
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 写入
			key := fmt.Sprintf("goroutine_%d", id)
			ctx.SetState(key, id)

			// 延迟一下
			time.Sleep(10 * time.Millisecond)

			// 读取验证
			val, ok := ctx.GetState(key)
			assert.True(t, ok)
			assert.Equal(t, id, val)
		}(i)
	}

	wg.Wait()

	// 验证所有 goroutine 写入的数据都在
	for i := 0; i < goroutines; i++ {
		key := fmt.Sprintf("goroutine_%d", i)
		val, ok := ctx.GetState(key)
		assert.True(t, ok, "State from goroutine %d should exist", i)
		assert.Equal(t, i, val, "State value from goroutine %d should be correct", i)
	}
}

// TestContext_GCBehavior 测试 Context 的 GC 行为
func TestContext_GCBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping GC behavior test in short mode")
	}

	// 创建一些 Context 并让它们超出作用域
	func() {
		for i := 0; i < 1000; i++ {
			event := &dto.Payload{Type: dto.C2CMessageCreate}
			ctx := NewContext(event, nil)
			ctx.SetState("index", i)
			// Context 在这里超出作用域
		}
	}()

	// 强制 GC
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	beforeGC := m.NumGC

	runtime.GC()

	runtime.ReadMemStats(&m)
	afterGC := m.NumGC

	// 验证 GC 确实运行了
	assert.Greater(t, afterGC, beforeGC, "GC should have run")

	t.Logf("GC cycles: before=%d, after=%d", beforeGC, afterGC)
	t.Logf("Heap alloc: %d bytes", m.HeapAlloc)
}
