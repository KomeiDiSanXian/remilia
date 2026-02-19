package chaos

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rcontext "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/stretchr/testify/assert"
)

// TestChaos_RandomFailures 测试随机失败场景
func TestChaos_RandomFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	var successCount, failCount int32

	// 注册会随机失败的处理器
	eng.OnCommand(dto.C2CMessageCreate, "/chaos").Handle(func(ctx *rcontext.Context) error {
		if rand.Float32() < 0.3 { // 30% 失败率
			atomic.AddInt32(&failCount, 1)
			return errors.New("random failure")
		}
		atomic.AddInt32(&successCount, 1)
		return nil
	})

	// 发送大量请求
	const totalRequests = 1000
	var wg sync.WaitGroup

	for range totalRequests {
		wg.Go(func() {
			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/chaos"}`),
			}
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
		})
	}

	wg.Wait()

	// 验证
	total := successCount + failCount
	assert.Equal(t, int32(totalRequests), total)
	t.Logf("成功: %d, 失败: %d, 失败率: %.2f%%", successCount, failCount, float64(failCount)/float64(total)*100)
}

// TestChaos_HighConcurrency 测试高并发场景
func TestChaos_HighConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	var processedCount int32

	eng.OnCommand(dto.C2CMessageCreate, "/concurrent").Handle(func(ctx *rcontext.Context) error {
		atomic.AddInt32(&processedCount, 1)
		// 模拟一些工作
		time.Sleep(time.Millisecond * time.Duration(rand.Intn(10)))
		return nil
	})

	// 高并发测试
	const concurrency = 500
	const requestsPerWorker = 10

	start := time.Now()
	var wg sync.WaitGroup

	for range concurrency {
		wg.Go(func() {
			for range requestsPerWorker {
				event := &dto.Payload{
					Type:   dto.C2CMessageCreate,
					Detail: []byte(`{"content": "/concurrent"}`),
				}
				ctx := rcontext.NewContext(event, nil)
				eng.ProcessEvent(ctx)
			}
		})
	}

	wg.Wait()
	duration := time.Since(start)

	// 验证
	expectedCount := int32(concurrency * requestsPerWorker)
	assert.Equal(t, expectedCount, processedCount)

	qps := float64(processedCount) / duration.Seconds()
	t.Logf("处理 %d 个请求，耗时: %v, QPS: %.2f", processedCount, duration, qps)
}

// TestChaos_MemoryPressure 测试内存压力
func TestChaos_MemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	// 注册会分配大量内存的处理器
	eng.OnCommand(dto.C2CMessageCreate, "/memory").Handle(func(ctx *rcontext.Context) error {
		// 分配 1MB 内存
		data := make([]byte, 1024*1024)
		_ = data
		time.Sleep(10 * time.Millisecond)
		return nil
	})

	// 并发发送请求创造内存压力
	const concurrency = 100
	var wg sync.WaitGroup

	for range concurrency {
		wg.Go(func() {
			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/memory"}`),
			}
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
		})
	}

	wg.Wait()
	t.Log("内存压力测试完成")
}

// TestChaos_RapidRegistrationUnregistration 测试快速注册/注销
func TestChaos_RapidRegistrationUnregistration(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	// 快速注册和删除匹配器
	const iterations = 1000
	for i := range iterations {
		matcher := eng.OnCommand(dto.C2CMessageCreate, fmt.Sprintf("/cmd%d", i))

		// 立即删除
		if i%2 == 0 {
			matcher.Delete()
		}
	}

	// 验证状态一致性
	count := eng.GetMatcherCount()
	t.Logf("剩余匹配器数量: %d", count)
	assert.True(t, count > 0 && count <= iterations)
}

