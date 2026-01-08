package middleware

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

func TestRetry_Success(t *testing.T) {
	engine := remilia.NewEngine()

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
		BackoffMax:  50 * time.Millisecond,
	}))

	var called int32
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&called, 1)
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	assert.Equal(t, int32(1), called, "成功执行，不应重试")
}

func TestRetry_FailThenSuccess(t *testing.T) {
	engine := remilia.NewEngine()

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
		BackoffMax:  50 * time.Millisecond,
	}))

	var called int32
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		count := atomic.AddInt32(&called, 1)
		if count < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	assert.Equal(t, int32(3), called, "应该重试2次后成功")
}

func TestRetry_AllFailed(t *testing.T) {
	engine := remilia.NewEngine()

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
		BackoffMax:  50 * time.Millisecond,
	}))

	var called int32
	testError := errors.New("persistent error")

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&called, 1)
		return testError
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	assert.Equal(t, int32(4), called, "应该执行1次+重试3次=4次")
}

func TestRetry_BlockError(t *testing.T) {
	engine := remilia.NewEngine()

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
		BackoffMax:  50 * time.Millisecond,
	}))

	var called int32
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&called, 1)
		return remilia.NewBlockError("blocked")
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	assert.Equal(t, int32(1), called, "BlockError 不应重试")
}

func TestRetry_CustomShouldRetry(t *testing.T) {
	engine := remilia.NewEngine()

	// 自定义重试策略：只重试特定错误
	tempError := errors.New("temporary")
	permError := errors.New("permanent")

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
		BackoffMax:  50 * time.Millisecond,
		ShouldRetry: func(err error) bool {
			return err.Error() == "temporary"
		},
	}))

	// 测试 1: temporary 错误应该重试
	var called1 int32
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&called1, 1)
		return tempError
	})

	ctx1 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx1)
	assert.Equal(t, int32(4), called1, "temporary 错误应该重试")

	// 清空 matchers
	engine.DeleteAllMatchers()

	// 测试 2: permanent 错误不应该重试
	var called2 int32
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&called2, 1)
		return permError
	})

	ctx2 := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx2)
	assert.Equal(t, int32(1), called2, "permanent 错误不应该重试")
}

func TestRetryWithDeadLetter(t *testing.T) {
	engine := remilia.NewEngine()

	deadLetterCh := make(chan remilia.DeadLetterItem, 10)

	engine.Use(RetryWithDeadLetter(
		RetryConfig{
			MaxAttempts: 2,
			BackoffBase: 10 * time.Millisecond,
			BackoffMax:  50 * time.Millisecond,
		},
		deadLetterCh,
	))

	testError := errors.New("persistent error")
	var called int32

	m := engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&called, 1)
		return testError
	})
	m.Source = "test-source"

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	assert.Equal(t, int32(3), called, "应该执行1次+重试2次=3次")

	// 检查死信队列
	select {
	case item := <-deadLetterCh:
		assert.NotNil(t, item.Event)
		assert.Equal(t, dto.C2CMessageCreate, item.Event.Type)
		assert.Error(t, item.Err)
		assert.Equal(t, 2, item.Attempt)
		assert.Equal(t, "test-source", item.Source)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("应该收到死信消息")
	}
}

func TestErrorHandler(t *testing.T) {
	engine := remilia.NewEngine()

	var capturedError error
	var capturedEventType dto.EventType

	engine.Use(ErrorHandler(func(ctx *remilia.Context, err error) {
		capturedError = err
		// 立即获取需要的值，不要保存 ctx 引用
		capturedEventType = ctx.GetEventType()
	}))

	testError := errors.New("test error")

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		return testError
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
	engine.ProcessEvent(ctx)

	assert.Equal(t, testError, capturedError)
	assert.Equal(t, dto.C2CMessageCreate, capturedEventType)
}

func TestRetry_BackoffCalculation(t *testing.T) {
	cfg := RetryConfig{
		MaxAttempts: 5,
		BackoffBase: 100 * time.Millisecond,
		BackoffMax:  1 * time.Second,
	}

	// 测试指数退避计算
	expected := []time.Duration{
		100 * time.Millisecond,  // 2^0 * 100ms
		200 * time.Millisecond,  // 2^1 * 100ms
		400 * time.Millisecond,  // 2^2 * 100ms
		800 * time.Millisecond,  // 2^3 * 100ms
		1000 * time.Millisecond, // 2^4 * 100ms, 但限制在 max
	}

	for i := 0; i < len(expected); i++ {
		delay := cfg.BackoffBase * time.Duration(1<<uint(i))
		if delay > cfg.BackoffMax {
			delay = cfg.BackoffMax
		}
		assert.Equal(t, expected[i], delay, "Attempt %d", i)
	}
}

func TestRetry_ContextCancellation(t *testing.T) {
	engine := remilia.NewEngine()

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 5,
		BackoffBase: 50 * time.Millisecond,
		BackoffMax:  200 * time.Millisecond,
	}))

	var called int32
	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		atomic.AddInt32(&called, 1)
		return errors.New("retry")
	})

	stdCtx, cancel := context.WithCancel(context.Background())
	ctx := remilia.NewContextWithContext(stdCtx, &dto.Payload{Type: dto.C2CMessageCreate}, nil)

	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	engine.ProcessEvent(ctx)

	assert.Less(t, atomic.LoadInt32(&called), int32(5), "should stop retrying after cancellation")
}

// BenchmarkRetry 基准测试
func BenchmarkRetry_NoError(b *testing.B) {
	engine := remilia.NewEngine()

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 3,
		BackoffBase: 10 * time.Millisecond,
		BackoffMax:  50 * time.Millisecond,
	}))

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		return nil
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := remilia.NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}
}

func BenchmarkRetry_WithRetries(b *testing.B) {
	engine := remilia.NewEngine()

	engine.Use(Retry(RetryConfig{
		MaxAttempts: 2,
		BackoffBase: time.Millisecond,
		BackoffMax:  5 * time.Millisecond,
	}))

	var counter int32

	engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
		// 每3次失败2次
		if atomic.AddInt32(&counter, 1)%3 == 0 {
			return nil
		}
		return errors.New("error")
	})

	event := &dto.Payload{Type: dto.C2CMessageCreate}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := remilia.NewContext(event, nil)
		engine.ProcessEvent(ctx)
	}
}
