package remilia

import (
	"runtime"
	"sync"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestMatcherCreationFrequency 测试 Matcher 创建频率
func TestMatcherCreationFrequency(t *testing.T) {
	engine := NewEngine()

	// 模拟应用启动时注册 Matcher
	startTime := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < 50; i++ {
			engine.OnC2C().Handle(func(ctx *Context) {
				// Handler logic
			})
		}
	})

	t.Logf("创建 50 个 Matcher 耗时: %v", startTime.T)
	t.Logf("平均每个 Matcher 创建时间: %v", startTime.T/50)

	// 通常这个时间非常短（微秒级），说明不需要对象池
	if startTime.T < 1000000 { // < 1ms
		t.Log("✅ Matcher 创建非常快，不需要对象池优化")
	}
}

// BenchmarkMatcherCreation 基准测试 Matcher 创建
func BenchmarkMatcherCreation(b *testing.B) {
	engine := NewEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.OnC2C().Handle(func(ctx *Context) {
			// Handler
		})
	}
}

// BenchmarkStateMapOperations 测试 State map 操作性能
func BenchmarkStateMapOperations(b *testing.B) {
	event := &dto.Payload{Type: dto.C2CMessageCreate}

	b.Run("CurrentImplementation", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ctx := NewContext(event, nil)
			ctx.SetState("key1", "value1")
			ctx.SetState("key2", 123)
			ctx.SetState("key3", true)
			_, _ = ctx.GetState("key1")
			_ = ctx.GetAllState()
		}
	})

	// 模拟如果使用独立 map 池的情况
	b.Run("TheoreticalSeparateMapPool", func(b *testing.B) {
		// 这只是理论测试，实际不实现
		mapPool := &sync.Pool{
			New: func() interface{} {
				return make(State)
			},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = NewContext(event, nil)
			// 理论上从 map 池获取
			stateMap := mapPool.Get().(State)

			stateMap["key1"] = "value1"
			stateMap["key2"] = 123
			stateMap["key3"] = true
			_ = stateMap["key1"]

			// 清理并放回
			for k := range stateMap {
				delete(stateMap, k)
			}
			mapPool.Put(stateMap)
		}
	})
}

// TestMatcherMemoryUsage 测试 Matcher 内存使用
func TestMatcherMemoryUsage(t *testing.T) {
	var m1, m2 runtime.MemStats

	runtime.GC()
	runtime.ReadMemStats(&m1)

	// 创建 100 个 Matcher
	engine := NewEngine()
	for i := 0; i < 100; i++ {
		engine.OnC2C().Handle(func(ctx *Context) {})
	}

	runtime.ReadMemStats(&m2)

	matcherMem := m2.TotalAlloc - m1.TotalAlloc
	perMatcher := matcherMem / 100

	t.Logf("100 个 Matcher 内存使用: %d bytes", matcherMem)
	t.Logf("平均每个 Matcher: %d bytes", perMatcher)

	// Matcher 内存占用应该很小
	if matcherMem < 50000 { // < 50KB
		t.Log("✅ Matcher 内存占用极小，不需要对象池")
	}
}