// TestChaos_MixedOperations 测试混合操作
func TestChaos_MixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	var wg sync.WaitGroup
	duration := 5 * time.Second
	stopCh := make(chan struct{})

	// Worker 1: 持续注册匹配器
	wg.Go(func() {
		i := 0
		for {
			select {
			case <-stopCh:
				return
			default:
				eng.OnCommand(dto.C2CMessageCreate, fmt.Sprintf("/cmd%d", i))
				i++
				time.Sleep(10 * time.Millisecond)
			}
		}
	})

	// Worker 2: 持续处理事件
	wg.Go(func() {
		for {
			select {
			case <-stopCh:
				return
			default:
				event := &dto.Payload{
					Type:   dto.C2CMessageCreate,
					Detail: []byte(`{"content": "/cmd0"}`),
				}
				ctx := rcontext.NewContext(event, nil)
				eng.ProcessEvent(ctx)
				time.Sleep(5 * time.Millisecond)
			}
		}
	})

	// Worker 3: 持续删除所有匹配器
	wg.Go(func() {
		for {
			select {
			case <-stopCh:
				return
			default:
				eng.DeleteAllMatchers()
				time.Sleep(100 * time.Millisecond)
			}
		}
	})

	// 运行一段时间后停止
	time.Sleep(duration)
	close(stopCh)
	wg.Wait()

	t.Log("混合操作测试完成")
}

// TestChaos_SlowHandler 测试慢处理器
func TestChaos_SlowHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	var slowCount, fastCount int32

	// 慢处理器
	eng.OnCommand(dto.C2CMessageCreate, "/slow").Handle(func(ctx *rcontext.Context) error {
		time.Sleep(500 * time.Millisecond)
		atomic.AddInt32(&slowCount, 1)
		return nil
	})

	// 快处理器
	eng.OnCommand(dto.C2CMessageCreate, "/fast").Handle(func(ctx *rcontext.Context) error {
		atomic.AddInt32(&fastCount, 1)
		return nil
	})

	// 混合发送快慢请求
	var wg sync.WaitGroup
	const totalRequests = 50

	start := time.Now()

	for i := range totalRequests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			var cmd string
			if i%5 == 0 { // 20% 慢请求
				cmd = "/slow"
			} else {
				cmd = "/fast"
			}

			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: fmt.Appendf(nil, `{"content": "%s"}`, cmd),
			}
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("慢请求: %d, 快请求: %d, 总耗时: %v", slowCount, fastCount, duration)
}

// TestChaos_TimeoutHandling 测试超时处理
func TestChaos_TimeoutHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	var timeoutCount, successCount int32

	eng.OnCommand(dto.C2CMessageCreate, "/timeout").Handle(func(ctx *rcontext.Context) error {
		// 检查 context 是否已超时
		select {
		case <-ctx.Context().Done():
			atomic.AddInt32(&timeoutCount, 1)
			return ctx.Context().Err()
		default:
		}

		// 模拟长时间处理
		time.Sleep(100 * time.Millisecond)
		atomic.AddInt32(&successCount, 1)
		return nil
	})

	// 发送带超时的请求
	const totalRequests = 50
	var wg sync.WaitGroup

	for range totalRequests {
		wg.Go(func() {

			// 创建带超时的 context
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/timeout"}`),
			}
			eventCtx := rcontext.NewContextWithContext(ctx, event, nil)
			eng.ProcessEvent(eventCtx)
		})
	}

	wg.Wait()

	t.Logf("超时: %d, 成功: %d", timeoutCount, successCount)
}

// TestChaos_ResourceExhaustion 测试资源耗尽
func TestChaos_ResourceExhaustion(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	// 设置较小的匹配器限制
	eng := engine.NewEngine()
	eng.SetMaxMatchers(100)
	defer eng.Close()

	// 尝试注册超过限制的匹配器
	for i := range 150 {
		eng.OnCommand(dto.C2CMessageCreate, fmt.Sprintf("/cmd%d", i))
	}

	// 验证不会超过限制
	count := eng.GetMatcherCount()
	assert.LessOrEqual(t, count, 100)
	t.Logf("匹配器数量: %d (限制: 100)", count)
}

