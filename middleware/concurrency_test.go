package middleware

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestConcurrencyLimit 测试并发限流中间件
func TestConcurrencyLimit(t *testing.T) {
	t.Run("Drop 策略：超过限制立即丢弃", func(t *testing.T) {
		middleware := ConcurrencyLimit(2, ConcurrencyDrop, 0)

		var executing int32
		var completed int32
		var dropped int32

		handler := middleware(func(ctx *remilia.Context) error {
			current := atomic.AddInt32(&executing, 1)
			defer atomic.AddInt32(&executing, -1)

			// 验证并发数不超过限制
			assert.LessOrEqual(t, current, int32(2))

			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&completed, 1)
			return nil
		})

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				err := handler(ctx)
				if err != nil {
					atomic.AddInt32(&dropped, 1)
				}
			}()
		}

		wg.Wait()

		t.Logf("Completed: %d, Dropped: %d", completed, dropped)
		assert.Equal(t, int32(10), completed+dropped)
		assert.Greater(t, dropped, int32(0)) // 应该有请求被丢弃
	})

	t.Run("Block 策略：等待直到有空闲", func(t *testing.T) {
		middleware := ConcurrencyLimit(2, ConcurrencyBlock, 0)

		var maxConcurrent int32
		var currentConcurrent int32
		var completed int32

		handler := middleware(func(ctx *remilia.Context) error {
			current := atomic.AddInt32(&currentConcurrent, 1)
			defer atomic.AddInt32(&currentConcurrent, -1)

			// 更新最大并发数
			for {
				max := atomic.LoadInt32(&maxConcurrent)
				if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
					break
				}
			}

			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&completed, 1)
			return nil
		})

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}()
		}

		wg.Wait()

		assert.Equal(t, int32(10), completed)          // 所有请求都完成
		assert.LessOrEqual(t, maxConcurrent, int32(2)) // 最大并发不超过限制
		t.Logf("Max concurrent: %d, Completed: %d", maxConcurrent, completed)
	})

	t.Run("TryWait 策略：超时后丢弃", func(t *testing.T) {
		middleware := ConcurrencyLimit(2, ConcurrencyTryWait, 30*time.Millisecond)

		var completed int32
		var timedOut int32

		handler := middleware(func(ctx *remilia.Context) error {
			time.Sleep(100 * time.Millisecond) // 长时间占用
			atomic.AddInt32(&completed, 1)
			return nil
		})

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				err := handler(ctx)
				if err != nil && err.Error() == "concurrency limit exceeded (timeout)" {
					atomic.AddInt32(&timedOut, 1)
				}
			}()
		}

		wg.Wait()

		t.Logf("Completed: %d, TimedOut: %d", completed, timedOut)
		assert.Greater(t, timedOut, int32(0)) // 应该有请求超时
	})
}

// TestConcurrencyLimitSemaphoreRelease 测试信号量正确释放
func TestConcurrencyLimitSemaphoreRelease(t *testing.T) {
	t.Run("信号量应该在 handler 完成后释放", func(t *testing.T) {
		middleware := ConcurrencyLimit(3, ConcurrencyBlock, 0)

		var executing int32
		handler := middleware(func(ctx *remilia.Context) error {
			atomic.AddInt32(&executing, 1)
			defer atomic.AddInt32(&executing, -1)
			time.Sleep(50 * time.Millisecond)
			return nil
		})

		// 第一批：启动 3 个（占满）
		for i := 0; i < 3; i++ {
			go func() {
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}()
		}

		// 等待第一批占满
		time.Sleep(10 * time.Millisecond)
		assert.Equal(t, int32(3), atomic.LoadInt32(&executing))

		// 等待第一批完成
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, int32(0), atomic.LoadInt32(&executing))

		// 第二批：启动 3 个（应该能全部执行，证明信号量被释放了）
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}()
		}

		wg.Wait()
		// 如果信号量泄漏，第二批会卡住
		assert.Equal(t, int32(0), atomic.LoadInt32(&executing))
	})

	t.Run("handler 返回错误时信号量也应该释放", func(t *testing.T) {
		middleware := ConcurrencyLimit(2, ConcurrencyBlock, 0)

		handler := middleware(func(ctx *remilia.Context) error {
			time.Sleep(20 * time.Millisecond)
			return errors.New("handler error")
		})

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}()
		}

		// 如果信号量泄漏，会卡住
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// 正常完成
		case <-time.After(2 * time.Second):
			t.Fatal("Semaphore leaked: handlers are stuck")
		}
	})

	t.Run("handler panic 时信号量也应该释放", func(t *testing.T) {
		middleware := ConcurrencyLimit(2, ConcurrencyBlock, 0)

		handler := middleware(func(ctx *remilia.Context) error {
			time.Sleep(20 * time.Millisecond)
			panic("intentional panic")
		})

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					recover() // 捕获 panic
				}()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}()
		}

		// 如果信号量泄漏，会卡住
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// 正常完成
		case <-time.After(2 * time.Second):
			t.Fatal("Semaphore leaked after panic: handlers are stuck")
		}
	})
}

