package remilia

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	eventctx "github.com/KomeiDiSanXian/remilia/core/context"
	"github.com/KomeiDiSanXian/remilia/core/engine"
	"github.com/KomeiDiSanXian/remilia/errutil"
	"github.com/KomeiDiSanXian/remilia/infra/health"
	"github.com/KomeiDiSanXian/remilia/infra/logger"
	"github.com/KomeiDiSanXian/remilia/lifecycle"
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const (
	DefaultShutdownTimeout = 30 * time.Second
	DefaultStartTimeout    = 30 * time.Second
)

// shutdownListenerActive 确保整个进程中只有一个 WaitForShutdown 处于监听状态。
//
// Bot.WaitForShutdown 与 BotManager.WaitForShutdown 共用此标志：
// 第二次调用时会立即 panic，强制开发者在编写代码时就发现误用，
// 而非在生产环境中触发难以排查的双重关闭。
var shutdownListenerActive atomic.Bool

// acquireShutdownListener 尝试抢占全局信号监听权。
// 返回 true 表示抢占成功（当前调用者应继续执行监听逻辑）；
// 返回 false 表示已有其他监听者，当前调用者应直接返回。
// caller 用于日志提示。
func acquireShutdownListener(caller string) bool {
	if !shutdownListenerActive.CompareAndSwap(false, true) {
		logger.WithField("caller", caller).Warn(
			"[remilia] WaitForShutdown is already active in this process; " +
				"this call is a no-op. Use either Bot.WaitForShutdown or BotManager.WaitForShutdown, not both.")
		return false
	}
	return true
}

// releaseShutdownListener 释放全局信号监听权（在 WaitForShutdown 返回后调用）。
func releaseShutdownListener() {
	shutdownListenerActive.Store(false)
}

// adapterCache 是每个平台适配器在启动时构建的只读快照，
// 避免热路径中对 platformRegistry 加 RLock。
// adapterCache 是每个平台适配器在启动时构建的只读快照。
// adapter 引用用于在事件到达时动态调用 Sender()，确保在 pa.Start() 初始化
// client 之后总能拿到真实发送器（而非启动前的 NoopSender）。
type adapterCache struct {
	adapter platform.Adapter
	caps    platform.Capabilities
}

// Bot 是对 Engine 的高级封装，提供完整的生命周期管理。
//
// D3：Bot 内部统一使用 platformRegistry 作为唯一的平台适配器来源，
// 不再维护单独的 adapter 字段，消除双路径冗余逻辑。
type Bot struct {
	engine           *engine.Engine
	lifecycle        *lifecycle.Manager
	health           *health.Check
	config           *Config
	pluginManager    *plugin.Manager
	platformRegistry *platform.Registry // 唯一事件来源（单/多平台均通过此注册表管理）

	// adapterSnapshot 在 Start() 时构建，此后只读，用于热路径零锁访问。
	// 使用 atomic.Value 存储 map[string]adapterCache 快照：
	//   - Start() 内写入一次（Store）
	//   - handlePlatformEvent 热路径只需 Load()，无任何锁开销
	// 与 core/engine 的 COW 设计保持一致。
	adapterSnapshot atomic.Value // stores map[string]adapterCache

	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu        sync.RWMutex
	running   bool
	starting  bool
	startTime time.Time
	stopTime  time.Time
}

// Config Bot 配置
type Config struct {
	Name    string
	Version string
	Debug   bool
}

// NewBot 创建新的 Bot 实例。
//
// adapter 可以为 nil（仅使用多平台注册表时），非 nil 时会自动包装进内部 Registry。
// 推荐使用 BotBuilder.Build() 代替直接调用。
//
// 若 engine 为 nil，返回错误。编写测试或确信参数合法时可使用 [MustNewBot]。
func NewBot(adapter platform.Adapter, e *engine.Engine, opts ...Option) (*Bot, error) {
	if e == nil {
		return nil, errutil.New("engine cannot be nil")
	}

	b := &Bot{
		engine: e,
		config: &Config{
			Name:    "remilia-bot",
			Version: Version,
			Debug:   false,
		},
	}

	// 应用选项（可能设置 platformRegistry 或其他字段）
	for _, opt := range opts {
		opt(b)
	}

	// 若提供了单个适配器，自动注册到 Registry
	if adapter != nil {
		b.health = health.NewCheck()
		b.health.AddChecker(NewAdapterHealthChecker(adapter))
		if b.platformRegistry == nil {
			b.platformRegistry = platform.NewRegistry()
		}
		b.platformRegistry.Register(adapter)
	} else {
		b.health = health.NewCheck()
		// N5: registry-only 模式下，平台适配器 health checker 在 Start() 中注册
		logger.Debug("[Bot] adapter is nil; events will only be received via platformRegistry")
	}

	b.health.AddChecker(NewBotStatusChecker(b))
	b.health.AddChecker(health.NewEngineHealthChecker(e))
	b.buildBaseLifecycle()
	return b, nil
}

