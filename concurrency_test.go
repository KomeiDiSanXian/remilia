package remilia

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// 注意: 并发限流功能已移到中间件
// 详见 middleware/ConcurrencyLimit

func TestEngine_ConcurrencyWithMiddleware_Drop(t *testing.T) {
	engine := NewEngine()

	// 使用中间件实现并发限流
	var dropped int32
	sema := make(chan struct{}, 2) // 最大并发2
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			select {
			case sema <- struct{}{}:
				defer func() { <-sema }()
				return next(ctx)
			default:
				atomic.AddInt32(&dropped, 1)
				return fmt.Errorf("concurrency limit exceeded")
			}
		}
	})

	var processed int32
	// 使用新 Engine.On 签名，显式指定事件类型并传入规则
	engine.On(dto.C2CMessageCreate, OnC2CMessage()).HandleE(func(ctx *Context) error {
		atomic.AddInt32(&processed, 1)
		time.Sleep(100 * time.Millisecond) // 增加处理时间
		return nil
	})

	// 快速发送 10 个事件
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
			engine.ProcessEvent(ctx)
		}()
		time.Sleep(time.Millisecond) // 稍微错开发送时间
	}
	wg.Wait()

	// 由于并发限制，应该有事件被丢弃
	t.Logf("Processed: %d, Dropped: %d", processed, dropped)
	assert.Greater(t, int(processed), 0, "应该有事件被处理")
	assert.Greater(t, int(dropped), 0, "应该有事件被丢弃")
}

func TestEngine_ConcurrencyWithMiddleware_Block(t *testing.T) {
	engine := NewEngine()

	// 使用中间件实现阻塞式并发控制
	sema := make(chan struct{}, 2)
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			sema <- struct{}{} // 阻塞等待
			defer func() { <-sema }()
			return next(ctx)
		}
	})

	var processed int32
	engine.On(dto.C2CMessageCreate, OnC2CMessage()).HandleE(func(ctx *Context) error {
		atomic.AddInt32(&processed, 1)
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	// 发送 5 个事件
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
			engine.ProcessEvent(ctx)
		}()
	}
	wg.Wait()

	// 阻塞模式会等待，所有事件都应该被处理
	assert.Equal(t, int32(5), processed)
}

func TestEngine_ConcurrencyWithMiddleware_TryWait(t *testing.T) {
	engine := NewEngine()

	// 使用中间件实现超时等待
	var timedout int32
	sema := make(chan struct{}, 1)
	engine.Use(func(next HandlerE) HandlerE {
		return func(ctx *Context) error {
			select {
			case sema <- struct{}{}:
				defer func() { <-sema }()
				return next(ctx)
			case <-time.After(20 * time.Millisecond):
				atomic.AddInt32(&timedout, 1)
				return fmt.Errorf("concurrency wait timeout")
			}
		}
	})

	var processed int32
	engine.On(dto.C2CMessageCreate, OnC2CMessage()).HandleE(func(ctx *Context) error {
		atomic.AddInt32(&processed, 1)
		time.Sleep(50 * time.Millisecond) // 处理时间超过等待超时
		return nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
			engine.ProcessEvent(ctx)
		}()
		time.Sleep(time.Millisecond) // 稍微错开
	}
	wg.Wait()

	// 部分事件会超时
	t.Logf("Processed: %d, Timedout: %d", processed, timedout)
	assert.Greater(t, int(processed), 0, "应该有事件被处理")
	assert.Greater(t, int(timedout), 0, "应该有事件超时")
}
