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
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const (
	// DefaultShutdownTimeout is the default timeout for graceful shutdown
	DefaultShutdownTimeout = 30 * time.Second
	// DefaultStartTimeout is the default timeout for component OnStart phase
	DefaultStartTimeout = 30 * time.Second
)

// Bot 是对 Engine 的高级封装，提供完整的生命周期管理
type Bot struct {
	engine        *engine.Engine
	adapter       engine.PlatformAdapter
	lifecycle     *lifecycle.Manager
	health        *health.Check
	config        *Config
	openAPI       openapi.OpenAPI // OpenAPI client for sending messages
	tokenManager  *token.Manager  // Token manager for lifecycle management
	botInfo       *dto.BotInfo    // 保存 BotInfo，用于 Start() 时延迟初始化 tokenManager
	pluginManager *plugin.Manager // 插件管理器（可选，通过 UsePlugins 注入）

	// platformRegistry 多平台适配器注册表（可选）
	// 若注册了平台适配器，事件会通过 handlePlatformEvent 处理
	platformRegistry *platform.Registry

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
//
// 注意：若 adapter 或 engine 为 nil，此函数会直接 panic。
// 推荐使用 [BotBuilder.Build] 代替，它返回错误而非 panic，
// 可由调用方优雅处理：
//
//	bot, err := remilia.NewBotBuilder().
//	    WithAdapter(adapter).
//	    WithEngine(engine).
//	    Build()
func NewBot(adapter engine.PlatformAdapter, e *engine.Engine, opts ...Option) *Bot {
	// 验证必需参数
	if adapter == nil {
		logger.Panic("[Bot] adapter cannot be nil")
	}
	if e == nil {
		logger.Panic("[Bot] engine cannot be nil")
	}

	b := &Bot{
		engine:    e,
		adapter:   adapter,
		lifecycle: lifecycle.NewManager(),
		config: &Config{
			Name:    "remilia-bot",
			Version: Version,
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
	if e != nil {
		b.health.AddChecker(health.NewEngineHealthChecker(e))
	}

	// 注册组件到生命周期管理器
	b.lifecycle.Register(lifecycle.NewSimpleComponent(
		"adapter",
		nil, // onStart
		func(ctx context.Context) error {
			// onRun: adapter.StartPlatform 是阻塞的，适合在这里运行
			return b.adapter.StartPlatform(ctx, b.handlePlatformEvent)
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

	// 若已注入多平台注册表，为每个平台适配器注册独立的生命周期组件
	// 各平台适配器在独立 goroutine 中运行，ctx 取消时统一退出
	if b.platformRegistry != nil {
		for _, pa := range b.platformRegistry.All() {
			name := "platform:" + pa.Platform()
			b.lifecycle.Register(lifecycle.NewSimpleComponent(
				name,
				nil,
				func(ctx context.Context) error {
					return pa.StartPlatform(ctx, b.handlePlatformEvent)
				},
				func(ctx context.Context) error {
					return pa.Stop(ctx)
				},
			))
		}
	}

	return b
}

// NewBotWithInfo 创建带 OpenAPI 支持的 Bot 实例
// 这个构造函数会自动初始化 OpenAPI client，用于发送消息
func NewBotWithInfo(adapter engine.PlatformAdapter, eng *engine.Engine, botInfo *dto.BotInfo, opts ...Option) *Bot {
	b := NewBot(adapter, eng, opts...)

	if botInfo != nil {
		// 仅存储 botInfo，延迟到 Start() 时再创建 tokenManager。
		// 这样可以避免在构造阶段启动后台 goroutine（此时还没有根 context），
		// 防止用户只构造 Bot 而不调用 Start() 时出现 goroutine 泄漏。
		b.botInfo = botInfo

		// 添加 Token Manager health checker（checker 本身不依赖 tokenManager 是否已启动）
		b.health.AddChecker(NewTokenManagerHealthChecker(b))

		logger.Info("[Bot] BotInfo stored; OpenAPI client and token manager will be initialized on Start()")
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

	// 修复 B3（完整版）：
	// 1. 在锁内读取 oldManager/oldOpenAPI，并更新为新实例，消除并发读写数据竞态
	// 2. lifecycle.Start 失败后在失败路径回滚，保持 bot 可重启
	//
	// 注意：NewBotWithInfo 不再预创建 tokenManager，因此首次 Start() 时 oldManager == nil。
	// 仅在 Bot.Stop() 后重新 Start()（热重启）时，oldManager 才非 nil。
	var oldManager *token.Manager
	var oldOpenAPI openapi.OpenAPI

	if b.botInfo != nil {
		newManager := token.NewManagerWithContext(rootCtx, b.botInfo)
		newOpenAPI := openapi.New(newManager)

		// 在锁内原子替换，与 handleEvent 的读操作互斥
		b.mu.Lock()
		oldManager = b.tokenManager
		oldOpenAPI = b.openAPI
		b.tokenManager = newManager
		b.openAPI = newOpenAPI
		b.mu.Unlock()
	}

	// 为 OnStart 阶段创建带超时的子 context（不影响 rootCtx）
	startCtx, startCancel := context.WithTimeout(rootCtx, DefaultStartTimeout)
	defer startCancel()

	// 将 rootCtx 传给 lifecycle.Start，使 OnRun goroutine 从 rootCtx 派生
	err := b.lifecycle.Start(startCtx)

	b.mu.Lock()
	b.starting = false
	if err != nil {
		// 失败路径：回滚 tokenManager/openAPI 到旧值，使 bot 仍可重启
		if b.botInfo != nil {
			b.tokenManager = oldManager
			b.openAPI = oldOpenAPI
		}
		b.mu.Unlock()

		// 取消 rootCtx，停止新建的 tokenManager goroutine
		rootCancel()
		// 旧 manager 在整个过程中未受影响，异步停止即可（它的 context 仍是 context.Background()）
		if oldManager != nil {
			go oldManager.Stop()
		}
		logger.WithError(err).Error("[Bot] Failed to start")
		return err
	}
	b.running = true
	b.startTime = time.Now()
	b.rootCtx = rootCtx
	b.rootCancel = rootCancel
	b.mu.Unlock()

	// 启动成功：旧 manager 已被新 manager 取代，异步停止
	if oldManager != nil {
		go oldManager.Stop()
	}
	// 旧 openAPI 无需关闭（它是无状态的 wrapper，持有的 tokenManager 即将停止）
	_ = oldOpenAPI

	logger.Info("[Bot] Started successfully")

	// 若已注入插件管理器，触发所有插件的 Setup（注册 Matcher、启动 goroutine 等）
	if b.pluginManager != nil {
		if err := b.pluginManager.StartAll(b.rootCtx); err != nil {
			logger.WithError(err).Warn("[Bot] Some plugins failed to start")
		}
	}

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

// handlePlatformEvent 处理来自 platform.PlatformAdapter 的事件
//
// 直接调用 engine.ProcessPlatformEvent，不再降级到 *dto.Payload 路径。
// Engine 内部通过 context.AcquireContextFromEvent 创建平台无关的 Context，
// Handler 可通过 ctx.GetPlatformEvent() 访问原始事件，通过 ctx.Reply() 发送回复。
func (b *Bot) handlePlatformEvent(event platform.Event) {
	if event == nil {
		logger.Warn("[Bot] Received nil platform event, skipping")
		return
	}

	start := time.Now()

	if b.config.Debug {
		logger.WithFields(logger.Fields{
			"platform": event.Platform(),
			"kind":     string(event.Kind()),
			"type":     event.RawType(),
		}).Debug("[Bot] Platform event received")
	}

	// 获取该平台的 Sender
	var sender platform.Sender
	b.mu.RLock()
	reg := b.platformRegistry
	b.mu.RUnlock()
	if reg != nil {
		if pa, ok := reg.Get(event.Platform()); ok {
			sender = pa.Sender()
		}
	}
	if sender == nil {
		sender = &platform.NoopSender{}
	}

	// 直接走新引擎路径：无 dto.Payload，无 openapi.OpenAPI
	b.engine.ProcessPlatformEvent(event, sender)

	if b.config.Debug {
		logger.WithFields(logger.Fields{
			"platform": event.Platform(),
			"kind":     string(event.Kind()),
			"elapsed":  time.Since(start),
		}).Debug("[Bot] Platform event processed")
	}
}

// UsePlatformRegistry 注入多平台适配器注册表
//
// 注入后 Bot.Start() 会为每个已注册的平台适配器启动独立事件循环。
// 必须在 Bot.Start() 之前调用。
//
// 示例：
//
//	registry := platform.NewRegistry()
//	registry.Register(qq.NewAdapter(webhookConn, api))
//	registry.Register(discord.NewAdapter())
//	bot.UsePlatformRegistry(registry)
func (b *Bot) UsePlatformRegistry(r *platform.Registry) *Bot {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.platformRegistry = r
	return b
}

// PlatformRegistry 返回已注入的多平台注册表，未注入时返回 nil
func (b *Bot) PlatformRegistry() *platform.Registry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.platformRegistry
}

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
	return b.shutdownSequence(ctx, rootCancel)
}

// shutdownSequence 执行有序的关闭流程：
//
//  1. 停止插件（逆序 Teardown），在 adapter/engine 停止前完成
//     —— 保证插件 Teardown 期间仍可访问 engine 和 adapter
//  2. 取消根 context（rootCancel）
//     —— 通知 tokenManager 及所有与 rootCtx 绑定的后台 goroutine 退出
//     —— 早于 lifecycle.Stop，使 goroutine 有机会在 adapter 停止前完成清理
//  3. 停止 lifecycle（adapter.Stop + engine.Shutdown）
//  4. tokenManager.Stop()（双重保险：rootCtx 已取消，此处确保同步等待退出）
func (b *Bot) shutdownSequence(ctx context.Context, rootCancel context.CancelFunc) error {
	// Step 1: 插件逆序 Teardown
	if b.pluginManager != nil {
		logger.Debug("[Bot] Stopping plugin manager...")
		if err := b.pluginManager.StopAll(ctx); err != nil {
			logger.WithError(err).Warn("[Bot] Some plugins failed to stop cleanly")
		}
	}

	// Step 2: 取消根 context，驱动所有绑定 goroutine（tokenManager、adaptiveRateLimiter 等）退出
	if rootCancel != nil {
		rootCancel()
	}

	// Step 3: 停止 lifecycle（adapter.Stop + engine.Shutdown）
	err := b.lifecycle.Stop(ctx)

	// Step 4: tokenManager 同步兜底（rootCtx 已取消，此处等待其完全退出）
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

// UsePlugins 注入插件管理器，将其生命周期与 Bot 绑定。
//
// 调用后：
//   - Bot.Start() 会自动触发所有已注册插件的 Setup
//   - Bot.Stop() 会自动按逆序触发所有插件的 Teardown
//   - 插件 goroutine 随 Bot.Stop() 统一回收，无泄露风险
//
// 必须在 Bot.Start() 之前调用，支持链式调用：
//
//	bot.UsePlugins(pm).Start()
//
// 若需在 Build 阶段注入，使用 BotBuilder.WithPluginManager(pm)。
func (b *Bot) UsePlugins(pm *plugin.Manager) *Bot {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pluginManager = pm
	return b
}

// Plugins 返回已注入的插件管理器，未注入时返回 nil。
func (b *Bot) Plugins() *plugin.Manager {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pluginManager
}

// Engine 返回 Bot 的 Engine 实例
func (b *Bot) Engine() *engine.Engine {
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

// OnEventKind 注册处理指定平台事件类别的规则（平台无关，推荐使用）
//
// 示例：
//
//	bot.OnEventKind(platform.EventKindPrivateMessage, context.OnCommand("/ping")).Handle(handler)
func (b *Bot) OnEventKind(kind platform.EventKind, rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnEventKind(kind, rule...)
}

// WaitForShutdown 等待系统信号并优雅关闭
func (b *Bot) WaitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh) // 防止信号 channel 泄漏：函数返回后注销信号通知

	<-sigCh

	logger.Info("[Bot] Received shutdown signal")

	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	if err := b.Stop(ctx); err != nil {
		logger.WithError(err).Error("[Bot] Shutdown failed")
	}
}