// MustNewBot 同 [NewBot]，但失败时 panic。适用于初始化阶段且参数已确认合法的场景。
func MustNewBot(adapter platform.Adapter, e *engine.Engine, opts ...Option) *Bot {
	bot, err := NewBot(adapter, e, opts...)
	if err != nil {
		panic(err)
	}
	return bot
}

// buildBaseLifecycle 创建 lifecycle.Manager 并注册 engine 基础组件。
//
// 平台适配器组件在 Start() 时从 platformRegistry 动态注册，支持热重启。
func (b *Bot) buildBaseLifecycle() {
	lm := lifecycle.NewManager()
	lm.Register(lifecycle.NewSimpleComponent(
		"engine",
		func(ctx context.Context) error { return nil },
		nil,
		func(ctx context.Context) error { return b.engine.Shutdown(ctx) },
	))
	b.lifecycle = lm
}

// Start 启动 Bot
//
// 并发安全设计（锁-解-锁模式）：
//
//   - 第一阶段持锁（快速）：检查 running/starting 标志，设置 starting=true
//   - 解锁执行耗时操作（启动 lifecycle、注册组件），期间 IsRunning() 等读方法可正常访问
//   - 第二阶段持锁（快速）：写入 running=true、startTime、rootCtx/rootCancel
//
// starting 标志在整过程中防止并发二次启动：任何 goroutine 在 starting==true 期间
// 调用 Start() 都会被拒绝（返回 "bot is already starting"）。
//
// 若启动失败，starting 被重置为 false 且 running 保持 false，
// 下次 Start() 调用可正常重试。rootCtx/rootCancel 在失败路径中已被 cancel，
// 不会泄漏。
func (b *Bot) Start() error {
	// 并发安全：先用写锁检查并设置 starting 标志，随后解锁。
	// 这是有意为之的"锁-解-锁"模式：
	//   1. 快速持锁完成状态检查，避免长时间阻塞其他读操作（如 IsRunning）
	//   2. 解锁后执行耗时的 lifecycle.Start / pluginManager.StartAll，
	//      期间其他 goroutine 仍可读取 Bot 状态（如健康检查）
	//   3. 启动成功后再次加锁写入 running=true
	// starting 标志在整个过程中防止并发二次启动。
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

	rootCtx, rootCancel := context.WithCancel(context.Background())

	// 每次 Start() 重建 lifecycle，确保热重启不会重复注册组件
	b.buildBaseLifecycle()

	// 重建 health，防止热重启后健康检查器叠加重复（B-5）
	b.health = health.NewCheck()
	b.health.AddChecker(NewBotStatusChecker(b))
	b.health.AddChecker(health.NewEngineHealthChecker(b.engine))

	// 为 platformRegistry 中的每个适配器注册独立的生命周期组件
	b.mu.RLock()
	reg := b.platformRegistry
	b.mu.RUnlock()

	// 构建适配器快照（P-1），热路径直接读快照，无需每事件加 RLock
	snapshot := make(map[string]adapterCache)
	if reg != nil {
		for _, pa := range reg.All() {
			// N5: 为每个平台适配器注册独立的健康检查器
			b.health.AddChecker(NewAdapterHealthChecker(pa))
			snapshot[pa.Platform()] = adapterCache{
				adapter: pa,
				caps:    pa.Capabilities(),
			}
			name := "platform:" + pa.Platform()
			b.lifecycle.Register(lifecycle.NewSimpleComponent(
				name,
				nil,
				func(ctx context.Context) error {
					return pa.Start(ctx, b.handlePlatformEvent)
				},
				func(ctx context.Context) error {
					return pa.Stop(ctx)
				},
			))
		}
	}

	b.adapterSnapshot.Store(snapshot)

	// 将 pluginManager 注册为 lifecycle 的最后一个组件（4.1 fix）。
	// 注册顺序：engine → platform adapters → plugin-manager
	// 停止顺序（逆序）：plugin-manager → platform adapters → engine
	// 这样插件 Teardown 在平台连接断开之前执行，且在 lifecycle.Stop() 返回前 rootCtx 仍有效。
	if b.pluginManager != nil {
		b.lifecycle.Register(plugin.NewManagerComponent(b.pluginManager))
	}

	startCtx, startCancel := context.WithTimeout(rootCtx, DefaultStartTimeout)
	defer startCancel()

	err := b.lifecycle.Start(startCtx)

	b.mu.Lock()
	b.starting = false
	if err != nil {
		b.mu.Unlock()
		rootCancel()
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

// handlePlatformEvent 处理来自 platform.Adapter 的事件。
//
// D3：统一通过 platformRegistry 查找 Sender 和 Capabilities，无双路径。
// F2：将 Capabilities 注入 ProcessPlatformEvent，Handler 可通过 ctx.GetPlatformCapabilities() 获取。
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
			"type":     platform.RawType(event),
		}).Debug("[Bot] Platform event received")
	}

	var sender platform.Sender
	var caps platform.Capabilities
	var botID string

	// P-1: 读取启动时构建的快照（atomic.Value.Load，零锁，热路径无 RLock 开销）。
	// 同一次 Load 同时取出 sender、caps 和 botID，避免重复 map 查找（原先两次查找已合并）。
	snapshot, _ := b.adapterSnapshot.Load().(map[string]adapterCache)

	if snapshot != nil {
		if c, ok := snapshot[event.Platform()]; ok {
			sender = c.adapter.Sender() // 动态获取，确保拿到 Start() 后初始化的真实发送器
			caps = c.caps
			// F-9: 若适配器实现了 BotIdentity，注入 botID 供 ctx.IsFromSelf() 使用
			if bi, ok2 := c.adapter.(platform.BotIdentity); ok2 {
				botID = bi.BotID()
			} else if b.config.Debug {
				// 调试模式下提示适配器未实现 BotIdentity，
				// 否则 ctx.IsFromSelf() 将永远返回 false，排查困难。
				logger.WithField("platform", event.Platform()).
					Debug("[Bot] Adapter does not implement platform.BotIdentity; ctx.IsFromSelf() will always return false. " +
						"Implement BotIdentity on your adapter to enable self-message detection.")
			}
		}
	}

	if sender == nil {
		logger.WithField("platform", event.Platform()).Warn(
			"[Bot] No sender found for platform, all ctx.Reply() calls will be silently dropped")
		sender = &platform.NoopSender{}
	}

	if botID != "" {
		b.engine.ProcessPlatformEventEx(event, sender, botID, caps)
	} else {
		b.engine.ProcessPlatformEvent(event, sender, caps)
	}

	if b.config.Debug {
		logger.WithFields(logger.Fields{
			"platform": event.Platform(),
			"kind":     string(event.Kind()),
			"elapsed":  time.Since(start),
		}).Debug("[Bot] Platform event processed")
	}
}

