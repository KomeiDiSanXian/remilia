package main

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KomeiDiSanXian/remilia"
	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/middleware"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
	"github.com/tidwall/gjson"
)

// 测试配置
type BenchmarkConfig struct {
	Duration          time.Duration // 测试持续时间
	ConcurrentClients int           // 并发客户端数量
	MessageRate       int           // 每秒发送消息数（每个客户端）
	EnableMiddleware  bool          // 是否启用中间件
	EnableMetrics     bool          // 是否启用指标收集
	EventType         dto.EventType // 测试的事件类型
}

// 性能指标
type PerformanceMetrics struct {
	TotalMessages   atomic.Int64
	SuccessMessages atomic.Int64
	FailedMessages  atomic.Int64
	TotalLatency    atomic.Int64 // 纳秒
	MinLatency      atomic.Int64 // 纳秒
	MaxLatency      atomic.Int64 // 纳秒
	StartTime       time.Time
	EndTime         time.Time
}

// MockAdapter 模拟的适配器，用于注入测试消息
type MockAdapter struct {
	eventCh chan *dto.Payload
	handler func(*dto.Payload)
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running atomic.Bool
}

func NewMockAdapter(bufferSize int) *MockAdapter {
	return &MockAdapter{
		eventCh: make(chan *dto.Payload, bufferSize),
	}
}

func (a *MockAdapter) Start(ctx context.Context, handler func(*dto.Payload)) error {
	if !a.running.CompareAndSwap(false, true) {
		return fmt.Errorf("adapter already running")
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.handler = handler

	for i := 0; i < runtime.NumCPU()*2; i++ { // 启动cpu核数*2个worker
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for {
				select {
				case <-a.ctx.Done():
					return
				case event := <-a.eventCh:
					if event != nil {
						handler(event)
					}
				}
			}
		}()
	}
	return nil
}

func (a *MockAdapter) Stop(ctx context.Context) error {
	if !a.running.Load() {
		return nil
	}

	if a.cancel != nil {
		a.cancel()
	}

	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.running.Store(false)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// InjectEvent 注入测试事件
func (a *MockAdapter) InjectEvent(payload *dto.Payload) {
	select {
	case a.eventCh <- payload:
	default:
		// Channel 满了，丢弃
	}
}

// MockOpenAPI 模拟的 OpenAPI，用于测试
type MockOpenAPI struct {
	responseDelay time.Duration
	failRate      float64 // 失败率 0.0-1.0
	callCount     atomic.Int64
}

func (m *MockOpenAPI) SingleChat(openid string, msg *dto.Message) (gjson.Result, error) {
	m.callCount.Add(1)
	if m.responseDelay > 0 {
		time.Sleep(m.responseDelay)
	}
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) GroupChat(groupOpenid string, msg *dto.Message) (gjson.Result, error) {
	m.callCount.Add(1)
	if m.responseDelay > 0 {
		time.Sleep(m.responseDelay)
	}
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) SingleRichMedia(openid string, media *dto.Media) (gjson.Result, error) {
	m.callCount.Add(1)
	if m.responseDelay > 0 {
		time.Sleep(m.responseDelay)
	}
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) GroupRichMedia(groupOpenid string, media *dto.Media) (gjson.Result, error) {
	m.callCount.Add(1)
	if m.responseDelay > 0 {
		time.Sleep(m.responseDelay)
	}
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) SingleReset(openid, messageID string) (gjson.Result, error) {
	m.callCount.Add(1)
	return gjson.Result{}, nil
}

func (m *MockOpenAPI) GroupReset(groupOpenid, messageID string) (gjson.Result, error) {
	m.callCount.Add(1)
	return gjson.Result{}, nil
}

// ThroughputTest 吞吐量测试
type ThroughputTest struct {
	config  BenchmarkConfig
	metrics *PerformanceMetrics
	bot     *remilia.Bot
	adapter *MockAdapter
	engine  *engine.Engine
	mockAPI *MockOpenAPI
}

func NewThroughputTest(cfg BenchmarkConfig) *ThroughputTest {
	return &ThroughputTest{
		config:  cfg,
		metrics: &PerformanceMetrics{},
	}
}

func (t *ThroughputTest) Setup() error {
	logger.Info("[Benchmark] Setting up test environment...")

	// 创建 engine
	t.engine = engine.NewEngine()

	// 添加中间件
	if t.config.EnableMiddleware {
		t.engine.Use(middleware.Recover())
		t.engine.Use(middleware.Logging())
	}

	// 注册测试处理器
	t.engine.On(t.config.EventType).Handle(func(ctx *eventctx.Context) error {
		startTime := time.Now()

		// 模拟消息处理
		var event dto.C2CMessageCreateEvent
		if err := ctx.DecodeEvent(&event); err != nil {
			t.metrics.FailedMessages.Add(1)
			return err
		}

		// 模拟回复
		msg := &dto.Message{
			Type:    dto.TextMessage,
			Content: "Echo: " + event.Content,
		}

		if t.mockAPI != nil {
			_, err := t.mockAPI.SingleChat(event.Author.UserOpenID, msg)
			if err != nil {
				t.metrics.FailedMessages.Add(1)
				return err
			}
		}

		// 记录延迟
		latency := time.Since(startTime).Nanoseconds()
		t.metrics.TotalLatency.Add(latency)

		// 更新最小/最大延迟
		for {
			oldMin := t.metrics.MinLatency.Load()
			if oldMin == 0 || latency < oldMin {
				if t.metrics.MinLatency.CompareAndSwap(oldMin, latency) {
					break
				}
			} else {
				break
			}
		}

		for {
			oldMax := t.metrics.MaxLatency.Load()
			if latency > oldMax {
				if t.metrics.MaxLatency.CompareAndSwap(oldMax, latency) {
					break
				}
			} else {
				break
			}
		}

		t.metrics.SuccessMessages.Add(1)
		return nil
	})

	// 创建 Mock Adapter
	bufferSize := t.config.ConcurrentClients * t.config.MessageRate * 10
	t.adapter = NewMockAdapter(bufferSize)

	// 创建 Mock OpenAPI
	t.mockAPI = &MockOpenAPI{
		responseDelay: 1 * time.Millisecond, // 模拟 API 延迟
	}

	// 创建 Bot（不使用真实的 Token Manager）
	t.bot = remilia.NewBot(t.adapter, t.engine)

	logger.Info("[Benchmark] Setup completed")
	return nil
}

func (t *ThroughputTest) Run() error {
	logger.Infof("[Benchmark] Starting throughput test...")
	logger.Infof("[Benchmark] Duration: %v", t.config.Duration)
	logger.Infof("[Benchmark] Concurrent Clients: %d", t.config.ConcurrentClients)
	logger.Infof("[Benchmark] Message Rate per Client: %d msg/s", t.config.MessageRate)
	logger.Infof("[Benchmark] Event Type: %s", t.config.EventType)

	// 启动 Bot
	if err := t.bot.Start(); err != nil {
		return fmt.Errorf("failed to start bot: %w", err)
	}

	// 等待系统稳定
	time.Sleep(500 * time.Millisecond)

	// 开始测试
	t.metrics.StartTime = time.Now()

	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), t.config.Duration)
	defer cancel()

	// 启动消息生成器
	for i := 0; i < t.config.ConcurrentClients; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()
			t.generateMessages(ctx, clientID)
		}(i)
	}

	// 等待所有生成器完成
	wg.Wait()
	t.metrics.EndTime = time.Now()

	logger.Info("[Benchmark] Test completed, waiting for message processing...")

	// 给足够的时间让所有消息处理完成
	time.Sleep(3 * time.Second)

	logger.Info("[Benchmark] Test finished (skipping Bot.Stop to avoid WaitGroup issues)")

	// 注意：为了避免 WaitGroup 重用问题，我们不调用 Bot.Stop()
	// 在多场景测试中，每个场景使用新的实例，旧实例会被垃圾回收
	// 在实际生产环境中，应该正确调用 Stop() 方法

	return nil
}

