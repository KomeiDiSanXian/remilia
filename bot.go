package remilia

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
	"github.com/KomeiDiSanXian/remilia/openapi"
	"github.com/KomeiDiSanXian/remilia/openapi/auth/token"
	"github.com/KomeiDiSanXian/remilia/openapi/dto"
)

const (
	// DefaultShutdownTimeout is the default timeout for graceful shutdown
	DefaultShutdownTimeout = 30 * time.Second
	// DefaultStartTimeout is the default timeout for component OnStart phase
	DefaultStartTimeout = 30 * time.Second
)

// Bot 是对 Engine 的高级封装，提供完整的生命周期管理
type Bot struct {
	engine       *engine.Engine
	adapter      Adapter
	lifecycle    *lifecycle.Manager
	health       *health.Check
	config       *Config
	openAPI      openapi.OpenAPI // OpenAPI client for sending messages
	tokenManager *token.Manager  // Token manager for lifecycle management
	botInfo      *dto.BotInfo    // 保存 BotInfo，用于 Start() 时延迟初始化 tokenManager

	// 根 Context：Bot 运行期间所有后台 goroutine 的上级 context
	// Start() 时创建，Stop() 时取消，确保所有依赖组件随 Bot 一起退出
	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu        sync.RWMutex
	running   bool
	starting  bool // Prevents concurrent Start() calls
	startTime time.Time
	stopTime  time.Time
}

// Config Bot 配置
type Config struct {
	Name    string
	Version string
	Debug   bool
}

// NewBot 创建新的 Bot 实例
func NewBot(adapter Adapter, engine *engine.Engine, opts ...Option) *Bot {
	// 验证必需参数
	if adapter == nil {
		logger.Panic("[Bot] adapter cannot be nil")
	}
	if engine == nil {
		logger.Panic("[Bot] engine cannot be nil")
	}

	b := &Bot{
		engine:    engine,
		adapter:   adapter,
		lifecycle: lifecycle.NewManager(),
		config: &Config{
			Name:    "remilia-bot",
			Version: "0.9.0",
			Debug:   false,
		},
	}

	// 应用选项
	for _, opt := range opts {
		opt(b)
	}

	// 初始化健康检查
	b.health = health.NewCheck()

	// 添加 Bot 自己的 checker
	b.health.AddChecker(NewBotStatusChecker(b))

	// 添加 Adapter checker
	if adapter != nil {
		b.health.AddChecker(NewAdapterHealthChecker(adapter))
	}

	// 添加 engine checker
	if engine != nil {
		b.health.AddChecker(health.NewEngineHealthChecker(engine))
	}

	// 注册组件到生命周期管理器
	b.lifecycle.Register(lifecycle.NewSimpleComponent(
		"adapter",
		nil, // onStart
		func(ctx context.Context) error {
			// onRun: adapter.Start 是阻塞的，适合在这里运行
			return b.adapter.Start(ctx, b.handleEvent)
		},
		func(ctx context.Context) error {
			// onStop
			return b.adapter.Stop(ctx)
		},
	))

	b.lifecycle.Register(lifecycle.NewSimpleComponent(
		"engine",
		func(ctx context.Context) error {
			// onStart: engine 初始化（如果需要）
			return nil
		},
		nil, // onRun: engine 没有阻塞循环，使用默认行为（等待 ctx.Done）
		func(ctx context.Context) error {
			// onStop
			return b.engine.Shutdown(ctx)
		},
	))

	return b
}

