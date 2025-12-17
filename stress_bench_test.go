package remilia

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// BenchmarkStressTest_SingleCore 单核性能测试
func BenchmarkStressTest_SingleCore(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	// 限制到单核
	runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(runtime.NumCPU())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}
}

// BenchmarkStressTest_MultiCore 多核并发性能测试
func BenchmarkStressTest_MultiCore(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := NewContext(event, nil)
			engine.ProcessEvent(ctx)
		}
	})
}

// BenchmarkStressTest_HighLoad 高负载压力测试 (1000 goroutines)
func BenchmarkStressTest_HighLoad(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
		// 模拟一些处理时间
		time.Sleep(time.Microsecond)
	})

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	numGoroutines := 1000
	eventsPerGoroutine := b.N / numGoroutines
	if eventsPerGoroutine == 0 {
		eventsPerGoroutine = 1
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)
	totalEvents := numGoroutines * eventsPerGoroutine

	b.ReportMetric(float64(totalEvents)/duration.Seconds(), "events/sec")
	b.ReportMetric(duration.Seconds()*1000, "total_ms")
}

// BenchmarkMemoryUsage 内存使用量测试
func BenchmarkMemoryUsage(b *testing.B) {
	engine := NewEngine()
	for i := 0; i < 100; i++ {
		engine.OnAny(OnKeyword("test" + string(rune(i)))).Handle(func(ctx *Context) {
			_ = ctx.GetMessageContent()
		})
	}

	detailMap := map[string]interface{}{"content": "test message test0"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	b.ReportMetric(float64(m2.Alloc-m1.Alloc)/float64(b.N), "bytes_per_op")
	b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/float64(b.N), "total_bytes_per_op")
}

// BenchmarkBatchProcessing_Optimal 最优批量处理性能
func BenchmarkBatchProcessing_Optimal(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		_ = ctx.GetMessageContent()
	})

	batchSizes := []int{1, 10, 50, 100, 200}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("Batch-%d", batchSize), func(b *testing.B) {
			events := make([]*dto.Payload, batchSize)
			for i := 0; i < batchSize; i++ {
				detailMap := map[string]interface{}{"content": fmt.Sprintf("message %d", i)}
				detailJSON, _ := json.Marshal(detailMap)
				events[i] = &dto.Payload{
					Type:   dto.C2CMessageCreate,
					Detail: detailJSON,
				}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				engine.ProcessEventBatch(events, nil)
			}

			b.ReportMetric(float64(b.N*batchSize), "total_events")
		})
	}
}

// BenchmarkMatcher_Performance 匹配器性能对比
func BenchmarkMatcher_Performance(b *testing.B) {
	tests := []struct {
		name      string
		setupFunc func() *Engine
	}{
		{
			"Single_Matcher",
			func() *Engine {
				engine := NewEngine()
				engine.OnAny(OnKeyword("hello")).Handle(func(ctx *Context) {})
				return engine
			},
		},
		{
			"Multiple_Matchers_10",
			func() *Engine {
				engine := NewEngine()
				for i := 0; i < 10; i++ {
					engine.OnAny(OnKeyword(fmt.Sprintf("test%d", i))).Handle(func(ctx *Context) {})
				}
				return engine
			},
		},
		{
			"Multiple_Matchers_100",
			func() *Engine {
				engine := NewEngine()
				for i := 0; i < 100; i++ {
					engine.OnAny(OnKeyword(fmt.Sprintf("test%d", i))).Handle(func(ctx *Context) {})
				}
				return engine
			},
		},
		{
			"Complex_Rules",
			func() *Engine {
				engine := NewEngine()
				engine.OnC2C(
					And(
						Or(OnKeyword("hello"), OnKeyword("hi")),
						Not(OnSuffix("!")),
						OnPrefix("user"),
					),
				).Handle(func(ctx *Context) {})
				return engine
			},
		},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			engine := test.setupFunc()

			detailMap := map[string]interface{}{"content": "hello world"}
			detailJSON, _ := json.Marshal(detailMap)
			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: detailJSON,
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}
		})
	}
}

func BenchmarkStress_ManyMatchers(b *testing.B) {
	engine := NewEngine()
	for i := 0; i < 1000; i++ {
		engine.OnAny(OnKeyword("test" + string(rune(i)))).Handle(func(ctx *Context) {
			_ = ctx.GetMessageContent()
		})
	}

	detailMap := map[string]interface{}{"content": "test message"}
	detailJSON, _ := json.Marshal(detailMap)
	event := &dto.Payload{
		Type:   dto.C2CMessageCreate,
		Detail: detailJSON,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := NewContext(event, nil)
			engine.ProcessEvent(ctx)
		}
	})
}