// TestConcurrencyLimitNoLeak 测试不泄漏信号量
func TestConcurrencyLimitNoLeak(t *testing.T) {
	t.Run("大量请求不泄漏信号量", func(t *testing.T) {
		middleware := ConcurrencyLimit(10, ConcurrencyBlock, 0)

		var completed int32
		handler := middleware(func(ctx *remilia.Context) error {
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&completed, 1)
			return nil
		})

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}()
		}

		// 设置超时
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			assert.Equal(t, int32(100), completed)
		case <-time.After(5 * time.Second):
			t.Fatalf("Semaphore leaked: only %d of 100 completed", atomic.LoadInt32(&completed))
		}
	})
}

// TestConcurrencyLimitEdgeCases 测试边界情况
func TestConcurrencyLimitEdgeCases(t *testing.T) {
	t.Run("maxInFlight = 1", func(t *testing.T) {
		middleware := ConcurrencyLimit(1, ConcurrencyBlock, 0)

		var maxConcurrent int32
		var currentConcurrent int32

		handler := middleware(func(ctx *remilia.Context) error {
			current := atomic.AddInt32(&currentConcurrent, 1)
			defer atomic.AddInt32(&currentConcurrent, -1)

			for {
				max := atomic.LoadInt32(&maxConcurrent)
				if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
					break
				}
			}

			time.Sleep(10 * time.Millisecond)
			return nil
		})

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
				_ = handler(ctx)
			}()
		}

		wg.Wait()
		assert.Equal(t, int32(1), maxConcurrent)
	})

	t.Run("maxInFlight = 0 应该使用默认值", func(t *testing.T) {
		middleware := ConcurrencyLimit(0, ConcurrencyDrop, 0)

		handler := middleware(func(ctx *remilia.Context) error {
			return nil
		})

		ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)
		err := handler(ctx)
		assert.NoError(t, err) // 应该使用默认值 100
	})
}

// BenchmarkConcurrencyLimit 基准测试
func BenchmarkConcurrencyLimit(b *testing.B) {
	middleware := ConcurrencyLimit(100, ConcurrencyBlock, 0)

	handler := middleware(func(ctx *remilia.Context) error {
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = handler(ctx)
		}
	})
}

// BenchmarkConcurrencyLimitWithWork 模拟实际工作的基准测试
func BenchmarkConcurrencyLimitWithWork(b *testing.B) {
	middleware := ConcurrencyLimit(10, ConcurrencyBlock, 0)

	handler := middleware(func(ctx *remilia.Context) error {
		time.Sleep(1 * time.Millisecond)
		return nil
	})

	ctx := remilia.NewContext(&dto.Payload{Type: dto.C2CMessageCreate}, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler(ctx)
	}
}

// TestConcurrencyLimitWithOtherMiddleware 测试与其他中间件组合
func TestConcurrencyLimitWithOtherMiddleware(t *testing.T) {
	t.Run("ConcurrencyLimit + Timeout", func(t *testing.T) {
		engine := remilia.NewEngine()

		engine.Use(ConcurrencyLimit(2, ConcurrencyDrop, 0))
		engine.Use(Timeout(100 * time.Millisecond))

		var executed int32
		engine.OnC2C().Handle(func(ctx *remilia.Context) {
			atomic.AddInt32(&executed, 1)
			time.Sleep(10 * time.Millisecond)
		})

		// 并发发送多个事件
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				event := &dto.Payload{Type: dto.C2CMessageCreate}
				ctx := remilia.NewContext(event, nil)
				engine.ProcessEvent(ctx)
			}()
		}

		wg.Wait()

		t.Logf("Executed: %d", executed)
		// 由于并发限制，不是所有请求都能执行
	})
}