// NewBotWithInfo 创建带 OpenAPI 支持的 Bot 实例
// 这个构造函数会自动初始化 OpenAPI client，用于发送消息
func NewBotWithInfo(adapter Adapter, engine *engine.Engine, botInfo *dto.BotInfo, opts ...Option) *Bot {
	b := NewBot(adapter, engine, opts...)

	// 保存 botInfo，在 Start() 时使用 rootCtx 创建 tokenManager
	// 避免在构造期创建后台 goroutine（此时还没有根 context）
	if botInfo != nil {
		b.botInfo = botInfo

		// 预初始化 openAPI client（openAPI 本身不启动 goroutine，只在调用时使用 tokenManager）
		// tokenManager 的实际创建延迟到 Start()
		tmpTokenManager := token.NewManagerWithContext(context.Background(), botInfo)
		b.tokenManager = tmpTokenManager
		b.openAPI = openapi.New(tmpTokenManager)

		// 添加 Token Manager health checker
		b.health.AddChecker(NewTokenManagerHealthChecker(b))

		logger.Info("[Bot] OpenAPI client initialized (token manager will rebind to root context on Start)")
	} else {
		logger.Warn("[Bot] BotInfo is nil, OpenAPI client not initialized")
	}

	return b
}

// Start 启动 Bot
func (b *Bot) Start() error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		logger.Warn("[Bot] Already running")
		return nil
	}
	if b.starting {
		b.mu.Unlock()
		return errutil.New("bot is already starting")
	}
	b.starting = true
	b.mu.Unlock()

	logger.WithFields(logger.Fields{
		"name":    b.config.Name,
		"version": b.config.Version,
	}).Info("[Bot] Starting...")

	// 创建根 context：Bot 运行期间所有后台 goroutine 的上级
	// Stop() 时调用 rootCancel 统一取消所有依赖组件
	rootCtx, rootCancel := context.WithCancel(context.Background())

	// 将 tokenManager 重新绑定到 rootCtx，替换构造时创建的临时实例
	// 这样 token 刷新 goroutine 会随 Bot 根 context 一起退出
	if b.botInfo != nil {
		oldManager := b.tokenManager
		newManager := token.NewManagerWithContext(rootCtx, b.botInfo)
		b.tokenManager = newManager
		b.openAPI = openapi.New(newManager)
		// 停止旧的临时 manager（它使用 context.Background()，不会被 rootCtx 取消）
		if oldManager != nil {
			go oldManager.Stop()
		}
	}

	// 为 OnStart 阶段创建带超时的子 context（不影响 rootCtx）
	startCtx, startCancel := context.WithTimeout(rootCtx, DefaultStartTimeout)
	defer startCancel()

	// 将 rootCtx 传给 lifecycle.Start，使 OnRun goroutine 从 rootCtx 派生
	err := b.lifecycle.Start(startCtx)

	b.mu.Lock()
	b.starting = false
	if err != nil {
		b.mu.Unlock()
		rootCancel() // 启动失败，立即释放 rootCtx
		logger.WithError(err).Error("[Bot] Failed to start")
		return err
	}
	b.running = true
	b.startTime = time.Now()
	b.rootCtx = rootCtx
	b.rootCancel = rootCancel
	b.mu.Unlock()

	logger.Info("[Bot] Started successfully")
	return nil
}

// Context 返回 Bot 的根 context。
//
// 此 context 在 Bot.Start() 时创建，Bot.Stop() 时取消。
// 可用于：
//   - 创建与 Bot 生命周期绑定的后台 goroutine
//   - 初始化 AdaptiveRateLimiter 等组件，使其随 Bot 自动退出
//
// 示例：
//
//	arl := middleware.NewAdaptiveRateLimiterWithContext(bot.Context(), config)
//
// 注意：在 Bot.Start() 调用之前，此方法返回 context.Background()。
func (b *Bot) Context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.rootCtx != nil {
		return b.rootCtx
	}
	return context.Background()
}

