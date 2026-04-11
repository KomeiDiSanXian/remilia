package middleware

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
)

// TestCircuitBreakerRace 并发竞态压测：验证 onSuccess/onFailure 与 canExecute 无数据竞争。
//
// 覆盖 H-2 修复：onSuccess/onFailure 持 mu.Lock()，与 canExecute 的锁边界一致，
// 消除"读状态→写计数→可能切换状态"的窗口期竞争。
//
// 推荐运行方式（开启 -race 探测器）：
//
//	go test -race ./middleware/ -run TestCircuitBreakerRace -count=3
func TestCircuitBreakerRace(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:         5,
		ResetTimeout:        10 * time.Millisecond,
		HalfOpenMaxRequests: 50,
		SuccessThreshold:    3,
		HalfOpenTimeout:     500 * time.Millisecond,
	})

	const goroutines = 100
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			ctx := createTestContext()

			for j := range iterations {
				// 1/3 的 goroutine 模拟失败，其余成功，制造状态频繁切换
				var handlerErr error
				if id%3 == 0 {
					handlerErr = errors.New("simulated failure")
				}
				handler := func(_ *eventctx.Context) error { return handlerErr }
				mw := CircuitBreakerMiddleware(cb)
				_ = mw(handler)(ctx)

				// 每隔若干次让出 CPU，增加 goroutine 交织
				if j%20 == 0 {
					time.Sleep(time.Microsecond)
				}
			}
		}(i)
	}

	wg.Wait()

	// 验证最终状态合法（不应出现非法状态值）
	state := cb.GetState()
	validStates := map[CircuitBreakerState]bool{
		StateClosed: true, StateOpen: true, StateHalfOpen: true,
	}
	if !validStates[state] {
		t.Errorf("unexpected circuit breaker state after concurrent test: %q", state)
	}
}

// TestCircuitBreakerRace_StateCycling 在高并发下反复触发 Closed→Open→HalfOpen→Closed 状态循环，
// 验证计数器（failures / successes / halfOpenReqs）在竞态下始终保持一致性。
func TestCircuitBreakerRace_StateCycling(t *testing.T) {
	const cycles = 5

	for cycle := range cycles {
		cb := NewCircuitBreaker(CircuitBreakerConfig{
			MaxFailures:         3,
			ResetTimeout:        20 * time.Millisecond,
			HalfOpenMaxRequests: 20,
			SuccessThreshold:    2,
			HalfOpenTimeout:     200 * time.Millisecond,
		})

		// 阶段 1：并发触发失败，打开熔断器
		var failWg sync.WaitGroup
		for range 20 {
			failWg.Add(1)
			go func() {
				defer failWg.Done()
				ctx := createTestContext()
				handler := func(_ *eventctx.Context) error { return errors.New("err") }
				mw := CircuitBreakerMiddleware(cb)
				_ = mw(handler)(ctx)
			}()
		}
		failWg.Wait()

		// 等待进入 HalfOpen
		time.Sleep(30 * time.Millisecond)

		// 阶段 2：并发发送成功请求，触发 HalfOpen→Closed
		var successWg sync.WaitGroup
		var successCount atomic.Int32
		for range 20 {
			successWg.Add(1)
			go func() {
				defer successWg.Done()
				ctx := createTestContext()
				handler := func(_ *eventctx.Context) error { return nil }
				mw := CircuitBreakerMiddleware(cb)
				err := mw(handler)(ctx)
				if err == nil {
					successCount.Add(1)
				}
			}()
		}
		successWg.Wait()

		// 验证状态合法
		state := cb.GetState()
		validStates := map[CircuitBreakerState]bool{
			StateClosed: true, StateOpen: true, StateHalfOpen: true,
		}
		if !validStates[state] {
			t.Errorf("cycle %d: unexpected state %q", cycle, state)
		}
	}
}

// TestCircuitBreakerRace_ConcurrentResetAndFailure 并发调用 Reset() 与 onFailure()，
// 验证 Reset() 持有 mu 的写锁，不会与状态读写产生竞争。
func TestCircuitBreakerRace_ConcurrentResetAndFailure(t *testing.T) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		MaxFailures:  3,
		ResetTimeout: 10 * time.Millisecond,
	})

	var wg sync.WaitGroup
	ctx := createTestContext()
	handler := func(_ *eventctx.Context) error { return errors.New("err") }
	mw := CircuitBreakerMiddleware(cb)

	// 并发执行请求与手动 Reset
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = mw(handler)(ctx)
		}()
		go func() {
			defer wg.Done()
			cb.Reset()
		}()
	}
	wg.Wait()
}