// UsePlatformRegistry 注入多平台适配器注册表。
//
// 必须在 Bot.Start() 之前调用。
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

// shutdownSequence 执行有序关闭序列。
//
// 停止顺序（严格遵守，不得随意调整）：
//  1. lifecycle.Stop()  — 统一停止所有 lifecycle 组件（逆注册顺序）：
//     a. plugin-manager OnStop → pm.StopAll() → 所有插件 Teardown
//     b. platform adapters OnStop → 平台连接断开
//     c. engine OnStop
//     插件 Teardown 在此阶段运行，rootCtx 尚未取消，可正常访问平台 API。
//  2. rootCancel()      — lifecycle 完全停止后才取消 rootCtx，
//     通知所有持有 rootCtx 的后台 goroutine 退出。
//
// lifecycle.Stop() 返回的错误包含了插件停止失败的详细信息（4.2 fix：不再静默吞噬），
// 会被传递给 Bot.Stop() 的调用方。
//
// 注意：pluginManager 已作为 lifecycle.Component 注册（4.1 fix），
// 无需再手动调用 pm.StopAll()，lifecycle.Stop() 会自动触发。
func (b *Bot) shutdownSequence(ctx context.Context, rootCancel context.CancelFunc) error {
	// lifecycle.Stop() 负责停止所有组件（含插件、适配器、engine）
	// 此时 rootCtx 尚未取消，插件 Teardown 可安全访问外部资源
	err := b.lifecycle.Stop(ctx)

	// lifecycle 完全停止后再取消 rootCtx
	if rootCancel != nil {
		rootCancel()
	}

	if err != nil {
		logger.WithError(err).Error("[Bot] Stop completed with errors")
		return err
	}
	logger.Info("[Bot] Stop complete")
	return nil
}

