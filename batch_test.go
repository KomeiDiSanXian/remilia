package remilia

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestProcessEventBatch 测试批量处理基本功能
func TestProcessEventBatch(t *testing.T) {
	engine := NewEngine()

	var counter int64
	engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt64(&counter, 1)
	})

	// 创建测试事件
	events := make([]*dto.Payload, 10)
	for i := 0; i < 10; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	// 批量处理
	engine.ProcessEventBatch(events, nil)

	// 验证所有事件都被处理
	assert.Equal(t, int64(10), counter)
}

// TestProcessEventBatchEmpty 测试空批次
func TestProcessEventBatchEmpty(t *testing.T) {
	engine := NewEngine()

	var counter int64
	engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt64(&counter, 1)
	})

	// 空批次
	engine.ProcessEventBatch([]*dto.Payload{}, nil)

	// 不应处理任何事件
	assert.Equal(t, int64(0), counter)
}

// TestProcessEventBatchWithMiddleware 测试批量处理和中间件
func TestProcessEventBatchWithMiddleware(t *testing.T) {
	engine := NewEngine()

	var mwCount, matcherCount int64

	// 添加全局中间件
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			atomic.AddInt64(&mwCount, 1)
			return next(ctx)
		}
	})

	engine.OnC2C().HandleE(func(ctx *Context) error {
		atomic.AddInt64(&matcherCount, 1)
		return nil
	})

	// 创建 5 个事件
	events := make([]*dto.Payload, 5)
	for i := 0; i < 5; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	engine.ProcessEventBatch(events, nil)

	// 验证中间件和处理器都被调用
	assert.Equal(t, int64(5), mwCount)
	assert.Equal(t, int64(5), matcherCount)
}

// TestProcessEventBatchWithBlock 测试阻塞模式
func TestProcessEventBatchWithBlock(t *testing.T) {
	engine := NewEngine()

	var matcher1Count, matcher2Count int64

	engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt64(&matcher1Count, 1)
	}).SetBlock(true)

	engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt64(&matcher2Count, 1)
	})

	events := make([]*dto.Payload, 5)
	for i := 0; i < 5; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	engine.ProcessEventBatch(events, nil)

	// 第一个matcher匹配并阻塞，第二个不应执行
	assert.Equal(t, int64(5), matcher1Count)
	assert.Equal(t, int64(0), matcher2Count)
}

// TestProcessEventBatchMiddlewareBlock 测试中间件阻塞
func TestProcessEventBatchMiddlewareBlock(t *testing.T) {
	engine := NewEngine()

	var mwCount, matcherCount int64

	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			atomic.AddInt64(&mwCount, 1)
			return NewBlockError("blocked") // 阻塞
		}
	})

	engine.OnC2C().HandleE(func(ctx *Context) error {
		atomic.AddInt64(&matcherCount, 1)
		return nil
	})

	events := make([]*dto.Payload, 3)
	for i := 0; i < 3; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	engine.ProcessEventBatch(events, nil)

	// 中间件阻塞，matcher不应执行
	assert.Equal(t, int64(3), mwCount)
	assert.Equal(t, int64(0), matcherCount)
}

// TestProcessEventBatchDifferentTypes 测试不同类型事件
func TestProcessEventBatchDifferentTypes(t *testing.T) {
	engine := NewEngine()

	var c2cCount, groupCount int64

	engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt64(&c2cCount, 1)
	})

	engine.OnGroupAt().Handle(func(ctx *Context) {
		atomic.AddInt64(&groupCount, 1)
	})

	// 混合类型事件
	events := []*dto.Payload{
		{Type: dto.C2CMessageCreate},
		{Type: dto.GroupAtMessageCreate},
		{Type: dto.C2CMessageCreate},
		{Type: dto.GroupAtMessageCreate},
		{Type: dto.C2CMessageCreate},
	}

	engine.ProcessEventBatch(events, nil)

	// 验证正确匹配
	assert.Equal(t, int64(3), c2cCount)
	assert.Equal(t, int64(2), groupCount)
}

// TestProcessEventBatchLargeVolume 测试大批量处理
func TestProcessEventBatchLargeVolume(t *testing.T) {
	engine := NewEngine()

	var counter int64
	engine.OnC2C().Handle(func(ctx *Context) {
		atomic.AddInt64(&counter, 1)
	})

	// 大批量：1000 个事件
	events := make([]*dto.Payload, 1000)
	for i := 0; i < 1000; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	start := time.Now()
	engine.ProcessEventBatch(events, nil)
	duration := time.Since(start)

	assert.Equal(t, int64(1000), counter)
	t.Logf("处理 1000 个事件耗时: %v", duration)
	t.Logf("平均每个事件: %v", duration/1000)
}

// BenchmarkProcessEventBatch 批量处理基准测试
func BenchmarkProcessEventBatch(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		ctx.Set("processed", true)
	})

	events := make([]*dto.Payload, 10)
	for i := 0; i < 10; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessEventBatch(events, nil)
	}
}

// BenchmarkProcessEventBatchSizes 不同批量大小的基准测试
func BenchmarkProcessEventBatchSizes(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		ctx.Set("processed", true)
	})

	sizes := []int{1, 10, 50, 100, 500}

	for _, size := range sizes {
		events := make([]*dto.Payload, size)
		for i := 0; i < size; i++ {
			events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
		}

		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				engine.ProcessEventBatch(events, nil)
			}
		})
	}
}

// BenchmarkCompareProcessMethods 对比单个处理和批量处理
func BenchmarkCompareProcessMethods(b *testing.B) {
	engine := NewEngine()
	engine.OnC2C().Handle(func(ctx *Context) {
		ctx.Set("processed", true)
	})

	events := make([]*dto.Payload, 10)
	for i := 0; i < 10; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	b.Run("SingleProcess", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, event := range events {
				ctx := NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}
		}
	})

	b.Run("BatchProcess", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			engine.ProcessEventBatch(events, nil)
		}
	})
}

// BenchmarkBatchProcessWithComplexMatchers 复杂匹配器的批量处理
func BenchmarkBatchProcessWithComplexMatchers(b *testing.B) {
	engine := NewEngine()

	// 添加多个匹配器
	for i := 0; i < 10; i++ {
		engine.OnC2C().Handle(func(ctx *Context) {
			ctx.Set(fmt.Sprintf("matcher_%d", i), true)
		})
	}

	events := make([]*dto.Payload, 50)
	for i := 0; i < 50; i++ {
		events[i] = &dto.Payload{Type: dto.C2CMessageCreate}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.ProcessEventBatch(events, nil)
	}
}
