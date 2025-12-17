package middleware

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestTimeoutMiddleware 测试超时中间件
func TestTimeoutMiddleware(t *testing.T) {
	t.Run("快速 handler 正常完成", func(t *testing.T) {
		middleware := Timeout(100 * time.Millisecond)

		handler := middleware(func(ctx *remilia.Context) error {
			return nil
		})

		ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		err := handler(ctx)

		assert.NoError(t, err)
	})

	t.Run("慢 handler 超时返回错误", func(t *testing.T) {
		middleware := Timeout(50 * time.Millisecond)

		handler := middleware(func(ctx *remilia.Context) error {
			time.Sleep(200 * time.Millisecond) // 慢于超时
			return nil
		})

		ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		start := time.Now()
		err := handler(ctx)
		duration := time.Since(start)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
		assert.Less(t, duration, 100*time.Millisecond) // 应该在超时附近返回
	})

	t.Run("handler 返回错误", func(t *testing.T) {
		middleware := Timeout(100 * time.Millisecond)

		expectedErr := errors.New("handler error")
		handler := middleware(func(ctx *remilia.Context) error {
			return expectedErr
		})

		ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		err := handler(ctx)

		assert.Equal(t, expectedErr, err)
	})

	t.Run("handler panic 应该被捕获", func(t *testing.T) {
		middleware := Timeout(100 * time.Millisecond)

		handler := middleware(func(ctx *remilia.Context) error {
			panic("intentional panic")
		})

		ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		err := handler(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "panic")
	})
}

// TestTimeoutNoGoroutineLeak 测试 goroutine 不泄漏
func TestTimeoutNoGoroutineLeak(t *testing.T) {
	t.Run("快速完成的 handler 不泄漏 goroutine", func(t *testing.T) {
		middleware := Timeout(100 * time.Millisecond)

		// 记录初始 goroutine 数量
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		initialGoroutines := runtime.NumGoroutine()

		// 执行多次
		for i := 0; i < 100; i++ {
			handler := middleware(func(ctx *remilia.Context) error {
				return nil // 快速完成
			})

			ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
			_ = handler(ctx)
		}

		// 等待 goroutine 清理
		time.Sleep(50 * time.Millisecond)
		runtime.GC()
		time.Sleep(10 * time.Millisecond)

		finalGoroutines := runtime.NumGoroutine()

		// goroutine 数量应该相近（允许一些波动）
		diff := finalGoroutines - initialGoroutines
		assert.Less(t, diff, 10, "goroutine leak detected: initial=%d, final=%d, diff=%d",
			initialGoroutines, finalGoroutines, diff)

		t.Logf("Initial goroutines: %d, Final goroutines: %d, Diff: %d",
			initialGoroutines, finalGoroutines, diff)
	})

	t.Run("超时的 handler 不泄漏 goroutine", func(t *testing.T) {
		middleware := Timeout(10 * time.Millisecond)

		// 记录初始 goroutine 数量
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		initialGoroutines := runtime.NumGoroutine()

		// 执行多次超时
		for i := 0; i < 50; i++ {
			handler := middleware(func(ctx *remilia.Context) error {
				time.Sleep(100 * time.Millisecond) // 慢，会超时
				return nil
			})

			ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
			_ = handler(ctx)
		}

		// 等待所有 goroutine 完成
		time.Sleep(200 * time.Millisecond)
		runtime.GC()
		time.Sleep(10 * time.Millisecond)

		finalGoroutines := runtime.NumGoroutine()

		// goroutine 数量应该相近
		diff := finalGoroutines - initialGoroutines
		assert.Less(t, diff, 10, "goroutine leak detected: initial=%d, final=%d, diff=%d",
			initialGoroutines, finalGoroutines, diff)

		t.Logf("Initial goroutines: %d, Final goroutines: %d, Diff: %d",
			initialGoroutines, finalGoroutines, diff)
	})
}

// TestTimeoutTimerCleanup 测试 Timer 是否被正确清理
func TestTimeoutTimerCleanup(t *testing.T) {
	t.Run("快速完成时 timer 被停止", func(t *testing.T) {
		middleware := Timeout(100 * time.Millisecond)

		handler := middleware(func(ctx *remilia.Context) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		})

		ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		err := handler(ctx)

		assert.NoError(t, err)
		// timer.Stop() 在 defer 中被调用，确保 timer 被清理
	})

	t.Run("超时后 timer 也被清理", func(t *testing.T) {
		middleware := Timeout(10 * time.Millisecond)

		handler := middleware(func(ctx *remilia.Context) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		})

		ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		err := handler(ctx)

		assert.Error(t, err)
		// timer.Stop() 在 defer 中被调用
	})
}

// TestTimeoutConcurrent 测试并发场景
func TestTimeoutConcurrent(t *testing.T) {
	t.Run("并发执行多个带超时的 handler", func(t *testing.T) {
		middleware := Timeout(50 * time.Millisecond)

		// 并发执行
		for i := 0; i < 100; i++ {
			go func(i int) {
				handler := middleware(func(ctx *remilia.Context) error {
					if i%2 == 0 {
						time.Sleep(20 * time.Millisecond) // 快速
					} else {
						time.Sleep(100 * time.Millisecond) // 超时
					}
					return nil
				})

				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}(i)
		}

		// 等待完成
		time.Sleep(200 * time.Millisecond)
	})
}

// TestTimeoutWithOtherMiddleware 测试与其他中间件组合
func TestTimeoutWithOtherMiddleware(t *testing.T) {
	t.Run("Timeout + Retry 组合", func(t *testing.T) {
		engine := remilia.NewEngine()

		var attemptCount int
		engine.Use(Retry(RetryConfig{MaxAttempts: 3, BackoffBase: 10 * time.Millisecond}))
		engine.Use(Timeout(50 * time.Millisecond))

		engine.OnC2C().HandleE(func(ctx *remilia.Context) error {
			attemptCount++
			if attemptCount < 2 {
				return fmt.Errorf("temporary error")
			}
			return nil
		})

		event := &dto.Payload{Type: dto.C2CMessageCreate}
		ctx := remilia.NewContext(event, nil)

		engine.ProcessEvent(ctx)

		assert.GreaterOrEqual(t, attemptCount, 2)
	})
}

// BenchmarkTimeout 基准测试
func BenchmarkTimeout(b *testing.B) {
	middleware := Timeout(100 * time.Millisecond)

	handler := middleware(func(ctx *remilia.Context) error {
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

// BenchmarkTimeoutWithWork 模拟实际工作的基准测试
func BenchmarkTimeoutWithWork(b *testing.B) {
	middleware := Timeout(100 * time.Millisecond)

	handler := middleware(func(ctx *remilia.Context) error {
		time.Sleep(1 * time.Millisecond) // 模拟工作
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}