// Shutdown 使用默认超时时间优雅关闭 Bot
func (b *Bot) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()
	return b.Stop(ctx)
}

// ShutdownAsync 在后台 goroutine 中发起优雅关闭，立即返回。
//
// 返回一个只读 channel，关闭完成后会写入最终错误（nil 表示成功）。
// 调用方可选择是否等待结果：
//
//	// 触发后不等待（fire-and-forget）
//	bot.ShutdownAsync()
//
//	// 触发后等待结果
//	if err := <-bot.ShutdownAsync(); err != nil {
//	    log.Println("shutdown error:", err)
//	}
//
// 适用于以下场景：
//   - HTTP handler 需要先响应客户端再关闭（同步 Shutdown 会阻塞响应）
//   - 插件回调内部触发关闭（同步调用会在 lifecycle 链上死锁）
//   - 与外部框架集成时，对方不允许阻塞其事件循环
//
// 注意：多次调用是安全的，后续调用会直接返回已关闭的 channel（Bot.Stop 本身幂等）。
func (b *Bot) ShutdownAsync() <-chan error {
	ch := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
		defer cancel()
		ch <- b.Stop(ctx)
	}()
	return ch
}

// UsePlugins 注入插件管理器
func (b *Bot) UsePlugins(pm *plugin.Manager) *Bot {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pluginManager = pm
	return b
}

// Plugins 返回已注入的插件管理器
func (b *Bot) Plugins() *plugin.Manager {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.pluginManager
}

// Engine 返回 Bot 的 Engine 实例
func (b *Bot) Engine() *engine.Engine { return b.engine }

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

// Config 获取配置副本。
func (b *Bot) Config() Config {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return *b.config
}

// Health 健康检查
func (b *Bot) Health() health.CheckResponse {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.health.Check(ctx)
}

// HealthCheck 返回 health.Check 实例
func (b *Bot) HealthCheck() *health.Check { return b.health }

// State 返回生命周期状态
func (b *Bot) State() lifecycle.State { return b.lifecycle.State() }

// OnAny 注册处理所有事件的规则
func (b *Bot) OnAny(rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnAny(rule...)
}

// OnEventKind 注册处理指定平台事件类别的规则（平台无关，推荐）
func (b *Bot) OnEventKind(kind platform.EventKind, rule ...eventctx.Rule) *engine.Matcher {
	return b.engine.OnEventKind(kind, rule...)
}

// WaitForShutdown 等待系统信号并优雅关闭。
//
// 收到第一个 SIGINT（Ctrl+C）或 SIGTERM 时，开始优雅关闭（等待后台清理完成）。
// 若在优雅关闭期间再次收到 SIGINT，立即强制退出（os.Exit(1)），
// 不再等待剩余清理工作——这与大多数 CLI 工具的行为一致。
//
// timeout 为可选参数，指定优雅关闭的超时时间；省略时使用 [DefaultShutdownTimeout]（30s）。
// 若同一进程已有另一个 WaitForShutdown 处于监听状态，此次调用会直接返回并打印 Warn 日志。
//
// 若需要完全自定义 context（如携带 trace），请直接调用 [Bot.Stop]。
//
// 多 Bot 场景请统一使用 [BotManager.WaitForShutdown]。
func (b *Bot) WaitForShutdown(timeout ...time.Duration) {
	if !acquireShutdownListener("Bot.WaitForShutdown") {
		return
	}
	defer releaseShutdownListener()

	shutdownTimeout := DefaultShutdownTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		shutdownTimeout = timeout[0]
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	<-sigCh
	logger.Info("[Bot] Received shutdown signal, shutting down gracefully... (press Ctrl+C again to force exit)")

	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := b.Stop(ctx); err != nil {
			logger.WithError(err).Error("[Bot] Shutdown failed")
		}
	}()

	select {
	case <-done:
	case <-sigCh:
		logger.Warn("[Bot] Forced exit by second signal")
		os.Exit(1)
	}
}