func (t *ThroughputTest) generateMessages(ctx context.Context, clientID int) {
	ticker := time.NewTicker(time.Second / time.Duration(t.config.MessageRate))
	defer ticker.Stop()

	messageID := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			messageID++
			payload := t.createTestPayload(clientID, messageID)
			t.adapter.InjectEvent(payload)
			t.metrics.TotalMessages.Add(1)
		}
	}
}

func (t *ThroughputTest) createTestPayload(clientID, messageID int) *dto.Payload {
	eventID := dto.EventID(fmt.Sprintf("test-%d-%d-%d", clientID, messageID, time.Now().UnixNano()))
	userID := fmt.Sprintf("user-%d", clientID)

	event := dto.C2CMessageCreateEvent{
		MessageCreateEvent: dto.MessageCreateEvent{
			ID:      eventID,
			Content: fmt.Sprintf("Test message %d from client %d", messageID, clientID),
			Author: dto.Author{
				UserOpenID: userID,
			},
		},
	}

	return &dto.Payload{
		ID:        eventID,
		Type:      t.config.EventType,
		Operation: dto.Dispatch,
		Detail:    []byte(fmt.Sprintf(`{"id":"%s","content":"%s","author":{"user_openid":"%s"}}`, eventID, event.Content, event.Author.UserOpenID)),
	}
}

func (t *ThroughputTest) Cleanup() error {
	logger.Info("[Benchmark] Cleaning up resources...")

	// 确保 adapter 完全停止
	if t.adapter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := t.adapter.Stop(ctx); err != nil {
			logger.WithError(err).Warn("[Benchmark] Adapter stop error")
		}
	}

	// 等待一下确保所有 goroutine 退出
	time.Sleep(500 * time.Millisecond)

	logger.Info("[Benchmark] Cleanup completed")
	return nil
}