// TestChaos_GracefulDegradation 测试优雅降级
func TestChaos_GracefulDegradation(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	var normalCount, degradedCount int32
	degradeMode := int32(0)

	eng.OnCommand(dto.C2CMessageCreate, "/service").Handle(func(ctx *rcontext.Context) error {
		if atomic.LoadInt32(&degradeMode) == 1 {
			// 降级模式：快速返回简化结果
			atomic.AddInt32(&degradedCount, 1)
			return nil
		}

		// 正常模式：完整处理
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&normalCount, 1)
		return nil
	})

	// 第一阶段：正常处理
	for range 20 {
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{"content": "/service"}`),
		}
		ctx := rcontext.NewContext(event, nil)
		eng.ProcessEvent(ctx)
	}

	// 模拟高负载，触发降级
	atomic.StoreInt32(&degradeMode, 1)

	// 第二阶段：降级处理
	for range 30 {
		event := &dto.Payload{
			Type:   dto.C2CMessageCreate,
			Detail: []byte(`{"content": "/service"}`),
		}
		ctx := rcontext.NewContext(event, nil)
		eng.ProcessEvent(ctx)
	}

	t.Logf("正常模式: %d, 降级模式: %d", normalCount, degradedCount)
	assert.Equal(t, int32(20), normalCount)
	assert.Equal(t, int32(30), degradedCount)
}

// TestChaos_CascadingFailures 测试级联失败
func TestChaos_CascadingFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	failureCount := int32(0)

	// 三个相互依赖的处理器
	eng.OnCommand(dto.C2CMessageCreate, "/service1").Handle(func(ctx *rcontext.Context) error {
		// Service1 依赖 Service2
		if atomic.LoadInt32(&failureCount) > 0 {
			return errors.New("service1 failed due to service2 failure")
		}
		return nil
	})

	eng.OnCommand(dto.C2CMessageCreate, "/service2").Handle(func(ctx *rcontext.Context) error {
		// Service2 可能失败
		if rand.Float32() < 0.5 {
			atomic.AddInt32(&failureCount, 1)
			return errors.New("service2 failed")
		}
		return nil
	})

	// 并发调用，观察级联失败
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(2)

		go func() {
			defer wg.Done()
			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/service2"}`),
			}
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
		}()

		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // 稍微延迟
			event := &dto.Payload{
				Type:   dto.C2CMessageCreate,
				Detail: []byte(`{"content": "/service1"}`),
			}
			ctx := rcontext.NewContext(event, nil)
			eng.ProcessEvent(ctx)
		}()
	}

	wg.Wait()
	t.Logf("级联失败次数: %d", failureCount)
}

// TestChaos_StressTest 综合压力测试
func TestChaos_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过混沌测试")
	}

	eng := engine.NewEngine()
	defer eng.Close()

	// 注册多个命令
	for i := range 10 {
		cmdNum := i
		eng.OnCommand(dto.C2CMessageCreate, fmt.Sprintf("/stress%d", i)).Handle(func(ctx *rcontext.Context) error {
			// 随机延迟
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(50)))

			// 随机失败
			if rand.Float32() < 0.1 {
				return fmt.Errorf("stress test error from cmd%d", cmdNum)
			}
			return nil
		})
	}

	// 高并发压力测试
	const (
		concurrency       = 200
		requestsPerWorker = 50
		testDuration      = 10 * time.Second
	)

	var (
		totalRequests int32
		successCount  int32
	)

	start := time.Now()
	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// 启动并发 worker
	for range concurrency {
		wg.Go(func() {

			for range requestsPerWorker {
				select {
				case <-stopCh:
					return
				default:
				}

				atomic.AddInt32(&totalRequests, 1)

				// 随机选择命令
				cmdNum := rand.Intn(10)
				event := &dto.Payload{
					Type:   dto.C2CMessageCreate,
					Detail: fmt.Appendf(nil, `{"content": "/stress%d"}`, cmdNum),
				}

				ctx := rcontext.NewContext(event, nil)
				eng.ProcessEvent(ctx)

				// 注意：由于 ProcessEvent 无返回值，我们统计总请求数
				atomic.AddInt32(&successCount, 1)

				// 随机间隔
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(10)))
			}
		})
	}

	// 等待完成或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有 worker 完成
	case <-time.After(testDuration):
		// 超时
		close(stopCh)
		<-done
	}

	duration := time.Since(start)
	qps := float64(totalRequests) / duration.Seconds()

	// 报告结果
	t.Logf("压力测试结果:")
	t.Logf("  总请求数: %d", totalRequests)
	t.Logf("  完成: %d", successCount)
	t.Logf("  耗时: %v", duration)
	t.Logf("  QPS: %.2f", qps)

	// 基本验证
	assert.Greater(t, successCount, int32(0))
}
