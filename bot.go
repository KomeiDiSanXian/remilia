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
	"github.com/KomeiDiSanXian/remilia/platform"
	"github.com/KomeiDiSanXian/remilia/plugin"
)

const (
	DefaultShutdownTimeout = 30 * time.Second
	DefaultStartTimeout    = 30 * time.Second
)

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

	// adapterSnapshot 在 Start() 时构建，此后只读，用于热路径无锁访问。
	// 仅在 Start() 内写入，在 handlePlatformEvent 中读取。
	adapterSnapshot map[string]adapterCache

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
func NewBot(adapter platform.Adapter, e *engine.Engine, opts ...Option) *Bot {
	if e == nil {
		logger.Panic("[Bot] engine cannot be nil")
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
	return b
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
	b.mu.Lock()
	b.adapterSnapshot = snapshot
	b.mu.Unlock()

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

	if b.pluginManager != nil {
		if err := b.pluginManager.StartAll(); err != nil {
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

	// P-1: 读取启动时构建的快照，避免热路径每事件加 RLock
	b.mu.RLock()
	snapshot := b.adapterSnapshot
	b.mu.RUnlock()

	if snapshot != nil {
		if c, ok := snapshot[event.Platform()]; ok {
			sender = c.adapter.Sender() // 动态获取，确保拿到 Start() 后初始化的真实发送器
			caps = c.caps
		}
	}

	if sender == nil {
		logger.WithField("platform", event.Platform()).Warn(
			"[Bot] No sender found for platform, all ctx.Reply() calls will be silently dropped")
		sender = &platform.NoopSender{}
	}

	b.engine.ProcessPlatformEvent(event, sender, caps)

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

func (b *Bot) shutdownSequence(ctx context.Context, rootCancel context.CancelFunc) error {
	if b.pluginManager != nil {
		logger.Debug("[Bot] Stopping plugin manager...")
		if err := b.pluginManager.StopAll(); err != nil {
			logger.WithError(err).Warn("[Bot] Some plugins failed to stop cleanly")
		}
	}
	if rootCancel != nil {
		rootCancel()
	}
	err := b.lifecycle.Stop(ctx)
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

// WaitForShutdown 等待系统信号并优雅关闭
func (b *Bot) WaitForShutdown() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	<-sigCh

	logger.Info("[Bot] Received shutdown signal")

	ctx, cancel := context.WithTimeout(context.Background(), DefaultShutdownTimeout)
	defer cancel()

	if err := b.Stop(ctx); err != nil {
		logger.WithError(err).Error("[Bot] Shutdown failed")
	}
}
