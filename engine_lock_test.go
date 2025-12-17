package remilia

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

// TestProcessEventLockOptimization 测试 ProcessEvent 锁优化
func TestProcessEventLockOptimization(t *testing.T) {
	t.Run("单线程性能", func(t *testing.T) {
		engine := NewEngine()

		// 注册多个匹配器
		for i := 0; i < 10; i++ {
			engine.OnC2C().Handle(func(ctx *Context) {
				// 简单处理
			})
		}

		event := &dto.Payload{Type: dto.C2CMessageCreate}

		// 多次调用，验证无错误
		for i := 0; i < 100; i++ {
			ctx2 := NewContext(event, nil)
			engine.ProcessEvent(ctx2)
		}
	})

	t.Run("并发性能", func(t *testing.T) {
		engine := NewEngine()

		var handlerCount int32
		handler := func(ctx *Context) {
			atomic.AddInt32(&handlerCount, 1)
		}

		// 注册匹配器
		engine.OnC2C().Handle(handler)
		engine.OnGroupAt().Handle(handler)

		event1 := &dto.Payload{Type: dto.C2CMessageCreate}
		event2 := &dto.Payload{Type: dto.GroupAtMessageCreate}

		var wg sync.WaitGroup
		concurrency := 100

		// 并发处理事件
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				var event *dto.Payload
				if i%2 == 0 {
					event = event1
				} else {
					event = event2
				}
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}(i)
		}

		wg.Wait()

		// 验证所有 handler 都被调用
		if atomic.LoadInt32(&handlerCount) != int32(concurrency) {
			t.Errorf("Expected %d handler calls, got %d", concurrency, handlerCount)
		}
	})
}

// TestProcessEventBatchLockOptimization 测试批量处理锁优化
func TestProcessEventBatchLockOptimization(t *testing.T) {
	t.Run("批量处理正确性", func(t *testing.T) {
		engine := NewEngine()

		var count int32
		engine.OnC2C().Handle(func(ctx *Context) {
			atomic.AddInt32(&count, 1)
		})

		events := make([]*dto.Payload, 100)
		for i := 0; i < 100; i++ {
			events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
		}

		engine.ProcessEventBatch(events, nil)

		if atomic.LoadInt32(&count) != 100 {
			t.Errorf("Expected 100 handler calls, got %d", count)
		}
	})

	t.Run("混合事件类型", func(t *testing.T) {
		engine := NewEngine()

		var c2cCount, groupCount int32
		engine.OnC2C().Handle(func(ctx *Context) {
			atomic.AddInt32(&c2cCount, 1)
		})
		engine.OnGroupAt().Handle(func(ctx *Context) {
			atomic.AddInt32(&groupCount, 1)
		})

		events := make([]*dto.Payload, 200)
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
			} else {
				events[i] = &dto.Payload{Type: dto.GroupAtMessageCreate}
			}
		}

		engine.ProcessEventBatch(events, nil)

		if atomic.LoadInt32(&c2cCount) != 100 {
			t.Errorf("Expected 100 C2C handler calls, got %d", c2cCount)
		}
		if atomic.LoadInt32(&groupCount) != 100 {
			t.Errorf("Expected 100 Group handler calls, got %d", groupCount)
		}
	})
}

// TestProcessEventConcurrentModification 测试并发修改场景
func TestProcessEventConcurrentModification(t *testing.T) {
	t.Run("处理事件时添加新匹配器", func(t *testing.T) {
		engine := NewEngine()

		var count int32
		engine.OnC2C().Handle(func(ctx *Context) {
			atomic.AddInt32(&count, 1)
		})

		var wg sync.WaitGroup

		// 启动多个 goroutine 处理事件
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				event := &dto.Payload{Type: dto.C2CMessageCreate}
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}()
		}

		// 同时添加新的匹配器
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				engine.OnC2C().Handle(func(ctx *Context) {
					atomic.AddInt32(&count, 1)
				})
			}()
		}

		wg.Wait()

		// 验证没有 panic 或死锁
		t.Logf("Handler called %d times", atomic.LoadInt32(&count))
	})
}

// BenchmarkProcessEvent 基准测试
func BenchmarkProcessEvent(b *testing.B) {
	engine := NewEngine()

	// 注册多个匹配器
	for i := 0; i < 10; i++ {
		engine.OnC2C().Handle(func(ctx *Context) {
			// 简单处理
		})
	}

	event := &dto.Payload{Type: dto.C2CMessageCreate}

	b.ResetTimer()
	b.Run("Sequential", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ctx := NewContext(event, nil)
			engine.ProcessEvent(ctx)
		}
	})

	b.Run("Parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}
		})
	})
}

// BenchmarkProcessEventBatchOptimization 批量处理优化基准测试
func BenchmarkProcessEventBatchOptimization(b *testing.B) {
	engine := NewEngine()

	engine.OnC2C().Handle(func(ctx *Context) {})
	engine.OnGroupAt().Handle(func(ctx *Context) {})

	// 准备不同批次大小的事件
	batches := map[string][]*dto.Payload{
		"10":   make([]*dto.Payload, 10),
		"100":  make([]*dto.Payload, 100),
		"1000": make([]*dto.Payload, 1000),
	}

	for name, batch := range batches {
		for i := range batch {
			if i%2 == 0 {
				batch[i] = &dto.Payload{Type: dto.C2CMessageCreate}
			} else {
				batch[i] = &dto.Payload{Type: dto.GroupAtMessageCreate}
			}
		}

		b.Run("Batch_"+name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				engine.ProcessEventBatch(batch, nil)
			}
		})
	}
}

// BenchmarkProcessEventVsBatch 对比单个处理和批量处理
func BenchmarkProcessEventVsBatch(b *testing.B) {
	engine := NewEngine()

	engine.OnC2C().Handle(func(ctx *Context) {})

	events := make([]*dto.Payload, 100)
	for i := range events {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	b.Run("ProcessEvent_100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, event := range events {
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}
		}
	})

	b.Run("ProcessEventBatch_100", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			engine.ProcessEventBatch(events, nil)
		}
	})
}