// handleEvent 处理事件
func (b *Bot) handleEvent(payload *dto.Payload) {
	// 修复 #17：防护 nil payload，虽然 adapter 层通常有保证，但防御性编程更健壮
	if payload == nil {
		logger.Warn("[Bot] Received nil payload, skipping")
		return
	}

	start := time.Now()

	if b.config.Debug {
		logger.WithFields(logger.Fields{
			"type": payload.Type,
			"id":   payload.ID,
		}).Debug("[Bot] Event received")
	}

	// 安全检查：确保 openAPI client 已初始化
	api := b.openAPI
	if api == nil {
		logger.Warn("[Bot] OpenAPI client not initialized, event processing may fail")
		// 仍然继续处理，context 可以处理 nil API
	}

	// 从 pool 获取 Context，处理完毕后归还，减少 per-event 堆分配。
	ctx := eventctx.AcquireContext(payload, api)
	defer eventctx.ReleaseContext(ctx)

	// 使用 engine 处理事件
	b.engine.ProcessEvent(ctx)

	// 记录事件处理耗时（Debug 模式下记录，生产中可通过 metrics 上报）
	elapsed := time.Since(start)
	if b.config.Debug {
		logger.WithFields(logger.Fields{
			"type":    payload.Type,
			"id":      payload.ID,
			"elapsed": elapsed,
		}).Debug("[Bot] Event processed")
	}
}

// Stop 优雅关闭 Bot
func (b *Bot) Stop(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		logger.Warn("[Bot] Not running")
		return nil
	}
	b.running = false
	b.stopTime = time.Now()
	rootCancel := b.rootCancel
	b.rootCancel = nil
	b.mu.Unlock()

	logger.Info("[Bot] Shutting down...")

	// 先停止 lifecycle（包括 adapter.Stop 和 engine.Shutdown）
	err := b.lifecycle.Stop(ctx)

	// 取消根 context：通知所有与 Bot 绑定的后台 goroutine（token manager、adaptive limiter 等）退出
	if rootCancel != nil {
		rootCancel()
	}

	// token manager.Stop() 保持向后兼容（rootCancel 已触发，这里是双重保险）
	if b.tokenManager != nil {
		logger.Debug("[Bot] Stopping token manager...")
		b.tokenManager.Stop()
	}

	if err != nil {
		logger.WithError(err).Error("[Bot] Stop completed with errors")
		return err
	}
	logger.Info("[Bot] Stop complete")
	return nil
}

// Shutdown 使用默认超时时间优雅关闭 Bot
// 这是 Stop 的便捷包装，使用 DefaultShutdownTimeout 作为超时时间
func (b *Bot) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()
	return b.Stop(ctx)
}

// Engine 返回 Bot 的 Engine 实例
func (b *Bot) Engine() *engine.Engine {
	return b.engine
}

// GetEngine 返回 Bot 的 Engine 实例（别名）
func (b *Bot) GetEngine() *engine.Engine {
	return b.engine
}

// IsRunning 返回 Bot 是否正在运行
func (b *Bot) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

// Uptime 返回 Bot 运行时间
func (b *Bot) Uptime() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.running {
		if !b.stopTime.IsZero() {
			return b.stopTime.Sub(b.startTime)
		}
		return 0
	}

	return time.Since(b.startTime)
}

// Config 获取配置
func (b *Bot) Config() *Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

// Health 健康检查
func (b *Bot) Health() health.CheckResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.health.Check(ctx)
}

// HealthCheck 返回 health.Check 实例（用于高级配置）
func (b *Bot) HealthCheck() *health.Check {
	return b.health
}

// State 返回生命周期状态
func (b *Bot) State() lifecycle.State {
	return b.lifecycle.State()
}

// OnAny 注册处理所有事件的规则（convenience method）
func (b *Bot) OnAny(rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnAny(rule...)
}

// OnC2C 注册处理私聊消息的规则（convenience method）
func (b *Bot) OnC2C(rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnC2C(rule...)
}

// OnGroupAt 注册处理群@消息的规则（convenience method）
func (b *Bot) OnGroupAt(rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnGroupAt(rule...)
}

// On 注册自定义规则（convenience method）
func (b *Bot) On(eventType dto.EventType, rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.On(eventType, rule...)
}

// WaitForShutdown 等待系统信号并优雅关闭
func (b *Bot) WaitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh

	logger.Info("[Bot] Received shutdown signal")

	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	if err := b.Stop(ctx); err != nil {
		logger.WithError(err).Error("[Bot] Shutdown failed")
	}
}