func (t *ThroughputTest) PrintResults() {
	duration := t.metrics.EndTime.Sub(t.metrics.StartTime)
	total := t.metrics.TotalMessages.Load()
	success := t.metrics.SuccessMessages.Load()
	failed := t.metrics.FailedMessages.Load()
	processing := total - success - failed

	throughput := float64(success) / duration.Seconds()
	avgLatency := time.Duration(0)
	if success > 0 {
		avgLatency = time.Duration(t.metrics.TotalLatency.Load() / success)
	}

	minLatency := time.Duration(t.metrics.MinLatency.Load())
	maxLatency := time.Duration(t.metrics.MaxLatency.Load())

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📊 吞吐量测试结果")
	fmt.Println(strings.Repeat("=", 80))

	fmt.Printf("\n⏱️  测试配置:\n")
	fmt.Printf("   持续时间:         %v\n", t.config.Duration)
	fmt.Printf("   并发客户端:       %d\n", t.config.ConcurrentClients)
	fmt.Printf("   每客户端速率:     %d msg/s\n", t.config.MessageRate)
	fmt.Printf("   目标总速率:       %d msg/s\n", t.config.ConcurrentClients*t.config.MessageRate)
	fmt.Printf("   事件类型:         %s\n", t.config.EventType)
	fmt.Printf("   中间件:           %v\n", t.config.EnableMiddleware)

	fmt.Printf("\n📈 性能指标:\n")
	fmt.Printf("   总消息数:         %d\n", total)
	fmt.Printf("   成功处理:         %d (%.2f%%)\n", success, float64(success)/float64(total)*100)
	fmt.Printf("   处理失败:         %d (%.2f%%)\n", failed, float64(failed)/float64(total)*100)
	fmt.Printf("   处理中/丢失:      %d (%.2f%%)\n", processing, float64(processing)/float64(total)*100)

	fmt.Printf("\n🚀 吞吐量:\n")
	fmt.Printf("   实际吞吐量:       %.2f msg/s\n", throughput)
	fmt.Printf("   目标达成率:       %.2f%%\n", throughput/float64(t.config.ConcurrentClients*t.config.MessageRate)*100)

	fmt.Printf("\n⏰ 延迟统计:\n")
	fmt.Printf("   平均延迟:         %v\n", avgLatency)
	fmt.Printf("   最小延迟:         %v\n", minLatency)
	fmt.Printf("   最大延迟:         %v\n", maxLatency)

	if t.mockAPI != nil {
		fmt.Printf("\n🔌 API 调用:\n")
		fmt.Printf("   总调用次数:       %d\n", t.mockAPI.callCount.Load())
	}

	fmt.Println("\n" + strings.Repeat("=", 80))
}

func main() {
	// 测试场景
	scenarios := []struct {
		name   string
		config BenchmarkConfig
	}{
		{
			name: "低负载测试 (100 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 10,
				MessageRate:       10,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
		{
			name: "中等负载测试 (1000 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 50,
				MessageRate:       20,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
		{
			name: "高负载测试 (5000 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 100,
				MessageRate:       50,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
		{
			name: "极限负载测试 (10000 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 200,
				MessageRate:       50,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
		{
			name: "400并发客户端高消息率测试 (20000 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 400,
				MessageRate:       50,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
		{
			name: "600并发客户端高消息率测试 (30000 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 600,
				MessageRate:       50,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
		{
			name: "1000并发客户端高消息率测试 (50000 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 1000,
				MessageRate:       50,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
		{
			name: "2000并发压测 (100000 msg/s)",
			config: BenchmarkConfig{
				Duration:          10 * time.Second,
				ConcurrentClients: 2000,
				MessageRate:       50,
				EnableMiddleware:  true,
				EventType:         dto.C2CMessageCreate,
			},
		},
	}

	// 运行所有场景
	for i, scenario := range scenarios {
		fmt.Printf("\n\n🧪 场景 %d: %s\n", i+1, scenario.name)
		fmt.Println(strings.Repeat("-", 80))

		test := NewThroughputTest(scenario.config)
		if err := test.Setup(); err != nil {
			logger.WithError(err).Fatal("Failed to setup test")
		}

		if err := test.Run(); err != nil {
			logger.WithError(err).Fatal("Failed to run test")
		}

		test.PrintResults()

		// 清理资源
		if err := test.Cleanup(); err != nil {
			logger.WithError(err).Warn("Failed to cleanup test")
		}

		// 场景间短暂冷却
		if i < len(scenarios)-1 {
			logger.Infof("Waiting for cooldown before next scenario (3s)...")
			time.Sleep(3 * time.Second)
		}
	}

	fmt.Println("\n\n✅ 所有测试场景完成!")
	fmt.Println("💡 提示: 测试程序未调用 Bot.Stop() 以避免 WaitGroup 重用问题")
	fmt.Println("   在生产环境中应该正确调用 Stop() 方法进行资源清理")

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("📊 性能上限分析")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("\n分析测试结果，找到系统的实际吞吐量上限：")
	fmt.Println("1. 查看哪个场景的吞吐量开始趋于平稳")
	fmt.Println("2. 达成率开始显著下降的点即为系统上限")
	fmt.Println("3. CPU 核心数 x 单核吞吐量 ≈ 理论最大吞吐量")
	fmt.Printf("4. 当前 CPU 核心数: %d\n", runtime.NumCPU())
	fmt.Printf("5. MockAdapter Workers: %d\n", runtime.NumCPU()*2)
	fmt.Println(strings.Repeat("=", 80))
}
